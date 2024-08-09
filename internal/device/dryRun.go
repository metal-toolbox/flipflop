package device

import (
	"context"
	"errors"
	"time"

	"github.com/metal-toolbox/flipflop/internal/model"
)

type serverState struct {
	powerStatus        string
	bootTime           time.Time
	bootDevice         string
	previousBootDevice string
	persistent         bool
	efiBoot            bool
}

var (
	errBmcCantFindServer = errors.New("dryrun BMC couldnt find server to set state")
	errBmcServerOffline  = errors.New("dryrun BMC couldnt set boot device, server is off")
	serverStates         = make(map[string]serverState)
)

// DryRunBMC is an simulated implementation of the Queryor interface
type DryRunBMC struct {
	id string
}

// NewDryRunBMCClient creates a new Queryor interface for a simulated BMC
func NewDryRunBMCClient(asset *model.Asset) Queryor {
	state, ok := serverStates[asset.ID.String()]
	if !ok {
		state.powerStatus = "on"
		state.bootDevice = "disk"
		state.previousBootDevice = "disk"
		state.persistent = true
		state.efiBoot = false
		state.bootTime = time.Now()
		serverStates[asset.ID.String()] = state
	}

	return &DryRunBMC{
		asset.ID.String(),
	}
}

// Open simulates creating a BMC session
func (b *DryRunBMC) Open(_ context.Context) error {
	return nil
}

// Close simulates logging out of the BMC
func (b *DryRunBMC) Close(_ context.Context) error {
	return nil
}

// GetPowerState simulates returning the device power status
func (b *DryRunBMC) GetPowerState(_ context.Context) (string, error) {
	server, err := b.getServer()
	if err != nil {
		return "", err
	}

	return server.powerStatus, nil
}

// SetPowerState simulates setting the given power state on the device
func (b *DryRunBMC) SetPowerState(_ context.Context, state string) error {
	server, err := b.getServer()
	if err != nil {
		return err
	}

	if isRestarting(state) {
		server.bootTime = getRestartTime(state)
	}

	server.powerStatus = state
	serverStates[b.id] = *server
	return nil
}

// SetBootDevice simulates setting the boot device of the remote device
func (b *DryRunBMC) SetBootDevice(_ context.Context, device string, persistent, efiBoot bool) error {
	server, err := b.getServer()
	if err != nil {
		return err
	}

	if server.powerStatus != "on" {
		return errBmcServerOffline
	}

	server.previousBootDevice = server.bootDevice
	server.bootDevice = device
	server.persistent = persistent
	server.efiBoot = efiBoot

	return nil
}

// GetBootDevice simulates getting the boot device information of the remote device
func (b *DryRunBMC) GetBootDevice(_ context.Context) (device string, persistent, efiBoot bool, err error) {
	server, err := b.getServer()
	if err != nil {
		return "", false, false, err
	}

	if server.powerStatus != "on" {
		return "", false, false, errBmcServerOffline
	}

	return server.bootDevice, server.persistent, server.efiBoot, nil
}

// PowerCycleBMC simulates a power cycle action on the BMC of the remote device
func (b *DryRunBMC) PowerCycleBMC(_ context.Context) error {
	return nil
}

// HostBooted reports whether or not the device has booted the host OS
func (b *DryRunBMC) HostBooted(_ context.Context) (bool, error) {
	return true, nil
}

// getServer gets a simulateed server state, and update power status and boot device if required
func (b *DryRunBMC) getServer() (*serverState, error) {
	server, ok := serverStates[b.id]
	if !ok {
		return nil, errBmcCantFindServer
	}

	if isRestarting(server.powerStatus) {
		if time.Now().After(server.bootTime) {
			server.powerStatus = "on"

			if !server.persistent {
				server.bootDevice = server.previousBootDevice
			}
		}
	}

	return &server, nil
}

func isRestarting(state string) bool {
	switch state {
	case "reset", "cycle":
		return true
	default:
		return false
	}
}

func getRestartTime(state string) time.Time {
	switch state {
	case "reset":
		return time.Now().Add(time.Second * 30) // Soft reboot should take longer than a hard reboot
	case "cycle":
		return time.Now().Add(time.Second * 20)
	default:
		return time.Now() // No reboot necessary
	}
}
