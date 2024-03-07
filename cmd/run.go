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
	"go.hollow.sh/toolbox/events"

	// nolint:gosec // profiling endpoint listens on localhost.
	_ "net/http/pprof"
)

var cmdRun = &cobra.Command{
	Use:   "run",
	Short: "Run flipflop service to listen for events on NATS and execute on serverState Conditions",
	Run: func(cmd *cobra.Command, args []string) {
		runWorker(cmd.Context())
	},
}

// run worker command
var (
	dryrun         bool
	faultInjection bool
	facilityCode   string
	storeKind      string
	replicas       int
)

var (
	ErrInventoryStore = errors.New("inventory store error")
)

func runWorker(ctx context.Context) {
	theApp, termCh, err := app.New(
		model.StoreKind(storeKind),
		cfgFile,
		logLevel,
		enableProfiling,
	)
	if err != nil {
		log.Fatal(err)
	}

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
		theApp.Config.StoreKind,
		theApp.Config,
		theApp.Logger,
	)
	if err != nil {
		theApp.Logger.Fatal(err)
	}

	stream, err := events.NewStream(*theApp.Config.NatsOptions)
	if err != nil {
		theApp.Logger.Fatal(err)
	}

	w := flipflop.New(
		facilityCode,
		dryrun,
		faultInjection,
		theApp.Config.Concurrency,
		replicas,
		stream,
		inv,
		theApp.Logger,
	)

	w.Run(ctx)
}

func init() {
	cmdRun.PersistentFlags().StringVar(&storeKind, "store", "", "Inventory store to lookup asset credentials - fleetdb")
	cmdRun.PersistentFlags().BoolVarP(&dryrun, "dry-run", "", false, "In dryrun mode, the worker actions the task without installing firmware")
	cmdRun.PersistentFlags().BoolVarP(&faultInjection, "fault-injection", "", false, "Tasks can include a Fault attribute to allow fault injection for development purposes")
	cmdRun.PersistentFlags().IntVarP(&replicas, "replica-count", "r", 3, "The number of replicas to use for NATS data")
	cmdRun.PersistentFlags().StringVar(&facilityCode, "facility-code", "", "The facility code this flipflop instance is associated with")

	if err := cmdRun.MarkPersistentFlagRequired("facility-code"); err != nil {
		log.Fatal(err)
	}

	if err := cmdRun.MarkPersistentFlagRequired("store"); err != nil {
		log.Fatal(err)
	}

	rootCmd.AddCommand(cmdRun)
}
