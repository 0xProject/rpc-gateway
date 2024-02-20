package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xProject/rpc-gateway/internal/rpcgateway"
	"github.com/carlmjohnson/flowmatic"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

func main() {
	c, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := &cli.App{
		Name:  "rpc-gateway",
		Usage: "The failover proxy for node providers.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config",
				Usage:    "The configuration file path.",
				Required: true,
			},
		},
		Action: func(cc *cli.Context) error {
			config, err := rpcgateway.NewRPCGatewayFromConfigFile(cc.String("config"))
			if err != nil {
				return err
			}

			service, err := rpcgateway.NewRPCGateway(*config)
			if err != nil {
				return errors.Wrap(err, "rpc-gateway failed")
			}

			return flowmatic.Do(
				func() error {
					return errors.Wrap(service.Start(c), "cannot start a service")
				},
				func() error {
					<-c.Done()

					return errors.Wrap(service.Stop(c), "cannot stop a service")
				},
			)
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
	}
}
