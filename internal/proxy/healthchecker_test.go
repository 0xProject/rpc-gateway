package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/caitlinelfring/go-env-default"
	"github.com/stretchr/testify/assert"
)

// TestBasicHealthchecker checks if it runs with default options.
func TestBasicHealthchecker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	healtcheckConfig := HealthCheckerConfig{
		URL:              env.GetDefault("RPC_GATEWAY_NODE_URL_1", "https://cloudflare-eth.com"),
		Interval:         1 * time.Second,
		Timeout:          2 * time.Second,
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	healthchecker, err := NewHealthChecker(healtcheckConfig)
	assert.NoError(t, err)

	healthchecker.Start(ctx)

	assert.NotZero(t, healthchecker.BlockNumber())

	// TODO: can be flaky due to cloudflare-eth endpoint
	assert.True(t, healthchecker.IsHealthy())

	healthchecker.isHealthy = false
	assert.False(t, healthchecker.IsHealthy())

	healthchecker.isHealthy = true
	assert.True(t, healthchecker.IsHealthy())
}

func TestGasLeftCall(t *testing.T) {
	client := &http.Client{}
	url := env.GetDefault("RPC_GATEWAY_NODE_URL_1", "https://cloudflare-eth.com")

	result, err := performGasLeftCall(context.TODO(), client, url)
	assert.NoError(t, err)
	assert.NotZero(t, result)

	// testing the timeout
	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelFunc()

	_, err = performGasLeftCall(ctx, client, url)
	assert.Error(t, err)
}
