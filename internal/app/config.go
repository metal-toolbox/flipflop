package app

import (
	"os"
	"reflect"
	"strings"

	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/metal-toolbox/flipflop/internal/store/fleetdb"
	"github.com/metal-toolbox/rivets/events"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
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

	err := setConfigDefaults(a.v, cfg, "", ".")
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

// setConfigDefaults sets all values in the struct to empty. This is to get around a weird quirk of viper.
// Viper will not override from a ENV variable if the value isnt set first.
// Resulting in ENV variables never getting loaded in if they arent in the config.yaml, even empty in the config.
// https://github.com/spf13/viper/issues/584#issuecomment-1210957041
func setConfigDefaults(v *viper.Viper, i interface{}, parent, delim string) error {
	// Retrieve the underlying type of variable `i`.
	r := reflect.TypeOf(i)

	// If `i` is of type pointer, retrieve the referenced type.
	if r.Kind() == reflect.Ptr {
		r = r.Elem()
	}

	// Iterate over each field for the type. By default, there is a single field.
	for i := 0; i < r.NumField(); i++ {
		// Retrieve the current field and get the `mapstructure` tag.
		f := r.Field(i)
		env := f.Tag.Get("mapstructure")

		// By default, set the key to the current tag value. If a parent value was passed in
		//	prepend the parent and the delimiter.
		if parent != "" {
			env = parent + delim + env
		}

		// If it's a struct, only bind properties.
		if f.Type.Kind() == reflect.Struct {
			t := reflect.New(f.Type).Elem().Interface()
			_ = setConfigDefaults(v, t, env, delim)
			continue
		}

		// Bind the environment variable.
		v.SetDefault(env, nil)
	}
	return v.Unmarshal(i)
}
