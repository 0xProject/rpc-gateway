package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestHealthcheckManager(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	manager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: []TargetConfig{
			{
				Name: "AnkrOne",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://rpc.ankr.com/eth",
					},
				},
			},
			{
				Name: "AnkrTwo",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://rpc.ankr.com/eth",
					},
				},
			},
		},

		Config: HealthCheckConfig{
			Interval:         200 * time.Millisecond,
			Timeout:          2000 * time.Millisecond,
			FailureThreshold: 1,
			SuccessThreshold: 1,
		},
	})

	ctx := context.TODO()
	go manager.Start(ctx)

	nextIdx := manager.GetNextHealthyTargetIndex()
	if nextIdx != 0 {
		t.Fatal("first index is not zero")
	}
	time.Sleep(1 * time.Second)

	manager.TaintTarget("AnkrOne")
	nextIdx = manager.GetNextHealthyTargetIndex()
	if nextIdx != 1 {
		t.Fatal("did not handle the taint well")
	}

	manager.Stop(ctx)
}

func TestHealthcheckManagerRollingWindowTaintEnabled(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	manager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: []TargetConfig{
			{
				Name: "AnkrOne",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://rpc.ankr.com/eth",
					},
				},
			},
			{
				Name: "AnkrTwo",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://rpc.ankr.com/eth",
					},
				},
			},
		},

		Config: HealthCheckConfig{
			Interval:                      200 * time.Millisecond,
			Timeout:                       2000 * time.Millisecond,
			FailureThreshold:              1,
			SuccessThreshold:              1,
			RollingWindowTaintEnabled:     true,
			RollingWindowSize:             2,
			RollingWindowFailureThreshold: 0.9,
		},
	})

	ctx := context.TODO()
	go manager.Start(ctx)

	// Make the first RPC Provider observed 100% failure (window size is set to 2 above)
	manager.GetRollingWindowByName("AnkrOne").Observe(0)
	manager.GetRollingWindowByName("AnkrOne").Observe(0)

	// Wait for 1.5 second so 1 runLoop has been run
	time.Sleep(1500 * time.Millisecond)

	want := true
	got := manager.GetTargetByName("AnkrOne").IsTainted()

	if want != got {
		t.Error("The healthcheck manager tainted the target while it should not when RollingWindowTaintEnabled is set to false")
	}

	manager.Stop(ctx)
}

func TestHealthcheckManagerRollingWindowTaintDisabled(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	manager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: []TargetConfig{
			{
				Name: "AnkrOne",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://rpc.ankr.com/eth",
					},
				},
			},
			{
				Name: "AnkrTwo",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://rpc.ankr.com/eth",
					},
				},
			},
		},

		Config: HealthCheckConfig{
			Interval:                      200 * time.Millisecond,
			Timeout:                       2000 * time.Millisecond,
			FailureThreshold:              1,
			SuccessThreshold:              1,
			RollingWindowTaintEnabled:     false,
			RollingWindowSize:             2,
			RollingWindowFailureThreshold: 0.9,
		},
	})

	ctx := context.TODO()
	go manager.Start(ctx)

	// Make the first RPC Provider observed 100% failure (window size is set to 2 above)
	manager.GetRollingWindowByName("AnkrOne").Observe(0)
	manager.GetRollingWindowByName("AnkrOne").Observe(0)

	// Wait for 1.5 second so 1 runLoop has been run
	time.Sleep(1500 * time.Millisecond)

	want := false
	got := manager.GetTargetByName("AnkrOne").IsTainted()

	if want != got {
		t.Error("The healthcheck manager tainted the target while it should not when RollingWindowTaintEnabled is set to false")
	}

	manager.Stop(ctx)
}
