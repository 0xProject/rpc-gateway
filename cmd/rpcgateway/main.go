package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xProject/rpc-gateway/internal/rpcgateway"
	"github.com/carlmjohnson/flowmatic"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func main() {
	c, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
	//
	zap.ReplaceGlobals(logger)
	defer func() {
		err := logger.Sync() // flushes buffer, if any
		if err != nil {
			logger.Error("failed to flush logger with err: %s", zap.Error(err))
		}
	}()

	// Initialize config
	configFileLocation := flag.String("config", "./config.yml", "path to rpc gateway config file")
	flag.Parse()
	config, err := rpcgateway.NewRPCGatewayFromConfigFile(*configFileLocation)
	if err != nil {
		logger.Fatal("failed to get config", zap.Error(err))
	}

	service := rpcgateway.NewRPCGateway(*config)

	err = flowmatic.Do(
		func() error {
			return errors.Wrap(service.Start(c), "cannot start a service")
		},
		func() error {
			<-c.Done()

			return errors.Wrap(service.Stop(c), "cannot stop a service")
		},
	)

	if err != nil {
		logger.Fatal("errors", zap.Error(err))
	}
}
