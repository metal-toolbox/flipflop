package app

import (
	"os"
	"strings"

	"github.com/jeremywohl/flatten"
	"github.com/metal-toolbox/rivets/v2/events"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"

	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store/fleetdb"
)

var (
	ErrConfig = errors.New("configuration error")
)

// Configuration values are first grabbed from the config file. They must be in the right place in order to be grabbed.
// Values are then grabbed from the ENV variables, anything found will be used to override values in the config file.
// Example: Setting Configuration.Endpoints.FleetDB.URL
// In the config file (as yaml); endpoints.fleetdb.url: http://fleetdb:8000
// As a ENV variable; FLIPFLOP_ENDPOINTS_FLEETDB_URL=http://fleetdb:8000
type Configuration struct {
	// FacilityCode limits this flipflop to events in a facility.
	FacilityCode string `mapstructure:"facility"`

	// LogLevel is the app verbose logging level.
	// one of - info, debug, trace
	LogLevel string `mapstructure:"log_level"`

	// Holds all endpoints
	Endpoints Endpoints `mapstructure:"endpoints"`

	// nats controller concurrency
	Concurrency int `mapstructure:"concurrency"`

	// In dryrun mode, the worker actions the task without installing firmware
	// Note: Currently completely overrided by commandline arg `--dry-run`
	Dryrun bool `mapstructure:"dryrun"`

	// Tasks can include a Fault attribute to allow fault injection for development purposes
	// Note: Currently completely overrided by commandline arg `--fault-injection`
	FaultInjection bool `mapstructure:"fault_injection"`
}

type Endpoints struct {
	// NatsOptions defines the NATs events broker configuration parameters.
	Nats events.NatsOptions `mapstructure:"nats"`

	// FleetDBConfig defines the fleetdb client configuration parameters
	FleetDB fleetdb.Config `mapstructure:"fleetdb"`
}

func (a *App) LoadConfiguration(cfgFilePath, loglevel string) error {
	cfg := &Configuration{}
	a.Config = cfg

	err := a.envBindVars()
	if err != nil {
		return err
	}

	a.v.SetConfigType("yaml")
	a.v.SetEnvPrefix(model.AppName)
	a.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	a.v.AutomaticEnv()

	err = a.ReadInFile(cfg, cfgFilePath)
	if err != nil {
		return err
	}

	if loglevel != "" {
		a.Config.LogLevel = loglevel
	}

	return a.Config.validate()
}

// Reads in the cfgFile when available and overrides from environment variables.
func (a *App) ReadInFile(cfg *Configuration, path string) error {
	if cfg == nil {
		return ErrConfig
	}

	if path != "" {
		fh, err := os.Open(path)
		if err != nil {
			return errors.Wrap(ErrConfig, err.Error())
		}

		if err = a.v.ReadConfig(fh); err != nil {
			return errors.Wrap(ErrConfig, "ReadConfig error:"+err.Error())
		}
	} else {
		a.v.AddConfigPath(".")
		a.v.SetConfigName("config")
		err := a.v.ReadInConfig()
		if err != nil {
			return err
		}
	}

	err := a.v.Unmarshal(cfg)
	if err != nil {
		return err
	}

	return nil
}

func (cfg *Configuration) validate() error {
	if cfg == nil {
		return ErrConfig
	}

	if cfg.Concurrency == 0 {
		cfg.Concurrency = 1
	}

	if cfg.FacilityCode == "" {
		return errors.Wrap(ErrConfig, "no facility code")
	}

	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return nil
}

// envBindVars binds environment variables to the struct
// without a configuration file being unmarshalled,
// this is a workaround for a viper bug,
//
// This can be replaced by the solution in https://github.com/spf13/viper/pull/1429
// once that PR is merged.
func (a *App) envBindVars() error {
	envKeysMap := map[string]interface{}{}
	if err := mapstructure.Decode(a.Config, &envKeysMap); err != nil {
		return err
	}

	// Flatten nested conf map
	flat, err := flatten.Flatten(envKeysMap, "", flatten.DotStyle)
	if err != nil {
		return errors.Wrap(err, "Unable to flatten config")
	}

	for k := range flat {
		if err := a.v.BindEnv(k); err != nil {
			return errors.Wrap(ErrConfig, "env var bind error: "+err.Error())
		}
	}

	return nil
}
