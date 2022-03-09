package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/mwitkow/go-conntrack"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type RpcGateway struct {
	httpFailoverProxy  *HttpFailoverProxy
	healthcheckManager *HealthcheckManager

	server *http.Server
}

func (r *RpcGateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.server.Handler.ServeHTTP(w, req)
}

func (r *RpcGateway) Start(ctx context.Context) error {
	zap.L().Info("starting rpc gateway")

	go func() {
		zap.L().Info("starting healthcheck manager")
		err := r.healthcheckManager.Start(ctx)
		if err != nil {
			// TODO: Handle gracefully
			zap.L().Fatal("failed to start healthcheck manager", zap.Error(err))
		}
	}()

	listenAddress := fmt.Sprintf(":%s", r.httpFailoverProxy.gatewayConfig.Proxy.Port)
	zap.L().Info("starting http failover proxy", zap.String("listenAddr", listenAddress))
	listener, err := net.Listen("tcp", fmt.Sprintf(listenAddress))
	if err != nil {
		zap.L().Error("Failed to listen", zap.Error(err))
	}
	httpListener := conntrack.NewListener(listener, conntrack.TrackWithTracing())
	return r.server.Serve(httpListener)
}

func (r *RpcGateway) Stop(ctx context.Context) error {
	zap.L().Info("stopping rpc gateway")
	err := r.healthcheckManager.Stop(ctx)
	if err != nil {
		zap.L().Error("healthcheck manager failed to stop gracefully", zap.Error(err))
	}
	return r.server.Close()
}

func (r *RpcGateway) GetCurrentTarget() string {
	return r.httpFailoverProxy.GetNextTargetName()
}

func NewRpcGateway(config RpcGatewayConfig) *RpcGateway {
	healthcheckManager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: config.Targets,
		Config:  config.HealthChecks,
	})
	httpFailoverProxy := NewHttpFailoverProxy(config, healthcheckManager)

	r := mux.NewRouter()
	r.Use(LoggingMiddleware())
	r.PathPrefix("/").Handler(httpFailoverProxy)
	r.PathPrefix("").Handler(httpFailoverProxy)

	srv := &http.Server{
		Handler:      r,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	return &RpcGateway{
		httpFailoverProxy:  httpFailoverProxy,
		healthcheckManager: healthcheckManager,
		server:             srv,
	}
}

func NewRpcGatewayFromConfigFile(path string) (*RpcGatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return NewRpcGatewayFromConfigBytes(data)
}

func NewRpcGatewayFromConfigBytes(configBytes []byte) (*RpcGatewayConfig, error) {
	config := RpcGatewayConfig{}
	err := yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func NewRpcGatewayFromConfigString(configString string) (*RpcGatewayConfig, error) {
	return NewRpcGatewayFromConfigBytes([]byte(configString))
}
