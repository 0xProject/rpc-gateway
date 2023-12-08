package rpcgateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/0xProject/rpc-gateway/internal/proxy"
	"github.com/gorilla/mux"
	"github.com/mwitkow/go-conntrack"
	"github.com/purini-to/zapmw"
	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	"github.com/slok/go-http-metrics/middleware/std"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

type RPCGateway struct {
	config             RPCGatewayConfig
	httpFailoverProxy  *proxy.Proxy
	healthcheckManager *proxy.HealthcheckManager
	server             *http.Server
}

func (r *RPCGateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.server.Handler.ServeHTTP(w, req)
}

func (r *RPCGateway) Start(ctx context.Context) error {
	zap.L().Info("starting rpc gateway")

	go func() {
		zap.L().Info("starting healthcheck manager")
		err := r.healthcheckManager.Start(ctx)
		if err != nil {
			// TODO: Handle gracefully
			zap.L().Fatal("failed to start healthcheck manager", zap.Error(err))
		}
	}()

	listenAddress := fmt.Sprintf(":%s", r.config.Proxy.Port)
	zap.L().Info("starting http failover proxy", zap.String("listenAddr", listenAddress))
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		zap.L().Error("Failed to listen", zap.Error(err))
	}
	httpListener := conntrack.NewListener(listener, conntrack.TrackWithTracing())
	return r.server.Serve(httpListener)
}

func (r *RPCGateway) Stop(ctx context.Context) error {
	zap.L().Info("stopping rpc gateway")
	err := r.healthcheckManager.Stop(ctx)
	if err != nil {
		zap.L().Error("healthcheck manager failed to stop gracefully", zap.Error(err))
	}
	return r.server.Close()
}

func NewRPCGateway(config RPCGatewayConfig) *RPCGateway {
	healthcheckManager := proxy.NewHealthcheckManager(
		proxy.HealthcheckManagerConfig{
			Targets: config.Targets,
			Config:  config.HealthChecks,
		})
	httpFailoverProxy := proxy.NewProxy(
		proxy.Config{
			Proxy:        config.Proxy,
			Targets:      config.Targets,
			HealthChecks: config.HealthChecks,
		},
		healthcheckManager,
	)

	r := mux.NewRouter()

	r.Use(std.HandlerProvider("",
		middleware.New(middleware.Config{
			Recorder: metrics.NewRecorder(metrics.Config{}),
		})),
	)

	r.Use(
		zapmw.WithZap(zap.L()),
		zapmw.Request(zapcore.InfoLevel, "request"),
		zapmw.Recoverer(zapcore.ErrorLevel, "recover", zapmw.RecovererDefault),
	)

	srv := &http.Server{
		Handler:           r,
		WriteTimeout:      15 * time.Second,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	gateway := &RPCGateway{
		config:             config,
		httpFailoverProxy:  httpFailoverProxy,
		healthcheckManager: healthcheckManager,
		server:             srv,
	}

	r.PathPrefix("/").Handler(httpFailoverProxy)

	return gateway
}

func NewRPCGatewayFromConfigFile(path string) (*RPCGatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return NewRPCGatewayFromConfigBytes(data)
}

func NewRPCGatewayFromConfigBytes(configBytes []byte) (*RPCGatewayConfig, error) {
	config := RPCGatewayConfig{}

	if err := yaml.Unmarshal(configBytes, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func NewRPCGatewayFromConfigString(configString string) (*RPCGatewayConfig, error) {
	return NewRPCGatewayFromConfigBytes([]byte(configString))
}
