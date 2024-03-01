package flipflop

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/metal-toolbox/flipflop/internal/device"
	"github.com/metal-toolbox/flipflop/internal/metrics"
	"github.com/metal-toolbox/flipflop/internal/model"
	rctypes "github.com/metal-toolbox/rivets/condition"
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.hollow.sh/toolbox/events"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type taskState int

const (
	// conditionInprogressTicker is the interval at which condtion in progress
	// will ack themselves as in progress on the event stream.
	//
	// This value should be set to less than the event stream Ack timeout value.
	conditionInprogressTick = 3 * time.Minute

	notStarted    taskState = iota
	inProgress              // another flipflop has started it, is still around and updated recently
	complete                // task is done
	orphaned                // the flipflop that started this task doesn't exist anymore
	indeterminate           // we got an error in the process of making the check
)

func (f *flipflop) processEvents(ctx context.Context) {
	// XXX: consider having a separate context for message retrieval
	msgs, err := f.stream.PullMsg(ctx, 1)
	switch {
	case err == nil:
	case errors.Is(err, nats.ErrTimeout):
		f.logger.WithFields(
			logrus.Fields{"err": err.Error()},
		).Trace("no new events")
	default:
		f.logger.WithFields(
			logrus.Fields{"err": err.Error()},
		).Warn("retrieving new messages")
		metrics.NATSError("pull-msg")
	}

	for _, msg := range msgs {
		if ctx.Err() != nil || f.concurrencyLimit() {
			f.eventNak(msg)

			return
		}

		// spawn msg process handler
		f.syncWG.Add(1)

		go func(msg events.Message) {
			defer f.syncWG.Done()

			atomic.AddInt32(&f.dispatched, 1)
			defer atomic.AddInt32(&f.dispatched, -1)

			f.processSingleEvent(ctx, msg)
		}(msg)
	}
}

func (f *flipflop) concurrencyLimit() bool {
	return int(f.dispatched) >= f.concurrency
}

func (f *flipflop) eventAckInProgress(event events.Message) {
	if err := event.InProgress(); err != nil {
		metrics.NATSError("ack-in-progress")
		f.logger.WithError(err).Warn("event Ack Inprogress error")
	}
}

func (f *flipflop) eventAckComplete(event events.Message) {
	if err := event.Ack(); err != nil {
		f.logger.WithError(err).Warn("event Ack error")
	}
}

func (f *flipflop) eventNak(event events.Message) {
	if err := event.Nak(); err != nil {
		metrics.NATSError("nak")
		f.logger.WithError(err).Warn("event Nak error")
	}
}

func (f *flipflop) registerEventCounter(valid bool, response string) {
	metrics.EventsCounter.With(
		prometheus.Labels{
			"valid":    strconv.FormatBool(valid),
			"response": response,
		}).Inc()
}

func (f *flipflop) processSingleEvent(ctx context.Context, e events.Message) {
	// extract parent trace context from the event if any.
	ctx = e.ExtractOtelTraceContext(ctx)

	ctx, span := otel.Tracer(pkgName).Start(
		ctx,
		"flipflop.processSingleEvent",
	)
	defer span.End()

	// parse condition from event
	condition, err := conditionFromEvent(e)
	if err != nil {
		f.logger.WithError(err).WithField(
			"subject", e.Subject()).Warn("unable to retrieve condition from message")

		f.registerEventCounter(false, "ack")
		f.eventAckComplete(e)

		return
	}

	// parse parameters from condition
	params, err := f.parametersFromCondition(condition)
	if err != nil {
		f.logger.WithError(err).WithField(
			"subject", e.Subject()).Warn("unable to retrieve parameters from condition")

		f.registerEventCounter(false, "ack")
		f.eventAckComplete(e)

		return
	}

	// fetch server from store
	server, err := f.store.AssetByID(ctx, params.AssetID.String())
	if err != nil {
		f.logger.WithFields(logrus.Fields{
			"ServerID":    params.AssetID.String(),
			"conditionID": condition.ID,
			"err":         err.Error(),
		}).Warn("cookie lookup error")

		f.registerEventCounter(true, "nack")
		f.eventNak(e) // have the message bus re-deliver the message
		metrics.RegisterSpanEvent(
			span,
			condition,
			f.id.String(),
			params.AssetID.String(),
			"sent nack, store query error",
		)

		return
	}

	flipCtx, cancel := context.WithTimeout(ctx, conditionTimeout)
	defer cancel()

	defer f.registerEventCounter(true, "ack")
	defer f.eventAckComplete(e)
	metrics.RegisterSpanEvent(
		span,
		condition,
		f.id.String(),
		params.AssetID.String(),
		"sent ack, condition fulfilled",
	)

	f.serverActionWithMonitor(flipCtx, server, condition, params, e)
}

func conditionFromEvent(e events.Message) (*rctypes.Condition, error) {
	data := e.Data()
	if data == nil {
		return nil, errors.New("data field empty")
	}

	condition := &rctypes.Condition{}
	if err := json.Unmarshal(data, condition); err != nil {
		return nil, errors.Wrap(errConditionDeserialize, err.Error())
	}

	return condition, nil
}

func (f *flipflop) parametersFromCondition(condition *rctypes.Condition) (*rctypes.ServerControlTaskParameters, error) {
	errParameters := errors.New("condition parameters error")

	parameters := &rctypes.ServerControlTaskParameters{}
	if err := json.Unmarshal(condition.Parameters, parameters); err != nil {
		return nil, errors.Wrap(errParameters, err.Error())
	}

	return parameters, nil
}

func (f *flipflop) serverActionWithMonitor(ctx context.Context, server *model.Asset, condition *rctypes.Condition, params *rctypes.ServerControlTaskParameters, e events.Message) {
	// the runTask method is expected to close this channel to indicate its done
	doneCh := make(chan bool)

	// monitor sends in progress ack's until the cookie flipflop method returns.
	monitor := func() {
		defer f.syncWG.Done()

		ticker := time.NewTicker(conditionInprogressTick)
		defer ticker.Stop()

	Loop:
		for {
			select {
			case <-ticker.C:
				f.eventAckInProgress(e)
			case <-doneCh:
				break Loop
			}
		}
	}

	f.syncWG.Add(1)

	go monitor()

	f.serverAction(ctx, server, condition, params, doneCh)

	<-doneCh
}

// where the action actually happens
func (f *flipflop) serverAction(ctx context.Context, server *model.Asset, condition *rctypes.Condition, params *rctypes.ServerControlTaskParameters, doneCh chan bool) {
	defer close(doneCh)
	startTS := time.Now()

	f.logger.WithFields(logrus.Fields{
		"ServerID":    server.ID,
		"conditionID": condition.ID,
	}).Info("actioning condition for server")

	f.publishStatus(
		ctx,
		server.ID.String(),
		rctypes.Active,
		"actioning condition for server",
	)

	failed := func(cause string) {
		f.publishStatus(
			ctx,
			server.ID.String(),
			rctypes.Failed,
			cause,
		)

		f.logger.WithFields(logrus.Fields{
			"ServerID":    server.ID,
			"conditionID": condition.ID,
			"action":      params.Action,
		}).Warn("condition failed")

		f.registerConditionMetrics(startTS, string(rctypes.Failed))
	}

	if condition.Fault != nil {
		if err := f.faultHandler(condition.Fault); err != nil {
			failed("failed due to induced fault")
			return
		}
	}

	// prepare logger
	l := logrus.New()
	l.Formatter = f.logger.Formatter
	l.Level = f.logger.Level
	lWithEntry := l.WithFields(
		logrus.Fields{
			"workerID":    f.id.String(),
			"conditionID": condition.ID.String(),
			"serverID":    server.ID.String(),
			"bmc":         server.BmcAddress.String(),
		},
	)

	client := device.NewBMCClient(server, lWithEntry)

	// TODO
	errUnsupported := errors.New("unsupported action")
	switch params.Action {
	case rctypes.GetPowerState:
		if state, err := client.GetPowerState(ctx); err != nil {

		}
	case rctypes.PowerCycleBMC:
		client.PowerCycleBMC(ctx)
	case rctypes.SetPowerState:
		client.SetPowerState(ctx, params.ActionParameter)
	case rctypes.SetNextBootDevice:
		client.SetBootDevice(ctx, params.ActionParameter, params.SetNextBootDevicePersistent, params.SetNextBootDeviceEFI)
	default:
		failed(errors.Wrap(errUnsupported, string(params.Action)).Error())
		return
	}

	f.registerConditionMetrics(startTS, string(rctypes.Succeeded))

	f.logger.WithFields(logrus.Fields{
		"ServerID":    server.ID,
		"conditionID": condition.ID,
		"action":      params.Action,
	}).Info("condition completed")
}

func (f *flipflop) faultHandler(fault *rctypes.Fault) error {
	if fault.FailAt != "" {
		return errors.New("condition induced fault")
	}

	if fault.Panic {
		panic("fault induced panic")
	}

	d, err := time.ParseDuration(fault.DelayDuration)
	if err == nil {
		time.Sleep(d)
	}

	return nil
}

func (f *flipflop) publishStatus(ctx context.Context, ServerID string, state rctypes.State, status string) []byte {
	sv := &rctypes.StatusValue{
		WorkerID:  f.id.String(),
		Target:    ServerID,
		TraceID:   trace.SpanFromContext(ctx).SpanContext().TraceID().String(),
		SpanID:    trace.SpanFromContext(ctx).SpanContext().SpanID().String(),
		State:     string(state),
		Status:    statusInfoJSON(status),
		UpdatedAt: time.Now(),
	}

	return sv.MustBytes()
}

func statusInfoJSON(s string) json.RawMessage {
	return []byte(fmt.Sprintf("{%q: %q}", "msg", s))
}

func (f *flipflop) registerConditionMetrics(startTS time.Time, state string) {
	metrics.ConditionRunTimeSummary.With(
		prometheus.Labels{
			"condition": string(rctypes.FirmwareInstall),
			"state":     state,
		},
	).Observe(time.Since(startTS).Seconds())
}
