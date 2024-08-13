package device

import (
	"context"
	"errors"

	"github.com/metal-toolbox/flipflop/internal/model"
)

type serverState struct {
	power      string
	bootDevice string
}

var (
	errBmcCantFindServer = errors.New("dryrun BMC couldnt find server to set state")
	serverStates         = make(map[string]serverState)
)

// Bmc is an implementation of the Queryor interface
type DryRunBMC struct {
	id string
}

func NewDryRunBMCClient(asset *model.Asset) Queryor {
	state, ok := serverStates[asset.ID.String()]
	if !ok {
		state.power = "on"
		state.bootDevice = "disk"
		serverStates[asset.ID.String()] = state
	}

	return &DryRunBMC{
		asset.ID.String(),
	}
}

// Open creates a BMC session
func (b *DryRunBMC) Open(_ context.Context) error {
	return nil
}

// Close logs out of the BMC
func (b *DryRunBMC) Close(_ context.Context) error {
	return nil
}

// GetPowerState returns the device power status
func (b *DryRunBMC) GetPowerState(_ context.Context) (string, error) {
	state, ok := serverStates[b.id]
	if ok {
		return state.power, nil
	}

	return "unknown", nil
}

// SetPowerState sets the given power state on the device
func (b *DryRunBMC) SetPowerState(_ context.Context, state string) error {
	serverState, ok := serverStates[b.id]
	if ok {
		serverState.power = state
		serverStates[b.id] = serverState
		return nil
	}

	return errBmcCantFindServer
}

// SetBootDevice simulates setting the boot device of the remote device
func (b *DryRunBMC) SetBootDevice(_ context.Context, _ string, _, _ bool) error {
	return nil
}

// PowerCycleBMC simulates a power cycle action on the BMC of the remote device
func (b *DryRunBMC) PowerCycleBMC(_ context.Context) error {
	return nil
}
