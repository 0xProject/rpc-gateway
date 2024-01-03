package proxy

import (
	"time"
)

type HealthCheckConfig struct {
	Interval         time.Duration `yaml:"interval"`
	Timeout          time.Duration `yaml:"timeout"`
	FailureThreshold uint          `yaml:"failureThreshold"`
	SuccessThreshold uint          `yaml:"successThreshold"`
}

type ProxyConfig struct { // nolint:revive
	Port            string        `yaml:"port"`
	UpstreamTimeout time.Duration `yaml:"upstreamTimeout"`
}

// This struct is temporary. It's about to keep the input interface clean and simple.
type Config struct {
	Proxy        ProxyConfig
	Targets      []NodeProviderConfig
	HealthChecks HealthCheckConfig
}
