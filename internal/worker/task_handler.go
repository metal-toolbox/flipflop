package worker

import (
	"context"
	"strings"
	"time"

	"github.com/metal-toolbox/ctrl"
	"github.com/metal-toolbox/flipflop/internal/device"
	"github.com/metal-toolbox/flipflop/internal/metrics"
	"github.com/metal-toolbox/flipflop/internal/model"
	rctypes "github.com/metal-toolbox/rivets/condition"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.hollow.sh/toolbox/events/registry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type TaskHandler struct {
	startTS     time.Time
	server      *model.Asset
	publisher   ctrl.Publisher
	task        *Task
	loggerEntry *logrus.Entry
	bmc         device.Queryor
}

func NewTaskHandler(task *Task, server *model.Asset, publisher ctrl.Publisher, bmc device.Queryor, controllerID registry.ControllerID, logger *logrus.Logger) *TaskHandler {
	taskHandler := &TaskHandler{
		server:    server,
		task:      task,
		publisher: publisher,
		bmc:       bmc,
	}

	taskHandler.loggerEntry = logger.WithFields(
		logrus.Fields{
			"controllerID": controllerID,
			"conditionID":  task.ID.String(),
			"serverID":     task.Parameters.AssetID.String(),
			"bmc":          server.BmcAddress.String(),
			"action":       task.Parameters.Action,
			"param":        task.Parameters.ActionParameter,
		},
	)

	return taskHandler
}

func (t *TaskHandler) Run(ctx context.Context) error {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"TaskHandler.Run",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	t.loggerEntry.Info("running condition action")

	err := t.publish(ctx, "running condition action", rctypes.Active)
	if err != nil {
		return err
	}

	if t.task.Fault != nil {
		err := t.fault()
		if err != nil {
			return t.failedWithError(ctx, "failed due to induced fault", err)
		}
	}

	switch t.task.Parameters.Action {
	case rctypes.GetPowerState:
		return t.powerState(ctx, t.bmc)
	case rctypes.PowerCycleBMC:
		return t.powerCycleBMC(ctx, t.bmc)
	case rctypes.SetPowerState:
		return t.setPowerState(ctx, t.bmc)
	case rctypes.SetNextBootDevice:
		return t.setNextBootDevice(ctx, t.bmc)
	default:
		return t.failedWithError(ctx, string(t.task.Parameters.Action), errUnsupportedAction)
	}
}

func (t *TaskHandler) powerState(ctx context.Context, bmc device.Queryor) error {
	state, err := bmc.GetPowerState(ctx)
	if err != nil {
		return t.failedWithError(ctx, "error identifying current power state", err)
	}

	return t.successful(ctx, state)
}

func (t *TaskHandler) powerCycleBMC(ctx context.Context, bmc device.Queryor) error {
	err := bmc.PowerCycleBMC(ctx)
	if err != nil {
		return t.failedWithError(ctx, "error power cycling BMC", err)
	}

	return t.successful(ctx, "BMC power cycled successfully")
}

func (t *TaskHandler) setPowerState(ctx context.Context, bmc device.Queryor) error {
	// identify current power state
	state, err := bmc.GetPowerState(ctx)
	if err != nil {
		return t.failedWithError(ctx, "error identifying current power state", err)
	}

	err = t.publishActive(ctx, "identified current power state: "+state)
	if err != nil {
		return err
	}

	// for a power cycle - if a server is powered off, invoke power on instead of cycle
	if t.task.Parameters.ActionParameter == "cycle" && strings.Contains(strings.ToLower(state), "off") {
		err = t.publishActive(ctx, "server was powered off, powering on")
		if err != nil {
			return err
		}

		err = bmc.SetPowerState(ctx, "on")
		if err != nil {
			return t.failedWithError(ctx, "server was powered off, failed to power on", err)
		}

		return t.successful(ctx, "server powered on successfully")
	}

	err = bmc.SetPowerState(ctx, t.task.Parameters.ActionParameter)
	if err != nil {
		return t.failedWithError(ctx, "", err)
	}

	return t.successful(ctx, "server power state set successful: "+t.task.Parameters.ActionParameter)
}

func (t *TaskHandler) setNextBootDevice(ctx context.Context, bmc device.Queryor) error {
	t.loggerEntry.WithFields(
		logrus.Fields{
			"persistent": t.task.Parameters.SetNextBootDevicePersistent,
			"efi":        t.task.Parameters.SetNextBootDeviceEFI,
		}).Info("setting next boot device to: " + t.task.Parameters.ActionParameter)

	err := bmc.SetBootDevice(ctx, t.task.Parameters.ActionParameter, t.task.Parameters.SetNextBootDevicePersistent, t.task.Parameters.SetNextBootDeviceEFI)
	if err != nil {
		return t.failedWithError(ctx, "error setting next boot device", err)
	}

	return t.successful(ctx, "next boot device set successfully: "+t.task.Parameters.ActionParameter)
}

func (t *TaskHandler) publish(ctx context.Context, status string, state rctypes.State) error {
	t.task.State = state
	t.task.Status.Append(status)

	genTask, err := t.task.ToGeneric()
	if err != nil {
		t.loggerEntry.WithError(errTaskConv).Error()
		return err
	}

	return t.publisher.Publish(ctx,
		genTask,
		false,
	)
}

func (t *TaskHandler) publishActive(ctx context.Context, status string) error {
	err := t.publish(ctx, status, rctypes.Active)
	if err != nil {
		t.loggerEntry.Infof("failed to publish condition status: %s", status)
		return err
	}

	t.loggerEntry.Infof("condition active: %s", status)
	return nil
}

// failed condition helper method
func (t *TaskHandler) failed(ctx context.Context, status string) error {
	err := t.publish(ctx, status, rctypes.Failed)

	t.registerConditionMetrics(string(rctypes.Failed))

	if err != nil {
		t.loggerEntry.Infof("failed to publish condition status: %s", status)
		return err
	}

	t.loggerEntry.Warnf("condition failed: %s", status)
	return nil
}

func (t *TaskHandler) failedWithError(ctx context.Context, status string, err error) error {
	newError := t.failed(ctx, errors.Wrap(err, status).Error())
	if newError != nil {
		if err != nil {
			return errors.Wrap(newError, err.Error())
		}

		return newError
	}

	return err
}

// successful condition helper method
func (t *TaskHandler) successful(ctx context.Context, status string) error {
	err := t.publish(ctx, status, rctypes.Succeeded)

	t.registerConditionMetrics(string(rctypes.Succeeded))

	if err != nil {
		t.loggerEntry.Warnf("failed to publish condition status: %s", status)
		return err
	}

	t.loggerEntry.Infof("condition complete: %s", status)
	return nil
}

func (t *TaskHandler) registerConditionMetrics(status string) {
	metrics.ConditionRunTimeSummary.With(
		prometheus.Labels{
			"condition": string(rctypes.ServerControl),
			"state":     status,
		},
	).Observe(time.Since(t.startTS).Seconds())
}

func (t *TaskHandler) fault() error {
	if t.task.Fault.FailAt != "" {
		return errors.New("condition induced fault")
	}

	if t.task.Fault.Panic {
		panic("fault induced panic")
	}

	d, err := time.ParseDuration(t.task.Fault.DelayDuration)
	if err == nil {
		time.Sleep(d)
	}

	return nil
}
