package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store/fleetdb"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Repository interface {
	// AssetByID returns asset based on the identifier.
	AssetByID(ctx context.Context, assetID string) (*model.Asset, error)
	// ValidateFirmwareSet marks the set as successfully tested
	ValidateFirmwareSet(context.Context, uuid.UUID, uuid.UUID, time.Time) error
}

func NewRepository(ctx context.Context, storeKind model.StoreKind, cfg *fleetdb.Config, logger *logrus.Logger) (Repository, error) {
	switch storeKind {
	case model.FleetDB:
		return fleetdb.New(ctx, cfg, logger)
	case model.MockDB:
	}

	return nil, errors.New("unsupported store kind: " + string(storeKind))
}
