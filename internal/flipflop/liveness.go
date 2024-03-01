package flipflop

import (
	"context"
	"sync"
	"time"

	"go.hollow.sh/toolbox/events"
	"go.hollow.sh/toolbox/events/pkg/kv"
	"go.hollow.sh/toolbox/events/registry"

	"github.com/nats-io/nats.go"
)

var (
	once           sync.Once
	checkinCadence = 30 * time.Second
	livenessTTL    = 3 * time.Minute
)

// This starts a go-routine to peridocally check in with the NATS kv
func (f *flipflop) startflipflopLivenessCheckin(ctx context.Context) {
	once.Do(func() {
		f.id = registry.GetID(f.name)
		natsJS, ok := f.stream.(*events.NatsJetstream)
		if !ok {
			f.logger.Error("Non-NATS stores are not supported for worker-liveness")
			return
		}

		opts := []kv.Option{
			kv.WithTTL(livenessTTL),
		}

		// any setting of replicas (even 1) chokes NATS in non-clustered mode
		if f.replicaCount != 1 {
			opts = append(opts, kv.WithReplicas(f.replicaCount))
		}

		if err := registry.InitializeRegistryWithOptions(natsJS, opts...); err != nil {
			f.logger.WithError(err).Error("unable to initialize active worker registry")
			return
		}

		go f.checkinRoutine(ctx)
	})
}

func (f *flipflop) checkinRoutine(ctx context.Context) {
	if err := registry.RegisterController(f.id); err != nil {
		f.logger.WithError(err).Warn("unable to do initial worker liveness registration")
	}

	tick := time.NewTicker(checkinCadence)
	defer tick.Stop()

	var stop bool
	for !stop {
		select {
		case <-tick.C:
			err := registry.ControllerCheckin(f.id)
			switch err {
			case nil:
			case nats.ErrKeyNotFound: // generally means NATS reaped our entry on TTL
				if err = registry.RegisterController(f.id); err != nil {
					f.logger.WithError(err).
						WithField("id", f.id.String()).
						Warn("unable to re-register worker")
				}
			default:
				f.logger.WithError(err).
					WithField("id", f.id.String()).
					Warn("worker checkin failed")
			}
		case <-ctx.Done():
			f.logger.Info("liveness check-in stopping on done context")
			stop = true
		}
	}
}
