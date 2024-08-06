package flipflop

import (
	"context"
	"os"

	ctrl "github.com/metal-toolbox/ctrl"
	"github.com/metal-toolbox/flipflop/internal/app"
	"github.com/metal-toolbox/flipflop/internal/device"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store"
	"github.com/metal-toolbox/flipflop/internal/version"
	"github.com/metal-toolbox/flipflop/internal/worker"
	rctypes "github.com/metal-toolbox/rivets/condition"
	"github.com/sirupsen/logrus"
	"go.hollow.sh/toolbox/events"
	"go.hollow.sh/toolbox/events/registry"
	"go.opentelemetry.io/otel"
)

const (
	pkgName = "internal/worker"
)

// flipflop holds attributes to run a cookie flipflop instance
type flipflop struct {
	logger         *logrus.Logger
	cfg            *app.Configuration
	stream         events.Stream
	store          store.Repository
	controllerID   registry.ControllerID // assigned when this worker registers itself
	name           string
	facilityCode   string
	dryrun         bool
	faultInjection bool
}

// New returns a cookie flipflop
//
// nolint:revive // unexported type is not annoying to use
func New(
	facilityCode string,
	dryrun,
	faultInjection bool,
	stream events.Stream,
	repository store.Repository,
	logger *logrus.Logger,
	cfg *app.Configuration,
) *flipflop {
	name, _ := os.Hostname()

	return &flipflop{
		name:           name,
		facilityCode:   facilityCode,
		dryrun:         dryrun,
		faultInjection: faultInjection,
		stream:         stream,
		store:          repository,
		logger:         logger,
		cfg:            cfg,
	}
}

func (f *flipflop) Run(ctx context.Context) {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"flipflop.Run",
	)
	defer span.End()

	v := version.Current()
	logEntry := f.logger.WithFields(
		logrus.Fields{
			"version":        v.AppVersion,
			"commit":         v.GitCommit,
			"branch":         v.GitBranch,
			"dry-run":        f.dryrun,
			"faultInjection": f.faultInjection,
		},
	)
	logEntry.Info("flipflip running")

	handlerFactory := func() ctrl.TaskHandler {
		return f
	}

	nats := ctrl.NewNatsController(
		model.AppName,
		f.facilityCode,
		string(rctypes.ServerControl),
		f.cfg.NatsOptions.URL,
		f.cfg.NatsOptions.CredsFile,
		rctypes.ServerControl,
		ctrl.WithConcurrency(f.cfg.Concurrency),
		ctrl.WithKVReplicas(f.cfg.NatsOptions.KVReplicationFactor),
		ctrl.WithLogger(f.logger),
		ctrl.WithConnectionTimeout(f.cfg.NatsOptions.ConnectTimeout),
	)

	err := nats.Connect(ctx)
	if err != nil {
		f.logger.Fatal(err)
	}

	err = nats.ListenEvents(ctx, handlerFactory)
	if err != nil {
		f.logger.Fatal(err)
	}
}

func (f *flipflop) HandleTask(ctx context.Context, genTask *rctypes.Task[any, any], publisher ctrl.Publisher) error {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"flipflop.HandleTask",
	)
	defer span.End()

	// Ungeneric the task
	task, err := worker.NewTask(genTask, f.faultInjection)
	if err != nil {
		f.logger.WithFields(logrus.Fields{
			"conditionID":  genTask.ID,
			"controllerID": f.controllerID,
			"err":          err.Error(),
		}).Error("asset lookup error")
		return err
	}

	// Get Server
	server, err := f.store.AssetByID(ctx, task.Parameters.AssetID.String())
	if err != nil {
		f.logger.WithFields(logrus.Fields{
			"assetID":      task.Parameters.AssetID.String(),
			"conditionID":  task.ID,
			"controllerID": f.controllerID,
			"err":          err.Error(),
		}).Error("asset lookup error")

		return ctrl.ErrRetryHandler
	}

	loggerEntry := f.logger.WithFields(
		logrus.Fields{
			"controllerID": f.controllerID,
			"conditionID":  task.ID.String(),
			"serverID":     server.ID.String(),
			"bmc":          server.BmcAddress.String(),
			"action":       task.Parameters.Action,
			"param":        task.Parameters.ActionParameter,
		},
	)

	var bmc device.Queryor
	if f.dryrun { // Fake BMC
		bmc = device.NewDryRunBMCClient("on")
	} else {
		bmc = device.NewBMCClient(server, loggerEntry)
	}

	err = bmc.Open(ctx)
	if err != nil {
		loggerEntry.WithError(err).Error("bmc connection failed to connect")
		return err
	}
	defer func() {
		if err := bmc.Close(ctx); err != nil {
			loggerEntry.WithError(err).Error("bmc connection close error")
		}
	}()

	handler := worker.NewTaskHandler(task, server, publisher, bmc, f.controllerID, f.logger)

	return handler.Run(ctx)
}
