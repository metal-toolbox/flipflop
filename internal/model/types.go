package model

import (
	"context"
	"net"

	"github.com/google/uuid"
	"github.com/stmcginnis/gofish/redfish"
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

// UpdateFn is a function that publishes the given string as a condition status message
type UpdateFn func(string)

type DelayFn func(context.Context) error

type OpenCloser interface {
	Open(context.Context) error
	Close(context.Context) error
}

type BMCResetter interface {
	BmcReset(context.Context, string) (bool, error)
}

type PowerMonitor interface {
	PowerStateGet(context.Context) (string, error)
	PowerSet(context.Context, string) (bool, error)
}

type BootMonitor interface {
	GetBootProgress() (*redfish.BootProgress, error)
	BootComplete() (bool, error)
}

type BMCBootMonitor interface {
	OpenCloser
	BMCResetter
	BootMonitor
	PowerMonitor
}
