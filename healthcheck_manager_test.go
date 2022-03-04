package main

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestHealthcheckManager(t *testing.T) {
	// initial setup
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)

	manager := NewHealthcheckManager(HealthcheckManagerConfig{
		Targets: []TargetConfig{
			TargetConfig{
				Name: "Cloudflare",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://cloudflare-eth.com",
					},
				},
			},
			TargetConfig{
				Name: "CloudflareTwo",
				Connection: TargetConfigConnection{
					HTTP: TargetConnectionHTTP{
						URL: "https://cloudflare-eth.com",
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

	manager.TaintTarget("Cloudflare")
	nextIdx = manager.GetNextHealthyTargetIndex()
	if nextIdx != 1 {
		t.Fatal("did not handle the taint well")
	}

	manager.Stop(ctx)
}
