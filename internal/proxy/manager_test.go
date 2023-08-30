package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
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
	assert.Zero(t, nextIdx)

	time.Sleep(1 * time.Second)

	manager.TaintTarget("AnkrOne")

	nextIdx = manager.GetNextHealthyTargetIndex()
	assert.Equal(t, 1, nextIdx)

	manager.Stop(ctx)
}

func TestGetNextHealthyTargetIndexExcluding(t *testing.T) {
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
	defer manager.Stop(ctx)

	manager.GetTargetByName("AnkrOne").Taint()

	assert.Equal(t, -1, manager.GetNextHealthyTargetIndexExcluding([]uint{}))

	assert.Equal(t, -1, manager.GetNextHealthyTargetIndexExcluding([]uint{0}))

	manager.GetTargetByName("AnkrOne").RemoveTaint()

	assert.Equal(t, 0, manager.GetNextHealthyTargetIndexExcluding([]uint{}))
}
