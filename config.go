package main

import (
	"net/url"
	"time"
)

type MetricsConfig struct {
	Port string `yaml:"port"`
}

type ProxyConfig struct {
	Port                            string        `yaml:"port"`
	AllowedNumberOfRetriesPerTarget uint          `yaml:"allowedNumberOfRetriesPerTarget"`
	AllowedNumberOfReroutes         uint          `yaml:"allowedNumberOfReroutes"`
	RetryDelay                      time.Duration `yaml:"retryDelay"`
	UpstreamTimeout                 time.Duration `yaml:"upstreamTimeout"`
}

type HealthCheckConfig struct {
	Interval                      time.Duration `yaml:"interval"`
	Timeout                       time.Duration `yaml:"timeout"`
	FailureThreshold              uint          `yaml:"failureThreshold"`
	SuccessThreshold              uint          `yaml:"successThreshold"`

	// Should the RollingWindow Taint be enabled
	// Set this to false will disable marking the RPC as tainted
	// when the error rate reaches the threshold
	RollingWindowTaintEnabled			bool					`yaml:"rollingWindowTaintEnabled"`

	RollingWindowSize             int           `yaml:"rollingWindowSize"`
	RollingWindowFailureThreshold float64       `yaml:"rollingWindowFailureThreshold"`
}

type TargetConnectionHTTP struct {
	URL         string `yaml:"url"`
	Compression bool   `yaml:"compression"`
}

type TargetConfigConnection struct {
	HTTP TargetConnectionHTTP `yaml:"http"`
}

type TargetConfig struct {
	Name       string                 `yaml:"name"`
	Connection TargetConfigConnection `yaml:"connection"`
}

func (t *TargetConfig) GetParsedHttpURL() (*url.URL, error) {
	return url.Parse(t.Connection.HTTP.URL)
}

type RpcGatewayConfig struct {
	Metrics      MetricsConfig     `yaml:"metrics"`
	Proxy        ProxyConfig       `yaml:"proxy"`
	HealthChecks HealthCheckConfig `yaml:"healthChecks"`
	Targets      []TargetConfig    `yaml:"targets"`
}
