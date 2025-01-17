package flipflop

import (
	"context"
	"os"

	ctrl "github.com/metal-toolbox/ctrl"
	rctypes "github.com/metal-toolbox/rivets/v2/condition"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"

	"github.com/metal-toolbox/flipflop/internal/app"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store"
	"github.com/metal-toolbox/flipflop/internal/version"
)

const (
	pkgName = "internal/flipflop"
)

// flipflop holds attributes to run a cookie flipflop instance
type flipflop struct {
	logger *logrus.Logger
	cfg    *app.Configuration
	store  store.Repository
	name   string
}

// New returns a cookie flipflop
//
// nolint:revive // unexported type is not annoying to use
func New(
	repository store.Repository,
	logger *logrus.Logger,
	cfg *app.Configuration,
) *flipflop {
	name, _ := os.Hostname()

	return &flipflop{
		name:   name,
		store:  repository,
		cfg:    cfg,
		logger: logger,
	}
}

func (f *flipflop) Run(ctx context.Context) {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"flipflop.Run",
	)
	defer span.End()

	v := version.Current()
	loggerEntry := f.logger.WithFields(
		logrus.Fields{
			"version":        v.AppVersion,
			"commit":         v.GitCommit,
			"branch":         v.GitBranch,
			"dry-run":        f.cfg.Dryrun,
			"faultInjection": f.cfg.FaultInjection,
		},
	)
	loggerEntry.Info("flipflop running")

	nc := ctrl.NewNatsController(
		model.AppName,
		f.cfg.FacilityCode,
		string(rctypes.ServerControl),
		f.cfg.Endpoints.Nats.URL,
		f.cfg.Endpoints.Nats.CredsFile,
		rctypes.ServerControl,
		ctrl.WithConcurrency(f.cfg.Concurrency),
		ctrl.WithKVReplicas(f.cfg.Endpoints.Nats.KVReplicationFactor),
		ctrl.WithLogger(f.logger),
		ctrl.WithConnectionTimeout(f.cfg.Endpoints.Nats.ConnectTimeout),
	)

	err := nc.Connect(ctx)
	if err != nil {
		f.logger.Fatal(err)
	}

	handlerFactory := func() ctrl.TaskHandler {
		return &ConditionTaskHandler{
			cfg:          f.cfg,
			logger:       loggerEntry,
			controllerID: nc.ID(),
			store:        f.store,
		}
	}

	err = nc.ListenEvents(ctx, handlerFactory)
	if err != nil {
		f.logger.Fatal(err)
	}
}
