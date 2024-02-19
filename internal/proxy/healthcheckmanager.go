package proxy

import (
	"context"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type HealthCheckManagerConfig struct {
	Targets []NodeProviderConfig
	Config  HealthCheckConfig
}

type HealthCheckManager struct {
	hcs []*HealthChecker

	metricRPCProviderInfo        *prometheus.GaugeVec
	metricRPCProviderStatus      *prometheus.GaugeVec
	metricRPCProviderBlockNumber *prometheus.GaugeVec
	metricRPCProviderGasLimit    *prometheus.GaugeVec
}

func NewHealthCheckManager(config HealthCheckManagerConfig) (*HealthCheckManager, error) {
	hcm := &HealthCheckManager{
		metricRPCProviderInfo: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "zeroex_rpc_gateway_provider_info",
				Help: "Gas limit of a given provider",
			}, []string{
				"index",
				"provider",
			}),
		metricRPCProviderStatus: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "zeroex_rpc_gateway_provider_status",
				Help: "Current status of a given provider by type. Type can be either healthy or tainted.",
			}, []string{
				"provider",
				"type",
			}),
		metricRPCProviderBlockNumber: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "zeroex_rpc_gateway_provider_block_number",
				Help: "Block number of a given provider",
			}, []string{
				"provider",
			}),
		metricRPCProviderGasLimit: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "zeroex_rpc_gateway_provider_gasLimit_number",
				Help: "Gas limit of a given provider",
			}, []string{
				"provider",
			}),
	}

	for _, target := range config.Targets {
		hc, err := NewHealthChecker(
			HealthCheckerConfig{
				URL:              target.Connection.HTTP.URL,
				Name:             target.Name,
				Interval:         config.Config.Interval,
				Timeout:          config.Config.Timeout,
				FailureThreshold: config.Config.FailureThreshold,
				SuccessThreshold: config.Config.SuccessThreshold,
			})

		if err != nil {
			return nil, err
		}

		hcm.hcs = append(hcm.hcs, hc)
	}

	return hcm, nil
}

func (h *HealthCheckManager) runLoop(c context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Done():
			return nil
		case <-ticker.C:
			h.reportStatusMetrics()
		}
	}
}

func (h *HealthCheckManager) IsHealthy(name string) bool {
	for _, hc := range h.hcs {
		if hc.Name() == name && hc.IsHealthy() {
			return true
		}
	}

	return false
}

func (h *HealthCheckManager) reportStatusMetrics() {
	for _, hc := range h.hcs {
		if hc.IsHealthy() {
			h.metricRPCProviderStatus.WithLabelValues(hc.Name(), "healthy").Set(1)
		} else {
			h.metricRPCProviderStatus.WithLabelValues(hc.Name(), "healthy").Set(0)
		}

		h.metricRPCProviderGasLimit.WithLabelValues(hc.Name()).Set(float64(hc.BlockNumber()))
		h.metricRPCProviderBlockNumber.WithLabelValues(hc.Name()).Set(float64(hc.BlockNumber()))
	}
}

func (h *HealthCheckManager) Start(c context.Context) error {
	for i, hc := range h.hcs {
		h.metricRPCProviderInfo.WithLabelValues(strconv.Itoa(i), hc.Name()).Set(1)
		go hc.Start(c)
	}

	return h.runLoop(c)
}

func (h *HealthCheckManager) Stop(c context.Context) error {
	for _, hc := range h.hcs {
		err := hc.Stop(c)
		if err != nil {
			zap.L().Error("healtchecker stop error", zap.Error(err))
		}
	}

	return nil
}
