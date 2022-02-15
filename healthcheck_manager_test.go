package main

import (
	"context"
	"testing"
	"time"
)

func TestHealthcheckManager(t *testing.T) {
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
			Interval:         500 * time.Millisecond,
			Timeout:          200 * time.Millisecond,
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

	manager.SetTargetTaint("Cloudflare", true)
	nextIdx = manager.GetNextHealthyTargetIndex()
	if nextIdx != 1 {
		t.Fatal("did not handle the taint well")
	}

	manager.Stop(ctx)
}
