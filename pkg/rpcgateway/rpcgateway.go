package rpcgateway

import (
	"context"
	"os"

	"github.com/0xProject/rpc-gateway/pkg/proxy"
	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type RPCGateway struct {
	config             RPCGatewayConfig
	httpFailoverProxy  *proxy.Proxy
	healthcheckManager *proxy.HealthcheckManager
	instance           *echo.Echo
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

	return r.instance.Start(":8080")
}

func (r *RPCGateway) Stop(ctx context.Context) error {
	zap.L().Info("stopping rpc gateway")

	if err := r.healthcheckManager.Stop(ctx); err != nil {
		zap.L().Error("healthcheck manager failed to stop gracefully", zap.Error(err))
	}

	return r.instance.Close()
}

func (r *RPCGateway) GetCurrentTarget() string {
	return r.httpFailoverProxy.GetNextTargetName()
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

	server := echo.New()
	server.HideBanner = true

	server.Use(middleware.Decompress())
	server.Use(middleware.Logger())

	metrics := prometheus.NewPrometheus("rpc_gateway_", nil)
	metrics.Use(server)

	gateway := &RPCGateway{
		config:             config,
		httpFailoverProxy:  httpFailoverProxy,
		healthcheckManager: healthcheckManager,
		instance:           server,
	}

	server.POST("/", echo.WrapHandler(httpFailoverProxy))

	return gateway
}

func NewRPCGatewayFromConfigFile(path string) (*RPCGatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return NewRPCGatewayFromConfigBytes(data)
}

func NewRPCGatewayFromConfigBytes(data []byte) (*RPCGatewayConfig, error) {
	config := RPCGatewayConfig{}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
