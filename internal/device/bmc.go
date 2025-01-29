package device

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/cookiejar"
	"path"
	"runtime"
	"strings"
	"time"

	logrusr "github.com/bombsimon/logrusr/v4"
	"github.com/metal-toolbox/bmclib"
	"github.com/metal-toolbox/bmclib/constants"
	"github.com/metal-toolbox/bmclib/providers"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"golang.org/x/net/publicsuffix"

	"github.com/metal-toolbox/flipflop/internal/model"
)

const (
	logoutTimeout = 1 * time.Minute
	loginTimeout  = 1 * time.Minute
)

var (
	errBMCLogin          = errors.New("bmc login error")
	errBMCLogout         = errors.New("bmc logout error")
	errBMCNotImplemented = errors.New("method not implemented")
)

// Bmc is an implementation of the Queryor interface
type Bmc struct {
	client *bmclib.Client
	asset  *model.Asset
	logger *logrus.Entry
}

// NewBMCClient creates a new Queryor interface for a BMC
func NewBMCClient(asset *model.Asset, logger *logrus.Entry) Queryor {
	client := newBmclibClient(asset, logger)

	return &Bmc{
		client,
		asset,
		logger,
	}
}

// Open creates a BMC session
func (b *Bmc) Open(ctx context.Context) error {
	if b.client == nil {
		return errors.Wrap(errBMCLogin, "client not initialized")
	}
	defer b.tracelog()

	return b.client.Open(ctx)
}

// Close logs out of the BMC
func (b *Bmc) Close(traceCtx context.Context) error {
	if b.client == nil {
		return nil
	}

	ctxClose, cancel := context.WithTimeout(traceCtx, logoutTimeout)
	defer cancel()

	defer b.tracelog()

	if err := b.client.Close(ctxClose); err != nil {
		return errors.Wrap(errBMCLogout, err.Error())
	}

	return nil
}

// GetPowerState returns the device power status
func (b *Bmc) GetPowerState(ctx context.Context) (string, error) {
	defer b.tracelog()
	return b.client.GetPowerState(ctx)
}

// SetPowerState sets the given power state on the device
func (b *Bmc) SetPowerState(ctx context.Context, state string) error {
	defer b.tracelog()
	_, err := b.client.SetPowerState(ctx, state)
	return err
}

// SetBootDevice sets the boot device of the remote device, and validates it was set
//
//nolint:gocritic // its a TODO
func (b *Bmc) SetBootDevice(ctx context.Context, device string, persistent, efiBoot bool) error {
	ok, err := b.client.SetBootDevice(ctx, device, persistent, efiBoot)
	if err != nil {
		return err
	}

	if !ok {
		return errors.New("setting boot device failed")
	}

	// Now lets validate the boot device order
	// TODO; This is a WIP. We do not know yet if This is the right bmc call to get boot device
	// override, err := b.client.GetBootDeviceOverride(ctx)
	// if err != nil {
	// 	return err
	// }

	// if device != string(override.Device) {
	// 	return errors.New("setting boot device failed to propagate")
	// }

	// if efiBoot != override.IsEFIBoot {
	// 	return errors.New("setting boot device EFI boot failed to propagate")
	// }

	// if persistent != override.IsPersistent {
	// 	return errors.New("setting boot device Persistent boot failed to propagate")
	// }

	return nil
}

// GetBootDevice gets the boot device information of the remote device
func (b *Bmc) GetBootDevice(_ context.Context) (device string, persistent, efiBoot bool, err error) {
	return "", false, false, errors.Wrap(errBMCNotImplemented, "GetBootDevice")
}

// PowerCycleBMC sets a power cycle action on the BMC of the remote device
func (b *Bmc) PowerCycleBMC(ctx context.Context) error {
	defer b.tracelog()
	_, err := b.client.ResetBMC(ctx, "GracefulRestart")
	return err
}

func (b *Bmc) HostBooted(ctx context.Context) (bool, error) {
	defer b.tracelog()
	status, _, err := b.client.PostCode(ctx)
	if err != nil {
		return false, err
	}
	return status == constants.POSTStateOS, nil
}

func (b *Bmc) tracelog() {
	pc, _, _, _ := runtime.Caller(1)
	funcName := path.Base(runtime.FuncForPC(pc).Name())

	mapstr := func(m map[string]string) string {
		if m == nil {
			return ""
		}

		var s []string
		for k, v := range m {
			s = append(s, k+": "+v)
		}

		return strings.Join(s, ", ")
	}

	b.logger.WithFields(
		logrus.Fields{
			"attemptedProviders":   strings.Join(b.client.GetMetadata().ProvidersAttempted, ","),
			"successfulProvider":   b.client.GetMetadata().SuccessfulProvider,
			"successfulOpens":      strings.Join(b.client.GetMetadata().SuccessfulOpenConns, ","),
			"successfulCloses":     strings.Join(b.client.GetMetadata().SuccessfulCloseConns, ","),
			"failedProviderDetail": mapstr(b.client.GetMetadata().FailedProviderDetail),
		}).Trace(funcName + ": connection metadata")
}

func newHTTPClient() *http.Client {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		panic(err)
	}

	// nolint:gomnd // time duration declarations are clear as is.
	return &http.Client{
		Timeout: time.Second * 600,
		Jar:     jar,
		Transport: &http.Transport{
			// nolint:gosec // BMCs don't have valid certs.
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
			Dial: (&net.Dialer{
				Timeout:   180 * time.Second,
				KeepAlive: 180 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   180 * time.Second,
			ResponseHeaderTimeout: 600 * time.Second,
			IdleConnTimeout:       180 * time.Second,
		},
	}
}

// newBmclibClient initializes a bmclib client with the given credentials
func newBmclibClient(asset *model.Asset, l *logrus.Entry) *bmclib.Client {
	logger := logrus.New()
	logger.Formatter = l.Logger.Formatter

	// setup a logr logger for bmclib
	// bmclib uses logr, for which the trace logs are logged with log.V(3),
	// this is a hax so the logrusr lib will enable trace logging
	// since any value that is less than (logrus.LogLevel - 4) >= log.V(3) is ignored
	// https://github.com/bombsimon/logrusr/blob/master/logrusr.go#L64
	switch l.Logger.GetLevel() {
	case logrus.TraceLevel:
		logger.Level = 7
	case logrus.DebugLevel:
		logger.Level = 5
	}

	logruslogr := logrusr.New(logger)

	bmcClient := bmclib.NewClient(
		asset.BmcAddress.String(),
		asset.BmcUsername,
		asset.BmcPassword,
		bmclib.WithLogger(logruslogr),
		bmclib.WithHTTPClient(newHTTPClient()),
		bmclib.WithPerProviderTimeout(loginTimeout),
		bmclib.WithRedfishEtagMatchDisabled(true),
		bmclib.WithTracerProvider(otel.GetTracerProvider()),
	)

	bmcClient.Registry.Drivers = bmcClient.Registry.Supports(
		providers.FeatureBmcReset,
		providers.FeatureBootDeviceSet,
		providers.FeaturePowerSet,
		providers.FeaturePowerState,
	)

	// NOTE: remove the .Using("redfish") before this ends up in prod
	// this is kept here since ipmitool doesn't work well in the docker sandbox env.
	return bmcClient.Using("redfish")
}
