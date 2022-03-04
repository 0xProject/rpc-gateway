package main

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type HealthcheckManagerConfig struct {
	Targets []TargetConfig
	Config  HealthCheckConfig
}

func NewRollingWindowWrapper(name string, windowSize int) *rollingWindowWrapper {
	return &rollingWindowWrapper{
		Name:          name,
		rollingWindow: NewRollingWindow(windowSize),
	}
}

type rollingWindowWrapper struct {
	rollingWindow *RollingWindow
	Name          string
}

type HealthcheckManager struct {
	healthcheckers []Healthchecker
	rollingWindows []*rollingWindowWrapper

	requestFailureThreshold float64
}

func NewHealthcheckManager(config HealthcheckManagerConfig) *HealthcheckManager {
	healthCheckers := []Healthchecker{}
	rollingWindows := []*rollingWindowWrapper{}

	for _, target := range config.Targets {
		healthchecker, err := NewHealthchecker(RPCHealthcheckerConfig{
			URL:              target.Connection.HTTP.URL,
			Name:             target.Name,
			Interval:         config.Config.Interval,
			Timeout:          config.Config.Timeout,
			FailureThreshold: config.Config.FailureThreshold,
			SuccessThreshold: config.Config.SuccessThreshold,
		})

		if err != nil {
			panic(err)
		}

		healthCheckers = append(healthCheckers, healthchecker)
		rollingWindows = append(rollingWindows, NewRollingWindowWrapper(target.Name, config.Config.RollingWindowSize))

	}
	return &HealthcheckManager{
		healthcheckers:          healthCheckers,
		rollingWindows:          rollingWindows,
		requestFailureThreshold: config.Config.RollingWindowFailureThreshold,
	}
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
		rpcProviderStatus.WithLabelValues(healthchecker.Name(), "healthy").Set(float64(healthy))
		rpcProviderStatus.WithLabelValues(healthchecker.Name(), "tainted").Set(float64(tainted))
	}
}

func (h *HealthcheckManager) Start(ctx context.Context) error {
	for _, healthChecker := range h.healthcheckers {
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
