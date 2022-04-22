package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func main() {
	topCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	debugLogEnabled := os.Getenv("DEBUG") == "true"
	logLevel := zap.WarnLevel
	if debugLogEnabled {
		logLevel = zap.DebugLevel
	}
	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(logLevel)
	logger, _ := zapConfig.Build()
	// We replace the global logger with this initialized here for simplyfication.
	// Do see: https://github.com/uber-go/zap/blob/master/FAQ.md#why-include-package-global-loggers
	// ref: https://pkg.go.dev/go.uber.org/zap?utm_source=godoc#ReplaceGlobals
	zap.ReplaceGlobals(logger)
	defer func() {
		err := logger.Sync() // flushes buffer, if any
		if err != nil {
			logger.Error("failed to flush logger with err: %s", zap.Error(err))
		}
	}()

	g, gCtx := errgroup.WithContext(topCtx)

	// Initialize config
	configFileLocation := flag.String("config", "./config.yml", "path to rpc gateway config file")
	flag.Parse()
	config, err := NewRpcGatewayFromConfigFile(*configFileLocation)
	if err != nil {
		logger.Fatal("failed to get config", zap.Error(err))
	}

	// start gateway
	rpcGateway := NewRpcGateway(*config)

	// start healthz and metrics server
	metricsServer := NewMetricsServer(config.Metrics)
	g.Go(func() error {
		return metricsServer.Start()
	})

	g.Go(func() error {
		return rpcGateway.Start(context.TODO())
	})

	g.Go(func() error {
		<-gCtx.Done()
		err := metricsServer.Stop()
		if err != nil {
			logger.Error("error when stopping healthserver", zap.Error(err))
		}
		err = rpcGateway.Stop(context.TODO())
		if err != nil {
			logger.Error("error when stopping rpc gateway", zap.Error(err))
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		fmt.Printf("exit reason: %s \n", err)
	}
}
