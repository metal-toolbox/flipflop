package flipflop

import (
	"context"
	"strings"
	"time"

	ctrl "github.com/metal-toolbox/ctrl"
	rctypes "github.com/metal-toolbox/rivets/v2/condition"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/metal-toolbox/flipflop/internal/app"
	"github.com/metal-toolbox/flipflop/internal/device"
	"github.com/metal-toolbox/flipflop/internal/metrics"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store"
)

type ConditionTaskHandler struct {
	logger       *logrus.Entry
	cfg          *app.Configuration
	bmc          device.Queryor
	store        store.Repository
	publisher    ctrl.Publisher
	server       *model.Asset
	task         *Task
	startTS      time.Time
	controllerID string
}

// return a live session to the BMC (or an error). The caller is responsible for closing the connection
func (cth *ConditionTaskHandler) openBMCConnection(ctx context.Context) error {
	var bmc device.Queryor
	if cth.cfg.Dryrun { // Fake BMC
		bmc = device.NewDryRunBMCClient(cth.server)
		cth.logger.Warn("using fake BMC")
	} else {
		bmc = device.NewBMCClient(cth.server, cth.logger)
	}

	err := bmc.Open(ctx)
	if err != nil {
		cth.logger.WithError(err).Error("bmc: failed to connect")
		return err
	}
	cth.bmc = bmc
	return nil
}

func (cth *ConditionTaskHandler) HandleTask(ctx context.Context, genTask *rctypes.Task[any, any], publisher ctrl.Publisher) error {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"flipflop.HandleTask",
	)
	defer span.End()

	cth.publisher = publisher

	// Ungeneric the task
	task, err := NewTask(genTask)
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

	if err := cth.openBMCConnection(ctx); err != nil {
		return err
	}

	defer func() {
		if err := cth.bmc.Close(ctx); err != nil {
			loggerEntry.WithError(err).Error("bmc connection close error")
		}
	}()

	return cth.Run(ctx)
}

func (cth *ConditionTaskHandler) Run(ctx context.Context) error {
	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"TaskHandler.Run",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	cth.startTS = time.Now()
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
		return cth.setPowerState(ctx, cth.task.Parameters.ActionParameter)
	case rctypes.SetNextBootDevice:
		return cth.setNextBootDevice(
			ctx,
			cth.task.Parameters.ActionParameter,
			cth.task.Parameters.SetNextBootDevicePersistent,
			cth.task.Parameters.SetNextBootDeviceEFI,
		)
	case rctypes.ValidateFirmware:
		return cth.validateFirmware(ctx)
	case rctypes.PxeBootPersistent:
		return cth.pxeBootPersistent(ctx)
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

func (cth *ConditionTaskHandler) setPowerState(ctx context.Context, newState string) error {
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
	if newState == "cycle" && strings.Contains(strings.ToLower(state), "off") {
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

	err = cth.bmc.SetPowerState(ctx, newState)
	if err != nil {
		return cth.failedWithError(ctx, "", err)
	}

	return cth.successful(ctx, "server power state set successful: "+cth.task.Parameters.ActionParameter)
}

func (cth *ConditionTaskHandler) setNextBootDevice(ctx context.Context, bootDevice string, persistent, efi bool) error {
	cth.logger.WithFields(
		logrus.Fields{
			"persistent": persistent,
			"efi":        efi,
		}).Info("setting next boot device to: " + bootDevice)

	err := cth.bmc.SetBootDevice(ctx, bootDevice, persistent, efi)
	if err != nil {
		return cth.failedWithError(ctx, "error setting next boot device", err)
	}

	return cth.successful(ctx, "next boot device set successfully: "+bootDevice)
}

func (cth *ConditionTaskHandler) validateFirmware(ctx context.Context) error {
	cth.logger.Info("starting firmware validation")

	deadline := time.Now().Add(cth.task.Parameters.ValidateFirmwareTimeout)

	// First reboot the BMC to ensure it's running the desired firmware
	if err := cth.bmc.PowerCycleBMC(ctx); err != nil {
		return cth.failedWithError(ctx, "failed to power cycle BMC", err)
	}

	cth.publishActive(ctx, "bmc power cycle complete")

	// Next we want to cycle the host, but the BMC will take some
	// time to reboot, so retry once every 30 seconds up to our
	// timeout deadline (ideally we'd have a way to distinguish
	// failures that are due to the BMC not being back online yet
	// from ones that aren't going to be resolved by waiting and
	// retrying...)
	bmcConnected := false
	var psErr error
	for time.Now().Before(deadline) {
		if errDelay := sleepInContext(ctx, 30*time.Second); errDelay != nil {
			return cth.failedWithError(context.Background(), "context error", errDelay)
		}

		if !bmcConnected {
			_ = cth.bmc.Close(ctx)
			if err := cth.openBMCConnection(ctx); err != nil {
				cth.logger.WithError(err).Warn("bmc: failed to connect")
				continue
			}
			bmcConnected = true
			// we have a deferred close for the BMC session queued up when we return from this function
		}

		newDeviceState := "cycle"
		currentState, err := cth.bmc.GetPowerState(ctx)
		if err != nil {
			cth.logger.WithError(err).Debug("getting device current power state")
			continue
		}

		if strings.Contains(strings.ToLower(currentState), "off") {
			newDeviceState = "on"
		}

		psErr = cth.bmc.SetPowerState(ctx, newDeviceState)
		if psErr == nil {
			cth.publishActive(ctx, "device power cycle complete")
			break
		}
		cth.logger.WithError(err).WithField("state", newDeviceState).Debug("bmc set power state")
	}

	if psErr != nil {
		return cth.failedWithError(ctx, "failed to cycle host power after BMC power cycle", psErr)
	}

	// Finally, wait for the host to boot successfully
	for time.Now().Before(deadline) {
		// sleep before checking to (hopefully) avoid seeing a
		// stale POST code from a previous boot before the
		// power-cycle has actually started happening
		if errDelay := sleepInContext(ctx, 30*time.Second); errDelay != nil {
			return cth.failedWithError(context.Background(), "failed to retrieve host boot status", errDelay)
		}

		booted, err := cth.bmc.HostBooted(ctx)
		if err != nil {
			return cth.failedWithError(ctx, "failed to retrieve host boot status", err)
		}
		if booted {
			done := time.Now()
			srvID := cth.task.Parameters.AssetID
			fwID := cth.task.Parameters.ValidateFirmwareID
			if dbErr := cth.store.ValidateFirmwareSet(ctx, srvID, fwID, done); dbErr != nil {
				return cth.failedWithError(ctx, "marking firmware set validated", dbErr)
			}
			return cth.successful(ctx, "firmware set validated: "+fwID.String())
		}
	}

	return cth.failed(ctx, "host failed to boot successfully before deadline")
}

func sleepInContext(ctx context.Context, t time.Duration) error {
	select {
	case <-time.After(t):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pxeBootPersistent sets up the server to pxe boot persistently
func (cth *ConditionTaskHandler) pxeBootPersistent(ctx context.Context) error {
	if err := cth.setNextBootDevice(ctx, "pxe", true, true); err != nil {
		return err
	}

	return cth.bmc.SetPowerState(ctx, "on")
}

func (cth *ConditionTaskHandler) publish(ctx context.Context, status string, state rctypes.State) error {
	cth.task.State = state
	cth.task.Status.Append(status)

	genTask, err := cth.task.ToGeneric()
	if err != nil {
		cth.logger.WithError(errTaskConv).Error()
		return err
	}

	if errDelay := sleepInContext(ctx, 10*time.Second); errDelay != nil {
		return context.Canceled
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
