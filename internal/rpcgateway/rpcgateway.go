package rpcgateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/0xProject/rpc-gateway/internal/metrics"
	"github.com/0xProject/rpc-gateway/internal/proxy"
	"github.com/carlmjohnson/flowmatic"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v2"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type RPCGateway struct {
	config  RPCGatewayConfig
	proxy   *proxy.Proxy
	hcm     *proxy.HealthCheckManager
	server  *http.Server
	metrics *metrics.Server
}

func (r *RPCGateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.server.Handler.ServeHTTP(w, req)
}

func (r *RPCGateway) Start(c context.Context) error {
	return flowmatic.Do(
		func() error {
			return errors.Wrap(r.hcm.Start(c), "failed to start health check manager")
		},
		func() error {
			return errors.Wrap(r.server.ListenAndServe(), "failed to start rpc-gateway")
		},
		func() error {
			return errors.Wrap(r.metrics.Start(), "failed to start metrics server")
		},
	)
}

func (r *RPCGateway) Stop(c context.Context) error {
	return flowmatic.Do(
		func() error {
			return errors.Wrap(r.hcm.Stop(c), "failed to stop health check manager")
		},
		func() error {
			return errors.Wrap(r.server.Close(), "failed to stop rpc-gateway")
		},
		func() error {
			return errors.Wrap(r.metrics.Stop(), "failed to stop metrics server")
		},
	)
}

func NewRPCGateway(config RPCGatewayConfig) *RPCGateway {
	logLevel := slog.LevelWarn
	if os.Getenv("DEBUG") == "true" {
		logLevel = slog.LevelDebug
	}

	logger := httplog.NewLogger("rpc-gateway", httplog.Options{
		JSON:           true,
		RequestHeaders: true,
		LogLevel:       logLevel,
	})

	hcm := proxy.NewHealthCheckManager(
		proxy.HealthCheckManagerConfig{
			Targets: config.Targets,
			Config:  config.HealthChecks,
			Logger: slog.New(
				slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
					Level: logLevel,
				})),
		})
	proxy := proxy.NewProxy(
		proxy.Config{
			Proxy:        config.Proxy,
			Targets:      config.Targets,
			HealthChecks: config.HealthChecks,
		},
		hcm,
	)

	r := chi.NewRouter()
	r.Use(httplog.RequestLogger(logger))
	r.Handle("/", proxy)

	return &RPCGateway{
		config: config,
		proxy:  proxy,
		hcm:    hcm,
		metrics: metrics.NewServer(
			metrics.Config{
				Port: config.Metrics.Port,
			},
		),
		server: &http.Server{
			Addr:              fmt.Sprintf(":%s", config.Proxy.Port),
			Handler:           r,
			WriteTimeout:      time.Second * 15,
			ReadTimeout:       time.Second * 15,
			ReadHeaderTimeout: time.Second * 5,
		},
	}
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
