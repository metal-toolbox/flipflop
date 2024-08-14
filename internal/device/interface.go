package device

import (
	"context"
)

// Queryor interface abstracts calls to remote devices
type Queryor interface {
	Open(ctx context.Context) error
	Close(ctx context.Context) error
	GetPowerState(ctx context.Context) (state string, err error)
	SetPowerState(ctx context.Context, state string) error
	SetBootDevice(ctx context.Context, device string, persistent, efiBoot bool) error
	GetBootDevice(ctx context.Context) (device string, persistent, efiBoot bool, err error)
	PowerCycleBMC(ctx context.Context) error
}
