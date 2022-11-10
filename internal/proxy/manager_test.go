package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/0xProject/rpc-gateway/internal/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestHealthcheckManager(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	manager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: []TargetConfig{
			{
				Name: "Primary",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: util.Getenv("RPC_GATEWAY_NODE_URL_1", "https://cloudflare-eth.com"),
					},
				},
			},
			{
				Name: "StandBy",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: util.Getenv("RPC_GATEWAY_NODE_URL_2", "https://rpc.ankr.com/eth"),
					},
				},
			},
		},

		Config: HealthCheckConfig{
			Interval:         1 * time.Second,
			Timeout:          1 * time.Second,
			FailureThreshold: 1,
			SuccessThreshold: 1,
		},
	})

	ctx := context.TODO()
	go manager.Start(ctx)

	time.Sleep(1 * time.Second)

	manager.TaintTarget("Primary")

	manager.Stop(ctx)
}

func TestGetNextHealthyTargetIndexExcluding(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	manager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: []TargetConfig{
			{
				Name: "Primary",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: util.Getenv("RPC_GATEWAY_NODE_URL_1", "https://cloudflare-eth.com"),
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

	manager.GetTargetByName("Primary").Taint()

	assert.Equal(t, -1, manager.GetNextHealthyTargetIndexExcluding([]uint{}))

	assert.Equal(t, -1, manager.GetNextHealthyTargetIndexExcluding([]uint{0}))

	manager.GetTargetByName("Primary").RemoveTaint()

	assert.Equal(t, 0, manager.GetNextHealthyTargetIndexExcluding([]uint{}))
}
