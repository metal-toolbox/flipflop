package flipflop

import (
	"context"
	"strings"
	"time"

	"github.com/metal-toolbox/flipflop/internal/device"
	"github.com/metal-toolbox/flipflop/internal/metrics"
	"github.com/metal-toolbox/flipflop/internal/model"
	rctypes "github.com/metal-toolbox/rivets/condition"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// handler executes actions
type handler struct {
	startTS     time.Time
	server      *model.Asset
	condition   *rctypes.Condition
	publisher   *statusPublisher
	params      *rctypes.ServerControlTaskParameters
	loggerEntry *logrus.Entry
	kvLastRev   uint64
}

func (h *handler) run(ctx context.Context, controllerID string, l *logrus.Logger, doneCh chan bool) {
	defer close(doneCh)
	h.startTS = time.Now()

	// prepare new logger with set fields
	n := logrus.New()
	n.Formatter = l.Formatter
	n.Level = l.Level
	h.loggerEntry = l.WithFields(
		logrus.Fields{
			"controllerID": controllerID,
			"conditionID":  h.condition.ID.String(),
			"serverID":     h.server.ID.String(),
			"bmc":          h.server.BmcAddress.String(),
			"action":       h.params.Action,
			"param":        h.params.ActionParameter,
		},
	)

	h.loggerEntry.Info("running condition")

	h.kvLastRev = h.publisher.Publish(
		ctx,
		h.server.ID.String(),
		h.condition.ID.String(),
		"running condition action",
		rctypes.Active,
		h.kvLastRev,
	)

	if h.condition.Fault != nil {
		if err := h.fault(); err != nil {
			h.failed(ctx, "failed due to induced fault")
			return
		}
	}

	bmc := device.NewBMCClient(h.server, h.loggerEntry)
	if err := bmc.Open(ctx); err != nil {
		h.failed(ctx, errors.Wrap(err, "bmc connection open error").Error())
		return
	}

	// always close bmc connection
	defer func() {
		if err := bmc.Close(ctx); err != nil {
			h.loggerEntry.WithError(err).Error("bmc connection close error")
		}
	}()

	errUnsupported := errors.New("unsupported action")
	switch h.params.Action {
	case rctypes.GetPowerState:
		h.powerState(ctx, bmc)
		return

	case rctypes.PowerCycleBMC:
		h.powerCycleBMC(ctx, bmc)
		return

	case rctypes.SetPowerState:
		h.setPowerState(ctx, bmc)
		return

	case rctypes.SetNextBootDevice:
		h.setNextBootDevice(ctx, bmc)
		return

	default:
		h.failed(ctx, errors.Wrap(errUnsupported, string(h.params.Action)).Error())
		return
	}
}

func (h *handler) powerState(ctx context.Context, bmc device.Queryor) {
	state, err := bmc.GetPowerState(ctx)
	if err != nil {
		h.failed(ctx, errors.Wrap(err, "error identifying current power state").Error())
		return
	}

	h.successful(ctx, state)
}

func (h *handler) powerCycleBMC(ctx context.Context, bmc device.Queryor) {
	if err := bmc.PowerCycleBMC(ctx); err != nil {
		h.failed(ctx, errors.Wrap(err, "error power cycling BMC").Error())
		return
	}

	h.successful(ctx, "BMC power cycled successfully")
}

func (h *handler) setPowerState(ctx context.Context, bmc device.Queryor) {
	// identify current power state
	state, err := bmc.GetPowerState(ctx)
	if err != nil {
		h.failed(ctx, errors.Wrap(err, "error identifying current power state").Error())
		return
	}

	h.publishActive(ctx, "identified current power state: "+state)

	// for a power cycle - if a server is powered off, invoke power on instead of cycle
	if h.params.ActionParameter == "cycle" && strings.Contains(strings.ToLower(state), "off") {
		h.publishActive(ctx, "server was powered off, powering on")

		if err := bmc.SetPowerState(ctx, "on"); err != nil {
			h.failed(ctx, errors.Wrap(err, "server was powered off, failed to power on").Error())
			return
		}

		h.successful(ctx, "server powered on successfully")
		return
	}

	if err := bmc.SetPowerState(ctx, h.params.ActionParameter); err != nil {
		h.failed(ctx, err.Error())
		return
	}

	h.successful(ctx, "server power state set successful: "+h.params.ActionParameter)
}

func (h *handler) setNextBootDevice(ctx context.Context, bmc device.Queryor) {
	h.loggerEntry.WithFields(
		logrus.Fields{
			"persistent": h.params.SetNextBootDevicePersistent,
			"efi":        h.params.SetNextBootDeviceEFI,
		}).Info("setting next boot device to: " + h.params.ActionParameter)

	err := bmc.SetBootDevice(ctx, h.params.ActionParameter, h.params.SetNextBootDevicePersistent, h.params.SetNextBootDeviceEFI)
	if err != nil {
		h.failed(ctx, errors.Wrap(err, "error setting next boot device").Error())
		return
	}

	h.successful(ctx, "next boot device set successfully: "+h.params.ActionParameter)
}

func (h *handler) publish(ctx context.Context, state rctypes.State, info string) {
	h.kvLastRev = h.publisher.Publish(
		ctx,
		h.server.ID.String(),
		h.condition.ID.String(),
		info,
		state,
		h.kvLastRev,
	)
}

func (h *handler) publishActive(ctx context.Context, info string) {
	h.loggerEntry.Info(info)
	h.publish(ctx, rctypes.Active, info)
}

// failed condition helper method
func (h *handler) failed(ctx context.Context, cause string) {
	h.publish(ctx, rctypes.Failed, cause)
	h.loggerEntry.Warn("condition failed")
	h.registerConditionMetrics(string(rctypes.Failed))
}

// successful condition helper method
func (h *handler) successful(ctx context.Context, status string) {
	h.publish(ctx, rctypes.Succeeded, status)
	h.loggerEntry.Info("condition complete")
	h.registerConditionMetrics(string(rctypes.Succeeded))
}

func (h *handler) registerConditionMetrics(state string) {
	metrics.ConditionRunTimeSummary.With(
		prometheus.Labels{
			"condition": string(rctypes.ServerControl),
			"state":     state,
		},
	).Observe(time.Since(h.startTS).Seconds())
}

func (h *handler) fault() error {
	if h.condition.Fault.FailAt != "" {
		return errors.New("condition induced fault")
	}

	if h.condition.Fault.Panic {
		panic("fault induced panic")
	}

	d, err := time.ParseDuration(h.condition.Fault.DelayDuration)
	if err == nil {
		time.Sleep(d)
	}

	return nil
}
