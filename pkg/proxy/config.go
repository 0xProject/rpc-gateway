package proxy

import (
	"net/url"
	"time"
)

type HealthCheckConfig struct {
	Interval         time.Duration `yaml:"interval"`
	Timeout          time.Duration `yaml:"timeout"`
	FailureThreshold uint          `yaml:"failureThreshold"`
	SuccessThreshold uint          `yaml:"successThreshold"`

	// Should the RollingWindow Taint be enabled
	// Set this to false will disable marking the RPC as tainted
	// when the error rate reaches the threshold
	RollingWindowTaintEnabled bool `yaml:"rollingWindowTaintEnabled"`

	RollingWindowSize             int     `yaml:"rollingWindowSize"`
	RollingWindowFailureThreshold float64 `yaml:"rollingWindowFailureThreshold"`
}

type ProxyConfig struct { // nolint:revive
	Port                            string        `yaml:"port"`
	AllowedNumberOfRetriesPerTarget uint          `yaml:"allowedNumberOfRetriesPerTarget"`
	AllowedNumberOfReroutes         uint          `yaml:"allowedNumberOfReroutes"`
	RetryDelay                      time.Duration `yaml:"retryDelay"`
	UpstreamTimeout                 time.Duration `yaml:"upstreamTimeout"`
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
	Backup     *bool                  `yaml:"backup,omitempty"`
	Weight     *int                   `yaml:"weight,omitempty"`
	Connection TargetConfigConnection `yaml:"connection"`
}

func (target *TargetConfig) IsBackup() bool {
	if target.Backup == nil {
		return false
	}

	return *target.Backup
}

func (target *TargetConfig) GetWeight() int {
	if target.Weight == nil || *target.Weight < 0 {
		return 100
	}

	return *target.Weight
}

func (target *TargetConfig) GetParsedHTTPURL() (*url.URL, error) {
	return url.Parse(target.Connection.HTTP.URL)
}

// This struct is temporary. It's about to keep the input interface clean and simple.
//
type Config struct {
	Proxy        ProxyConfig
	Targets      []TargetConfig
	HealthChecks HealthCheckConfig
}
