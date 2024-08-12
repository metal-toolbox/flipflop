package flipflop

import (
	"context"
	"strings"
	"time"

	ctrl "github.com/metal-toolbox/ctrl"
	"github.com/metal-toolbox/flipflop/internal/app"
	"github.com/metal-toolbox/flipflop/internal/device"
	"github.com/metal-toolbox/flipflop/internal/metrics"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store"
	rctypes "github.com/metal-toolbox/rivets/condition"
	"github.com/metal-toolbox/rivets/events"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type ConditionTaskHandler struct {
	logger       *logrus.Entry
	cfg          *app.Configuration
	bmc          device.Queryor
	stream       events.Stream
	store        store.Repository
	publisher    ctrl.Publisher
	server       *model.Asset
	task         *Task
	startTS      time.Time
	controllerID string
}

func (cth *ConditionTaskHandler) HandleTask(ctx context.Context, genTask *rctypes.Task[any, any], publisher ctrl.Publisher) error {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"flipflop.HandleTask",
	)
	defer span.End()

	cth.publisher = publisher

	// Ungeneric the task
	task, err := NewTask(genTask, cth.cfg.FaultInjection)
	if err != nil {
		cth.logger.WithFields(logrus.Fields{
			"conditionID":  genTask.ID,
			"controllerID": cth.controllerID,
			"err":          err.Error(),
		}).Error("asset lookup error")
		return err
	}
	cth.task = task

	// Get Server
	server, err := cth.store.AssetByID(ctx, task.Parameters.AssetID.String())
	if err != nil {
		cth.logger.WithFields(logrus.Fields{
			"assetID":      task.Parameters.AssetID.String(),
			"conditionID":  task.ID,
			"controllerID": cth.controllerID,
			"err":          err.Error(),
		}).Error("asset lookup error")

		return ctrl.ErrRetryHandler
	}
	cth.server = server

	loggerEntry := cth.logger.WithFields(
		logrus.Fields{
			"controllerID": cth.controllerID,
			"conditionID":  task.ID.String(),
			"serverID":     server.ID.String(),
			"bmc":          server.BmcAddress.String(),
			"action":       task.Parameters.Action,
			"param":        task.Parameters.ActionParameter,
		},
	)
	cth.logger = loggerEntry

	var bmc device.Queryor
	if cth.cfg.Dryrun { // Fake BMC
		bmc = device.NewDryRunBMCClient("on")
		loggerEntry.Warn("Running BMC Device in Dryrun mode")
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
	cth.bmc = bmc

	return cth.Run(ctx)
}

func (cth *ConditionTaskHandler) Run(ctx context.Context) error {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"TaskHandler.Run",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	cth.logger.Info("running condition action")

	err := cth.publishActive(ctx, "running condition action")
	if err != nil {
		return err
	}

	if cth.task.Fault != nil {
		err := cth.fault()
		if err != nil {
			return cth.failedWithError(ctx, "failed due to induced fault", err)
		}
	}

	switch cth.task.Parameters.Action {
	case rctypes.GetPowerState:
		return cth.powerState(ctx)
	case rctypes.PowerCycleBMC:
		return cth.powerCycleBMC(ctx)
	case rctypes.SetPowerState:
		return cth.setPowerState(ctx)
	case rctypes.SetNextBootDevice:
		return cth.setNextBootDevice(ctx)
	default:
		return cth.failedWithError(ctx, string(cth.task.Parameters.Action), errUnsupportedAction)
	}
}

func (cth *ConditionTaskHandler) powerState(ctx context.Context) error {
	state, err := cth.bmc.GetPowerState(ctx)
	if err != nil {
		return cth.failedWithError(ctx, "error identifying current power state", err)
	}

	return cth.successful(ctx, state)
}

func (cth *ConditionTaskHandler) powerCycleBMC(ctx context.Context) error {
	err := cth.bmc.PowerCycleBMC(ctx)
	if err != nil {
		return cth.failedWithError(ctx, "error power cycling BMC", err)
	}

	return cth.successful(ctx, "BMC power cycled successfully")
}

func (cth *ConditionTaskHandler) setPowerState(ctx context.Context) error {
	// identify current power state
	state, err := cth.bmc.GetPowerState(ctx)
	if err != nil {
		return cth.failedWithError(ctx, "error identifying current power state", err)
	}

	err = cth.publishActive(ctx, "identified current power state: "+state)
	if err != nil {
		return err
	}

	// for a power cycle - if a server is powered off, invoke power on instead of cycle
	if cth.task.Parameters.ActionParameter == "cycle" && strings.Contains(strings.ToLower(state), "off") {
		err = cth.publishActive(ctx, "server was powered off, powering on")
		if err != nil {
			return err
		}

		err = cth.bmc.SetPowerState(ctx, "on")
		if err != nil {
			return cth.failedWithError(ctx, "server was powered off, failed to power on", err)
		}

		return cth.successful(ctx, "server powered on successfully")
	}

	err = cth.bmc.SetPowerState(ctx, cth.task.Parameters.ActionParameter)
	if err != nil {
		return cth.failedWithError(ctx, "", err)
	}

	return cth.successful(ctx, "server power state set successful: "+cth.task.Parameters.ActionParameter)
}

func (cth *ConditionTaskHandler) setNextBootDevice(ctx context.Context) error {
	cth.logger.WithFields(
		logrus.Fields{
			"persistent": cth.task.Parameters.SetNextBootDevicePersistent,
			"efi":        cth.task.Parameters.SetNextBootDeviceEFI,
		}).Info("setting next boot device to: " + cth.task.Parameters.ActionParameter)

	err := cth.bmc.SetBootDevice(ctx, cth.task.Parameters.ActionParameter, cth.task.Parameters.SetNextBootDevicePersistent, cth.task.Parameters.SetNextBootDeviceEFI)
	if err != nil {
		return cth.failedWithError(ctx, "error setting next boot device", err)
	}

	return cth.successful(ctx, "next boot device set successfully: "+cth.task.Parameters.ActionParameter)
}

func (cth *ConditionTaskHandler) publish(ctx context.Context, status string, state rctypes.State) error {
	cth.task.State = state
	cth.task.Status.Append(status)

	genTask, err := cth.task.ToGeneric()
	if err != nil {
		cth.logger.WithError(errTaskConv).Error()
		return err
	}

	return cth.publisher.Publish(ctx,
		genTask,
		false,
	)
}

func (cth *ConditionTaskHandler) publishActive(ctx context.Context, status string) error {
	err := cth.publish(ctx, status, rctypes.Active)
	if err != nil {
		cth.logger.Infof("failed to publish condition status: %s", status)
		return err
	}

	cth.logger.Infof("condition active: %s", status)
	return nil
}

// failed condition helper method
func (cth *ConditionTaskHandler) failed(ctx context.Context, status string) error {
	err := cth.publish(ctx, status, rctypes.Failed)

	cth.registerConditionMetrics(string(rctypes.Failed))

	if err != nil {
		cth.logger.Infof("failed to publish condition status: %s", status)
		return err
	}

	cth.logger.Warnf("condition failed: %s", status)
	return nil
}

func (cth *ConditionTaskHandler) failedWithError(ctx context.Context, status string, err error) error {
	newError := cth.failed(ctx, errors.Wrap(err, status).Error())
	if newError != nil {
		if err != nil {
			return errors.Wrap(newError, err.Error())
		}

		return newError
	}

	return err
}

// successful condition helper method
func (cth *ConditionTaskHandler) successful(ctx context.Context, status string) error {
	err := cth.publish(ctx, status, rctypes.Succeeded)

	cth.registerConditionMetrics(string(rctypes.Succeeded))

	if err != nil {
		cth.logger.Warnf("failed to publish condition status: %s", status)
		return err
	}

	cth.logger.Infof("condition complete: %s", status)
	return nil
}

func (cth *ConditionTaskHandler) registerConditionMetrics(status string) {
	metrics.ConditionRunTimeSummary.With(
		prometheus.Labels{
			"condition": string(rctypes.ServerControl),
			"state":     status,
		},
	).Observe(time.Since(cth.startTS).Seconds())
}

func (cth *ConditionTaskHandler) fault() error {
	if cth.task.Fault.FailAt != "" {
		return errors.New("condition induced fault")
	}

	if cth.task.Fault.Panic {
		panic("fault induced panic")
	}

	d, err := time.ParseDuration(cth.task.Fault.DelayDuration)
	if err == nil {
		time.Sleep(d)
	}

	return nil
}
