package store

import (
	"context"

	"github.com/metal-toolbox/flipflop/internal/app"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store/fleetdb"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Repository interface {
	// AssetByID returns asset based on the identifier.
	AssetByID(ctx context.Context, assetID string) (*model.Asset, error)
}

func NewRepository(ctx context.Context, storeKind model.StoreKind, appKind model.AppKind, cfg *app.Configuration, logger *logrus.Logger) (Repository, error) {
	switch storeKind {
	case model.FleetDB:
		return fleetdb.New(ctx, appKind, cfg.FleetDBOptions, logger)
	case model.MockDB:
	}

	return nil, errors.New("unsupported store kind: " + string(storeKind))
}
