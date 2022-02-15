package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
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
		err := r.healthcheckManager.Start(ctx)
		if err != nil {
			// TODO: Handle gracefully
			zap.L().Fatal("failed to start healtcheck manager", zap.Error(err))
		}
	}()
	return r.server.ListenAndServe()
}

func (r *RpcGateway) Stop(ctx context.Context) error {
	zap.L().Info("stopping rpc gateway")
	err := r.healthcheckManager.Stop(ctx)
	zap.L().Error("healthcheck manager failed to stop gracefully", zap.Error(err))
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
	requestLogger := &RequestLogger{}

	r := mux.NewRouter()
	r.Use(requestLogger.Middleware)
	r.PathPrefix("/").Handler(httpFailoverProxy)
	r.PathPrefix("").Handler(httpFailoverProxy)

	srv := &http.Server{
		Handler:      r,
		Addr:         "0.0.0.0:8000",
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
