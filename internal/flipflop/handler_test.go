package flipflop

import (
	"context"
	"errors"
	"testing"

	"github.com/stmcginnis/gofish/redfish"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	ffmock "github.com/metal-toolbox/flipflop/internal/model/mock"
)

// test the internal parts of a firmware validation task
func TestValidateFirmwareInternal(t *testing.T) {
	t.Parallel()
	t.Run("fail to open monitor", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := func(_ context.Context) error {
			return nil
		}
		update := func(_ string) {}

		mon.EXPECT().Open(
			mock.IsType(context.TODO()),
		).Return(errors.New("pound sand")).Times(1)
		err := validateFirmwareInternal(context.TODO(), mon, update, delay)
		require.Error(t, err, "expected error on Open")
	})
	t.Run("fail to do BmcReset", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := func(_ context.Context) error {
			return nil
		}
		update := func(_ string) {}

		mon.EXPECT().Open(
			mock.IsType(context.TODO()),
		).Return(nil).Times(1)

		mon.EXPECT().BmcReset(
			mock.IsType(context.TODO()),
			string(redfish.PowerCycleResetType),
		).Return(false, errors.New("pound sand")).Times(1)
		err := validateFirmwareInternal(context.TODO(), mon, update, delay)
		require.Error(t, err, "expected error on Open")
	})
	t.Run("timeout reconnecting to bmc", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := func(_ context.Context) error {
			return context.Canceled
		}
		update := func(_ string) {}

		mon.EXPECT().Open(
			mock.IsType(context.TODO()),
		).Return(nil).Times(1)

		mon.EXPECT().Close(
			mock.IsType(context.TODO()),
		).Return(nil).Times(1)

		mon.EXPECT().BmcReset(
			mock.IsType(context.TODO()),
			string(redfish.PowerCycleResetType),
		).Return(true, nil).Times(1)
		err := validateFirmwareInternal(context.TODO(), mon, update, delay)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("bmc reopen timeout", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := ffmock.NewMockDelayFn(t)
		update := ffmock.NewMockUpdateFn(t)

		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(context.Canceled).Times(1)

		mon.On("Open", mock.IsType(context.TODO())).Return(nil).Times(1)
		mon.On("Open", mock.IsType(context.TODO())).Return(errors.New("pound sand")).Times(1)

		mon.EXPECT().Close(
			mock.IsType(context.TODO()),
		).Return(nil).Times(1)

		mon.EXPECT().BmcReset(
			mock.IsType(context.TODO()),
			string(redfish.PowerCycleResetType),
		).Return(true, nil).Times(1)

		update.On("Execute", "bmc power cycle sent").Return().Times(1)
		update.On("Execute", "failed to connect to bmc: pound sand").Return().Times(1)

		err := validateFirmwareInternal(context.TODO(), mon, update.Execute, delay.Execute)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("timeout on get power-state", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := ffmock.NewMockDelayFn(t)
		update := ffmock.NewMockUpdateFn(t)

		// the scenario: open the connection to the BMC
		//               do a BMC reset
		//               close/reopen
		//               get the power state and fail
		//               timeout

		mon.On("Open", mock.IsType(context.TODO())).Return(nil).Times(2)
		mon.On("BmcReset", mock.IsType(context.TODO()), string(redfish.PowerCycleResetType)).Return(true, nil).Times(1)
		mon.On("Close", mock.IsType(context.TODO())).Return(nil).Times(1)

		mon.On("PowerStateGet", mock.IsType(context.TODO())).Return("", errors.New("pound sand")).Times(1)

		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(context.Canceled).Times(1)

		update.On("Execute", "bmc power cycle sent").Return().Times(1)
		update.On("Execute", "bmc connection re-established").Return().Times(1)
		update.On("Execute", "getting bmc power state: pound sand").Return().Times(1)

		err := validateFirmwareInternal(context.TODO(), mon, update.Execute, delay.Execute)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("timeout on power set -- power cycle", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := ffmock.NewMockDelayFn(t)
		update := ffmock.NewMockUpdateFn(t)

		mon.On("Open", mock.IsType(context.TODO())).Return(nil).Times(2)
		mon.On("BmcReset", mock.IsType(context.TODO()), string(redfish.PowerCycleResetType)).Return(true, nil).Times(1)
		mon.On("Close", mock.IsType(context.TODO())).Return(nil).Times(1)

		mon.On("PowerStateGet", mock.IsType(context.TODO())).Return("on", nil).Times(1)
		mon.On("PowerSet", mock.IsType(context.TODO()), "cycle").Return(false, errors.New("pound sand")).Times(1)

		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(context.Canceled).Times(1)

		update.On("Execute", "bmc power cycle sent").Return().Times(1)
		update.On("Execute", "bmc connection re-established").Return().Times(1)
		update.On("Execute", "bmc set power state to cycle: pound sand").Return().Times(1)

		err := validateFirmwareInternal(context.TODO(), mon, update.Execute, delay.Execute)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("timeout on power set -- power on", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := ffmock.NewMockDelayFn(t)
		update := ffmock.NewMockUpdateFn(t)

		mon.On("Open", mock.IsType(context.TODO())).Return(nil).Times(2)
		mon.On("BmcReset", mock.IsType(context.TODO()), string(redfish.PowerCycleResetType)).Return(true, nil).Times(1)
		mon.On("Close", mock.IsType(context.TODO())).Return(nil).Times(1)

		mon.On("PowerStateGet", mock.IsType(context.TODO())).Return("off", nil).Times(1)
		mon.On("PowerSet", mock.IsType(context.TODO()), "on").Return(false, errors.New("pound sand")).Times(1)

		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(context.Canceled).Times(1)

		update.On("Execute", "bmc power cycle sent").Return().Times(1)
		update.On("Execute", "bmc connection re-established").Return().Times(1)
		update.On("Execute", "bmc set power state to on: pound sand").Return().Times(1)

		err := validateFirmwareInternal(context.TODO(), mon, update.Execute, delay.Execute)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("timeout on boot complete", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := ffmock.NewMockDelayFn(t)
		update := ffmock.NewMockUpdateFn(t)

		mon.On("Open", mock.IsType(context.TODO())).Return(nil).Times(2)
		mon.On("BmcReset", mock.IsType(context.TODO()), string(redfish.PowerCycleResetType)).Return(true, nil).Times(1)
		mon.On("Close", mock.IsType(context.TODO())).Return(nil).Times(1)

		mon.On("PowerStateGet", mock.IsType(context.TODO())).Return("off", nil).Times(1)
		mon.On("PowerSet", mock.IsType(context.TODO()), "on").Return(true, nil).Times(1)
		mon.On("BootComplete").Return(false, errors.New("pound sand")).Times(1)

		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(context.Canceled).Times(1)

		update.On("Execute", "bmc power cycle sent").Return().Times(1)
		update.On("Execute", "bmc connection re-established").Return().Times(1)
		update.On("Execute", "device power state set to on").Return().Times(1)
		update.On("Execute", "checking host boot state: pound sand").Return().Times(1)

		err := validateFirmwareInternal(context.TODO(), mon, update.Execute, delay.Execute)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("complete validation", func(t *testing.T) {
		t.Parallel()
		mon := ffmock.NewMockBMCBootMonitor(t)
		delay := ffmock.NewMockDelayFn(t)
		update := ffmock.NewMockUpdateFn(t)

		mon.On("Open", mock.IsType(context.TODO())).Return(nil).Times(2)
		mon.On("BmcReset", mock.IsType(context.TODO()), string(redfish.PowerCycleResetType)).Return(true, nil).Times(1)
		mon.On("Close", mock.IsType(context.TODO())).Return(nil).Times(1)

		mon.On("PowerStateGet", mock.IsType(context.TODO())).Return("off", nil).Times(1)
		mon.On("PowerSet", mock.IsType(context.TODO()), "on").Return(true, nil).Times(1)
		mon.On("BootComplete").Return(true, nil).Times(1)

		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)
		delay.On("Execute", mock.IsType(context.TODO())).Return(nil).Times(1)

		update.On("Execute", "bmc power cycle sent").Return().Times(1)
		update.On("Execute", "bmc connection re-established").Return().Times(1)
		update.On("Execute", "device power state set to on").Return().Times(1)
		update.On("Execute", "host boot complete").Return().Times(1)

		err := validateFirmwareInternal(context.TODO(), mon, update.Execute, delay.Execute)
		require.NoError(t, err)
	})

}
