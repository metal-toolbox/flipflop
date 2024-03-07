package flipflop

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/metal-toolbox/flipflop/internal/metrics"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store"
	rctypes "github.com/metal-toolbox/rivets/condition"
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.hollow.sh/toolbox/events"
	"go.hollow.sh/toolbox/events/registry"
	"go.opentelemetry.io/otel"
)

const (
	pkgName = "internal/worker"
)

var (
	fetchEventsInterval = 10 * time.Second

	// conditionTimeout defines the time after which the condition execution will be canceled.
	conditionTimeout = 180 * time.Minute

	// conditionInprogressTicker is the interval at which condtion in progress
	// will ack themselves as in progress on the event stream.
	//
	// This value should be set to less than the event stream Ack timeout value.
	conditionInprogressTick = 3 * time.Minute

	errConditionDeserialize = errors.New("unable to deserialize condition")
)

// flipflop holds attributes to run a cookie flipflop instance
type flipflop struct {
	stream         events.Stream
	store          store.Repository
	syncWG         *sync.WaitGroup
	logger         *logrus.Logger
	name           string
	controllerID   registry.ControllerID // assigned when this worker registers itself
	facilityCode   string
	concurrency    int
	dispatched     int32
	dryrun         bool
	faultInjection bool
	replicaCount   int
}

// New returns a cookie flipflop
func New(
	facilityCode string,
	dryrun,
	faultInjection bool,
	concurrency,
	replicaCount int,
	stream events.Stream,
	repository store.Repository,
	logger *logrus.Logger,
) *flipflop {
	name, _ := os.Hostname()

	return &flipflop{
		name:           name,
		facilityCode:   facilityCode,
		dryrun:         dryrun,
		faultInjection: faultInjection,
		concurrency:    concurrency,
		replicaCount:   replicaCount,
		syncWG:         &sync.WaitGroup{},
		stream:         stream,
		store:          repository,
		logger:         logger,
	}
}

// Run runs the firmware install worker which listens for events to action.
func (f *flipflop) Run(ctx context.Context) {
	tickerFetchEvents := time.NewTicker(fetchEventsInterval).C

	if err := f.stream.Open(); err != nil {
		f.logger.WithError(err).Error("event stream connection error")
		return
	}

	// returned channel ignored, since this is a Pull based subscription.
	_, err := f.stream.Subscribe(ctx)
	if err != nil {
		f.logger.WithError(err).Error("event stream subscription error")
		return
	}

	f.logger.Info("connected to event stream.")

	f.startflipflopLivenessCheckin(ctx)

	f.logger.WithFields(
		logrus.Fields{
			"replica-count":   f.replicaCount,
			"concurrency":     f.concurrency,
			"dry-run":         f.dryrun,
			"fault-injection": f.faultInjection,
		},
	).Info("flipflop running")

Loop:
	for {
		select {
		case <-tickerFetchEvents:
			if f.concurrencyLimit() {
				continue
			}

			f.processEvents(ctx)

		case <-ctx.Done():
			if f.dispatched > 0 {
				continue
			}

			break Loop
		}
	}
}

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
			f.controllerID.String(),
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
		f.controllerID.String(),
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

	publisher := newStatusKVPublisher(f.stream, f.replicaCount, f.facilityCode, f.controllerID, f.logger)
	h := &handler{
		server:    server,
		condition: condition,
		publisher: publisher,
		params:    params,
	}

	h.run(ctx, f.controllerID.String(), f.logger, doneCh)
	<-doneCh
}
