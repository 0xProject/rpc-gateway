package main

import (
	"context"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type HealthcheckManagerConfig struct {
	Targets []TargetConfig
	Config  HealthCheckConfig
}

func NewRollingWindowWrapper(name string, windowSize int) *RollingWindowWrapper {
	return &RollingWindowWrapper{
		Name:          name,
		rollingWindow: NewRollingWindow(windowSize),
	}
}

type RollingWindowWrapper struct {
	rollingWindow *RollingWindow
	Name          string
}

type HealthcheckManager struct {
	healthcheckers []Healthchecker
	rollingWindows []*RollingWindowWrapper

	requestFailureThreshold   float64
	rollingWindowTaintEnabled bool

	metricRPCProviderInfo        *prometheus.GaugeVec
	metricRPCProviderStatus      *prometheus.GaugeVec
	metricResponseTime           *prometheus.HistogramVec
	metricRPCProviderBlockNumber *prometheus.GaugeVec
	metricRPCProviderGasLimit    *prometheus.GaugeVec
}

func NewHealthcheckManager(config HealthcheckManagerConfig) *HealthcheckManager {
	healthCheckers := []Healthchecker{}
	rollingWindows := []*RollingWindowWrapper{}

	healthcheckManager := &HealthcheckManager{
		requestFailureThreshold:   config.Config.RollingWindowFailureThreshold,
		rollingWindowTaintEnabled: config.Config.RollingWindowTaintEnabled,
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
		metricResponseTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "zeroex_rpc_gateway_healthcheck_response_duration_seconds",
				Help: "Histogram of response time for Gateway Healthchecker in seconds",
				Buckets: []float64{
					.005,
					.01,
					.025,
					.05,
					.1,
					.25,
					.5,
					1,
					2.5,
					5,
					10,
				},
			}, []string{
				"provider",
				"method",
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
		healthchecker, err := NewHealthchecker(
			RPCHealthcheckerConfig{
				URL:              target.Connection.HTTP.URL,
				Name:             target.Name,
				Interval:         config.Config.Interval,
				Timeout:          config.Config.Timeout,
				FailureThreshold: config.Config.FailureThreshold,
				SuccessThreshold: config.Config.SuccessThreshold,
			})

		healthchecker.SetMetric(MetricBlockNumber, healthcheckManager.metricRPCProviderBlockNumber)
		healthchecker.SetMetric(MetricGasLimit, healthcheckManager.metricRPCProviderBlockNumber)
		healthchecker.SetMetric(MetricResponseTime, healthcheckManager.metricResponseTime)

		if err != nil {
			panic(err)
		}

		healthCheckers = append(healthCheckers, healthchecker)
		rollingWindows = append(rollingWindows, NewRollingWindowWrapper(target.Name, config.Config.RollingWindowSize))
	}

	healthcheckManager.healthcheckers = healthCheckers
	healthcheckManager.rollingWindows = rollingWindows

	return healthcheckManager
}

func (h *HealthcheckManager) runLoop(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			h.checkForFailingRequests()
			h.reportStatusMetrics()
		}
	}
}

func (h *HealthcheckManager) checkForFailingRequests() {
	if !h.rollingWindowTaintEnabled {
		return
	}
	for _, wrapper := range h.rollingWindows {
		rollingWindow := wrapper.rollingWindow
		if rollingWindow.HasEnoughObservations() {
			responseSuccessRate := rollingWindow.Avg()
			if responseSuccessRate < h.requestFailureThreshold {
				zap.L().Warn("RPC Success Rate falls below threshold", zap.String("name", wrapper.Name), zap.Float64("responseSuccessRate", responseSuccessRate))
				h.TaintTarget(wrapper.Name)
				rollingWindow.Reset()
			}
		}
	}
}

func (h *HealthcheckManager) reportStatusMetrics() {
	for _, healthchecker := range h.healthcheckers {
		healthy := 0
		tainted := 0
		if healthchecker.IsHealthy() {
			healthy = 1
		}
		if healthchecker.IsTainted() {
			tainted = 1
		}
		h.metricRPCProviderStatus.WithLabelValues(healthchecker.Name(), "healthy").Set(float64(healthy))
		h.metricRPCProviderStatus.WithLabelValues(healthchecker.Name(), "tainted").Set(float64(tainted))
	}
}

func (h *HealthcheckManager) Start(ctx context.Context) error {
	for index, healthChecker := range h.healthcheckers {
		h.metricRPCProviderInfo.WithLabelValues(strconv.Itoa(index), healthChecker.Name()).Set(1)
		go healthChecker.Start(ctx)
	}

	return h.runLoop(ctx)
}

func (h *HealthcheckManager) Stop(ctx context.Context) error {
	for _, healthChecker := range h.healthcheckers {
		err := healthChecker.Stop(ctx)
		if err != nil {
			zap.L().Error("healtchecker stop error", zap.Error(err))
		}
	}

	return nil
}

func (h *HealthcheckManager) GetTargetIndexByName(name string) int {
	for idx, healthChecker := range h.healthcheckers {
		if healthChecker.Name() == name {
			return idx
		}
	}

	zap.L().Error("tried to access a non-existing Healthchecker", zap.String("name", name))
	return 0
}

func (h *HealthcheckManager) GetTargetByName(name string) Healthchecker {
	for _, healthChecker := range h.healthcheckers {
		if healthChecker.Name() == name {
			return healthChecker
		}
	}

	zap.L().Error("tried to access a non-existing Healthchecker", zap.String("name", name))
	return nil
}

func (h *HealthcheckManager) TaintTarget(name string) {
	if healthChecker := h.GetTargetByName(name); healthChecker != nil {
		healthChecker.Taint()
		return
	}
}

func (h *HealthcheckManager) IsTargetHealthy(name string) bool {
	if healthChecker := h.GetTargetByName(name); healthChecker != nil {
		return healthChecker.IsHealthy()
	}

	return false
}

func (h *HealthcheckManager) GetNextHealthyTargetIndex() int {
	for idx, target := range h.healthcheckers {
		if target.IsHealthy() {
			return idx
		}
	}

	// no healthy targets, we down:(
	zap.L().Error("no more healthy targets")
	return 0
}

func (h *HealthcheckManager) GetNextHealthyTargetIndexExcluding(excludedIdx []uint) int {
	for idx, target := range h.healthcheckers {
		isExcluded := false
		for _, excludedIndex := range excludedIdx {
			if idx == int(excludedIndex) {
				isExcluded = true
				break
			}
		}

		if !isExcluded && target.IsHealthy() {
			return idx
		}
	}

	// no healthy targets, we down:(
	zap.L().Warn("no more healthy targets")
	return 0
}

func (h *HealthcheckManager) GetRollingWindowByName(name string) *RollingWindow {
	for _, wrapper := range h.rollingWindows {
		if wrapper.Name == name {
			return wrapper.rollingWindow
		}
	}

	panic("unknown rolling window")
}

func (h *HealthcheckManager) ObserveSuccess(name string) {
	rollingWindow := h.GetRollingWindowByName(name)
	rollingWindow.Observe(1)
}

func (h *HealthcheckManager) ObserveFailure(name string) {
	rollingWindow := h.GetRollingWindowByName(name)
	rollingWindow.Observe(0)
}
