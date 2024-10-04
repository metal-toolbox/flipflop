package cmd

import (
	"context"
	"log"

	"github.com/equinix-labs/otel-init-go/otelinit"
	"github.com/metal-toolbox/flipflop/internal/app"
	"github.com/metal-toolbox/flipflop/internal/flipflop"
	"github.com/metal-toolbox/flipflop/internal/metrics"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store"
	"github.com/metal-toolbox/flipflop/internal/version"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	// nolint:gosec // profiling endpoint listens on localhost.
	_ "net/http/pprof"
)

var cmdRun = &cobra.Command{
	Use:   "run",
	Short: "Run flipflop service to listen for events on NATS and execute on serverState Conditions",
	Run: func(cmd *cobra.Command, _ []string) {
		runWorker(cmd.Context())
	},
}

// run worker command
var (
	dryrun         bool
	faultInjection bool
)

var (
	ErrInventoryStore = errors.New("inventory store error")
)

func runWorker(ctx context.Context) {
	theApp, termCh, err := app.New(
		cfgFile,
		logLevel,
		enableProfiling,
	)
	if err != nil {
		log.Fatal(err)
	}

	// Override config with command line args
	theApp.Config.Dryrun = dryrun
	theApp.Config.FaultInjection = faultInjection

	// serve metrics endpoint
	metrics.ListenAndServe()
	version.ExportBuildInfoMetric()

	ctx, otelShutdown := otelinit.InitOpenTelemetry(ctx, "flipflop")
	defer otelShutdown(ctx)

	// Setup cancel context with cancel func.
	ctx, cancelFunc := context.WithCancel(ctx)

	// routine listens for termination signal and cancels the context
	go func() {
		<-termCh
		theApp.Logger.Info("got TERM signal, exiting...")
		cancelFunc()
	}()

	inv, err := store.NewRepository(
		ctx,
		model.FleetDB,
		&theApp.Config.Endpoints.FleetDB,
		theApp.Logger,
	)
	if err != nil {
		theApp.Logger.Fatal(err)
	}

	ff := flipflop.New(
		inv,
		theApp.Logger,
		theApp.Config,
	)

	ff.Run(ctx)
}

func init() {
	cmdRun.PersistentFlags().BoolVarP(&dryrun, "dry-run", "", false, "In dryrun mode, the worker actions the task without installing firmware")
	cmdRun.PersistentFlags().BoolVarP(&faultInjection, "fault-injection", "", false, "Tasks can include a Fault attribute to allow fault injection for development purposes")
	rootCmd.AddCommand(cmdRun)
}
