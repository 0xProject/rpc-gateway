package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestBasicHealthchecker checks if it runs with default options. It outputs
func TestBasicHealthchecker(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	// We replace the global logger with this initialized here for simplyfication.
	// Do see: https://github.com/uber-go/zap/blob/master/FAQ.md#why-include-package-global-loggers
	// ref: https://pkg.go.dev/go.uber.org/zap?utm_source=godoc#ReplaceGlobals
	zap.ReplaceGlobals(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	healtcheckConfig := RPCHealthcheckerConfig{
		URL:              "https://cloudflare-eth.com",
		Interval:         1 * time.Second,
		Timeout:          2 * time.Second,
		FailureThreshold: 1,
		SuccessThreshold: 1,
	}

	healthchecker, err := NewHealthchecker(healtcheckConfig)
	if err != nil {
		t.Fatal(err)
	}

	healthchecker.Start(ctx)

	if !(healthchecker.BlockNumber() > 0) {
		t.Fatal("Healthchecker did not update the blockNumber")
	}

	// TODO: can be flaky due to cloudflare-eth endpoint
	if healthchecker.IsHealthy() == false {
		t.Fatal("Healthchecker by default should be healthy after running for a bit")
	}

	healthchecker.Taint()
	if healthchecker.IsHealthy() == true {
		t.Fatal("Should be unhealthy if taint is set to true")
	}

	healthchecker.RemoveTaint()
	if healthchecker.IsHealthy() == false {
		t.Fatal("Should be healthy after the taint is removed")
	}
}

func TestGasLeftCall(t *testing.T) {
	client := &http.Client{}
	url := "https://cloudflare-eth.com"

	result, err := performGasLeftCall(context.TODO(), client, url)
	if err != nil {
		t.Fatal(err)
	}

	if result == 0 {
		t.Fatal("received gas limit equal to 0")
	}

	// testing the timeout
	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelFunc()
	_, err = performGasLeftCall(ctx, client, url)
	if err == nil {
		t.Fatal("expected the performGasLeftCall to timeout")
	}

}
