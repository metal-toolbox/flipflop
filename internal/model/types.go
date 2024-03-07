package model

import (
	"net"

	"github.com/google/uuid"
)

type (
	StoreKind string
	// LogLevel is the logging level string.
	LogLevel string
)

const (
	AppName = "flipflop"

	InventoryStoreYAML StoreKind = "yaml"
	FleetDB            StoreKind = "fleetdb"
	MockDB             StoreKind = "mockdb"

	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
	LogLevelTrace LogLevel = "trace"
)

// StoreKinds returns the supported asset inventory, firmware configuration sources
func StoreKinds() []StoreKind {
	return []StoreKind{InventoryStoreYAML, FleetDB}
}

// nolint:govet // prefer to keep field ordering as is
type Asset struct {
	ID uuid.UUID

	// Device BMC attributes
	BmcAddress  net.IP
	BmcUsername string
	BmcPassword string

	// Manufacturer attributes
	Vendor string
	Model  string
	Serial string

	// Facility this Asset is hosted in.
	FacilityCode string
}
