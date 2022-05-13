package rpcgateway

import (
	"github.com/0xProject/rpc-gateway/pkg/metrics"
	"github.com/0xProject/rpc-gateway/pkg/proxy"
)

type RPCGatewayConfig struct { //nolint:revive
	Metrics      metrics.Config          `yaml:"metrics"`
	Proxy        proxy.ProxyConfig       `yaml:"proxy"`
	HealthChecks proxy.HealthCheckConfig `yaml:"healthChecks"`
	Targets      []proxy.TargetConfig    `yaml:"targets"`
}
