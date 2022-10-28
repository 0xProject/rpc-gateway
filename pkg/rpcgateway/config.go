package rpcgateway

import (
	"github.com/0xProject/rpc-gateway/pkg/metrics"
	"github.com/0xProject/rpc-gateway/pkg/proxy"
)

type Logging struct {
	LogRequestBody bool `yaml:"log_request_body"`
}

type RPCGatewayConfig struct { //nolint:revive
	Metrics      metrics.Config          `yaml:"metrics"`
	Proxy        proxy.ProxyConfig       `yaml:"proxy"`
	HealthChecks proxy.HealthCheckConfig `yaml:"healthChecks"`
	Targets      []proxy.TargetConfig    `yaml:"targets"`
	Logging      Logging                 `yaml:"logging"`
}
