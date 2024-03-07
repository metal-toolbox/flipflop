package flipflop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/metal-toolbox/flipflop/internal/metrics"
	rctypes "github.com/metal-toolbox/rivets/condition"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"go.hollow.sh/toolbox/events"
	"go.hollow.sh/toolbox/events/pkg/kv"
	"go.hollow.sh/toolbox/events/registry"
)

var (
	statusKVName  = string(rctypes.ServerControl)
	defaultKVOpts = []kv.Option{
		kv.WithDescription("flipflop condition status tracking"),
		kv.WithTTL(10 * 24 * time.Hour),
	}
)

type statusPublisher struct {
	kv           nats.KeyValue
	log          *logrus.Logger
	facilityCode string
	controllerID registry.ControllerID
}

func newStatusKVPublisher(s events.Stream, replicaCount int, facility string, controllerID registry.ControllerID, log *logrus.Logger) *statusPublisher {
	var opts []kv.Option
	if replicaCount > 1 {
		opts = append(opts, kv.WithReplicas(replicaCount))
	}

	js, ok := s.(*events.NatsJetstream)
	if !ok {
		log.Fatal("status-kv publisher is only supported on NATS")
	}

	kvOpts := defaultKVOpts
	kvOpts = append(kvOpts, opts...)

	statusKV, err := kv.CreateOrBindKVBucket(js, statusKVName, kvOpts...)
	if err != nil {
		log.WithError(err).Fatal("unable to bind status KV bucket")
	}

	return &statusPublisher{
		kv:           statusKV,
		facilityCode: facility,
		log:          log,
		controllerID: controllerID,
	}
}

func statusInfoJSON(s string) json.RawMessage {
	return []byte(fmt.Sprintf("{%q: %q}", "msg", s))
}

// Publish publishes the condition status
func (s *statusPublisher) Publish(ctx context.Context, serverID, conditionID, status string, state rctypes.State, lastRevision uint64) (revision uint64) {
	_, span := otel.Tracer(pkgName).Start(
		ctx,
		"worker.Publish.KV",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	sv := &rctypes.StatusValue{
		WorkerID:  s.controllerID.String(),
		Target:    serverID,
		TraceID:   trace.SpanFromContext(ctx).SpanContext().TraceID().String(),
		SpanID:    trace.SpanFromContext(ctx).SpanContext().SpanID().String(),
		State:     string(state),
		Status:    statusInfoJSON(status),
		UpdatedAt: time.Now(),
	}

	payload := sv.MustBytes()

	facility := "facility"
	if s.facilityCode != "" {
		facility = s.facilityCode
	}

	key := fmt.Sprintf("%s.%s", facility, conditionID)

	var err error
	if lastRevision == 0 {
		revision, err = s.kv.Create(key, payload)
	} else {
		revision, err = s.kv.Update(key, payload, lastRevision)
	}

	if err != nil {
		metrics.NATSError("publish-condition-status")
		span.AddEvent("status publish failure",
			trace.WithAttributes(
				attribute.String("controllerID", s.controllerID.String()),
				attribute.String("serverID", serverID),
				attribute.String("conditionID", conditionID),
				attribute.String("error", err.Error()),
			),
		)
		s.log.WithError(err).WithFields(logrus.Fields{
			"controllerID":      s.controllerID,
			"serverID":          serverID,
			"assetFacilityCode": s.facilityCode,
			"conditionID":       conditionID,
			"lastRev":           lastRevision,
		}).Warn("unable to write condition status")
		return
	}

	s.log.WithFields(logrus.Fields{
		"controllerID":      s.controllerID,
		"serverID":          serverID,
		"assetFacilityCode": s.facilityCode,
		"conditionID":       conditionID,
		"lastRev":           lastRevision,
	}).Trace("published condition status")

	return revision
}
