package main

import (
	"context"

	"go.uber.org/zap"
)

type HealthcheckManagerConfig struct {
	Targets []TargetConfig
	Config  HealthCheckConfig
}

type HealthcheckManager struct {
	healthcheckers []Healthchecker
}

func NewHealthcheckManager(config HealthcheckManagerConfig) *HealthcheckManager {
	healthCheckers := []Healthchecker{}
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

	}
	return &HealthcheckManager{
		healthcheckers: healthCheckers,
	}
}

func (h *HealthcheckManager) Start(ctx context.Context) error {
	for _, healthChecker := range h.healthcheckers {
		go healthChecker.Start(ctx)
	}

	return nil
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

func (h *HealthcheckManager) GetTargetByName(name string) Healthchecker {
	for _, healthChecker := range h.healthcheckers {
		if healthChecker.Name() == name {
			return healthChecker
		}
	}

	zap.L().Error("tried to access a non-existing Healthchecker", zap.String("name", name))
	return nil
}

func (h *HealthcheckManager) SetTargetTaint(name string, isTainted bool) {
	if healthChecker := h.GetTargetByName(name); healthChecker != nil {
		healthChecker.SetTaint(isTainted)
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
			gatewayFailover.Set(float64(idx))
			return idx
		}
	}

	// no healthy targets, we down:(
	zap.L().Error("no more healthy targets")
	return 0
}
