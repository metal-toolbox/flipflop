package device

import "context"

// Bmc is an implementation of the Queryor interface
type DryRunBMC struct {
	state string
}

func NewDryRunBMCClient(initialState string) Queryor {
	return &DryRunBMC{
		state: initialState,
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
	return b.state, nil
}

// SetPowerState sets the given power state on the device
func (b *DryRunBMC) SetPowerState(_ context.Context, state string) error {
	b.state = state
	return nil
}

// SetBootDevice simulates setting the boot device of the remote device
func (b *DryRunBMC) SetBootDevice(_ context.Context, _ string, _, _ bool) error {
	return nil
}

// PowerCycleBMC simulates a power cycle action on the BMC of the remote device
func (b *DryRunBMC) PowerCycleBMC(_ context.Context) error {
	return nil
}
