package app

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jeremywohl/flatten"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"go.hollow.sh/toolbox/events"
)

const (
	flipflopConcurrency = 5
)

var (
	ErrConfig = errors.New("configuration error")
)

// Config holds application configuration read from a YAML or set by env variables.
//
// nolint:govet // prefer readability over field alignment optimization for this case.
type Configuration struct {
	// LogLevel is the app verbose logging level.
	// one of - info, debug, trace
	LogLevel string `mapstructure:"log_level"`

	// AppKind is the application kind - worker / client
	AppKind model.AppKind `mapstructure:"app_kind"`

	// flipflop configuration
	Concurrency int `mapstructure:"concurrency"`

	// FacilityCode limits this flipflop to events in a facility.
	FacilityCode string `mapstructure:"facility_code"`

	// The inventory source - one of serverservice OR Yaml
	InventorySource string `mapstructure:"inventory_source"`

	StoreKind model.StoreKind `mapstructure:"store_kind"`

	// FleetDBOptions defines the serverservice client configuration parameters
	//
	// This parameter is required when StoreKind is set to fleetdb.
	FleetDBOptions *FleetDBOptions `mapstructure:"fleetdb"`

	// EventsBrokerKind indicates the kind of event broker configuration to enable,
	//
	// Supported parameter value - nats
	EventsBorkerKind string `mapstructure:"events_broker_kind"`

	// NatsOptions defines the NATs events broker configuration parameters.
	//
	// This parameter is required when EventsBrokerKind is set to nats.
	NatsOptions *events.NatsOptions `mapstructure:"nats"`
}

// FleetDBOptions defines configuration for the Serverservice client.
// https://github.com/metal-toolbox/hollow-serverservice
type FleetDBOptions struct {
	EndpointURL          *url.URL
	Endpoint             string   `mapstructure:"endpoint"`
	OidcIssuerEndpoint   string   `mapstructure:"oidc_issuer_endpoint"`
	OidcAudienceEndpoint string   `mapstructure:"oidc_audience_endpoint"`
	OidcClientSecret     string   `mapstructure:"oidc_client_secret"`
	OidcClientID         string   `mapstructure:"oidc_client_id"`
	OidcClientScopes     []string `mapstructure:"oidc_client_scopes"`
	DisableOAuth         bool     `mapstructure:"disable_oauth"`
}

// LoadConfiguration loads application configuration
//
// Reads in the cfgFile when available and overrides from environment variables.
func (a *App) LoadConfiguration(cfgFile string, storeKind model.StoreKind) error {
	a.v.SetConfigType("yaml")
	a.v.SetEnvPrefix(model.AppName)
	a.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	a.v.AutomaticEnv()

	// these are initialized here so viper can read in configuration from env vars
	// once https://github.com/spf13/viper/pull/1429 is merged, this can go.
	a.Config.FleetDBOptions = &FleetDBOptions{}
	a.Config.NatsOptions = &events.NatsOptions{
		Stream:   &events.NatsStreamOptions{},
		Consumer: &events.NatsConsumerOptions{},
	}

	if cfgFile != "" {
		fh, err := os.Open(cfgFile)
		if err != nil {
			return errors.Wrap(ErrConfig, err.Error())
		}

		if err = a.v.ReadConfig(fh); err != nil {
			return errors.Wrap(ErrConfig, "ReadConfig error:"+err.Error())
		}
	}

	a.v.SetDefault("log.level", "info")

	if err := a.envBindVars(); err != nil {
		return errors.Wrap(ErrConfig, "env var bind error:"+err.Error())
	}

	if err := a.v.Unmarshal(a.Config); err != nil {
		return errors.Wrap(ErrConfig, "Unmarshal error: "+err.Error())
	}

	a.envVarAppOverrides()

	if a.Config.EventsBorkerKind == "nats" {
		if err := a.envVarNatsOverrides(); err != nil {
			return errors.Wrap(ErrConfig, "nats env overrides error:"+err.Error())
		}
	}

	if storeKind == model.FleetDB {
		if err := a.envVarServerserviceOverrides(); err != nil {
			return errors.Wrap(ErrConfig, "serverservice env overrides error:"+err.Error())
		}
	}

	return nil
}

func (a *App) envVarAppOverrides() {
	if a.v.GetString("log.level") != "" {
		a.Config.LogLevel = a.v.GetString("log.level")
	}
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

// NATs streaming configuration
var (
	defaultNatsConnectTimeout = 100 * time.Millisecond
)

// nolint:gocyclo // nats env config load is cyclomatic
func (a *App) envVarNatsOverrides() error {
	if a.Config.NatsOptions == nil {
		a.Config.NatsOptions = &events.NatsOptions{}
	}

	if a.v.GetString("nats.url") != "" {
		a.Config.NatsOptions.URL = a.v.GetString("nats.url")
	}

	if a.Config.NatsOptions.URL == "" {
		return errors.New("missing parameter: nats.url")
	}

	if a.v.GetString("nats.publisherSubjectPrefix") != "" {
		a.Config.NatsOptions.PublisherSubjectPrefix = a.v.GetString("nats.publisherSubjectPrefix")
	}

	if a.Config.NatsOptions.PublisherSubjectPrefix == "" {
		return errors.New("missing parameter: nats.publisherSubjectPrefix")
	}

	if a.v.GetString("nats.stream.user") != "" {
		a.Config.NatsOptions.StreamUser = a.v.GetString("nats.stream.user")
	}

	if a.v.GetString("nats.stream.pass") != "" {
		a.Config.NatsOptions.StreamPass = a.v.GetString("nats.stream.pass")
	}

	if a.v.GetString("nats.creds.file") != "" {
		a.Config.NatsOptions.CredsFile = a.v.GetString("nats.creds.file")
	}

	if a.v.GetString("nats.stream.name") != "" {
		if a.Config.NatsOptions.Stream == nil {
			a.Config.NatsOptions.Stream = &events.NatsStreamOptions{}
		}

		a.Config.NatsOptions.Stream.Name = a.v.GetString("nats.stream.name")
	}

	if a.Config.NatsOptions.Stream.Name == "" {
		return errors.New("A stream name is required")
	}

	if a.v.GetString("nats.consumer.name") != "" {
		if a.Config.NatsOptions.Consumer == nil {
			a.Config.NatsOptions.Consumer = &events.NatsConsumerOptions{}
		}

		a.Config.NatsOptions.Consumer.Name = a.v.GetString("nats.consumer.name")
	}

	if len(a.v.GetStringSlice("nats.consumer.subscribeSubjects")) != 0 {
		a.Config.NatsOptions.Consumer.SubscribeSubjects = a.v.GetStringSlice("nats.consumer.subscribeSubjects")
	}

	if len(a.Config.NatsOptions.Consumer.SubscribeSubjects) == 0 {
		return errors.New("missing parameter: nats.consumer.subscribeSubjects")
	}

	if a.v.GetString("nats.consumer.filterSubject") != "" {
		a.Config.NatsOptions.Consumer.FilterSubject = a.v.GetString("nats.consumer.filterSubject")
	}

	if a.Config.NatsOptions.Consumer.FilterSubject == "" {
		return errors.New("missing parameter: nats.consumer.filterSubject")
	}

	if a.v.GetDuration("nats.connect.timeout") != 0 {
		a.Config.NatsOptions.ConnectTimeout = a.v.GetDuration("nats.connect.timeout")
	}

	if a.Config.NatsOptions.ConnectTimeout == 0 {
		a.Config.NatsOptions.ConnectTimeout = defaultNatsConnectTimeout
	}

	return nil
}

// Server service configuration options

// nolint:gocyclo // parameter validation is cyclomatic
func (a *App) envVarServerserviceOverrides() error {
	if a.Config.FleetDBOptions == nil {
		a.Config.FleetDBOptions = &FleetDBOptions{}
	}

	if a.v.GetString("fleetdb.endpoint") != "" {
		a.Config.FleetDBOptions.Endpoint = a.v.GetString("fleetdb.endpoint")
	}

	endpointURL, err := url.Parse(a.Config.FleetDBOptions.Endpoint)
	if err != nil {
		return errors.New("serverservice endpoint URL error: " + err.Error())
	}

	a.Config.FleetDBOptions.EndpointURL = endpointURL

	if a.v.GetString("fleetdb.disable.oauth") != "" {
		a.Config.FleetDBOptions.DisableOAuth = a.v.GetBool("fleetdb.disable.oauth")
	}

	if a.Config.FleetDBOptions.DisableOAuth {
		return nil
	}

	if a.v.GetString("fleetdb.oidc.issuer.endpoint") != "" {
		a.Config.FleetDBOptions.OidcIssuerEndpoint = a.v.GetString("fleetdb.oidc.issuer.endpoint")
	}

	if a.Config.FleetDBOptions.OidcIssuerEndpoint == "" {
		return errors.New("serverservice oidc.issuer.endpoint not defined")
	}

	if a.v.GetString("fleetdb.oidc.audience.endpoint") != "" {
		a.Config.FleetDBOptions.OidcAudienceEndpoint = a.v.GetString("fleetdb.oidc.audience.endpoint")
	}

	if a.Config.FleetDBOptions.OidcAudienceEndpoint == "" {
		return errors.New("serverservice oidc.audience.endpoint not defined")
	}

	if a.v.GetString("fleetdb.oidc.client.secret") != "" {
		a.Config.FleetDBOptions.OidcClientSecret = a.v.GetString("fleetdb.oidc.client.secret")
	}

	if a.Config.FleetDBOptions.OidcClientSecret == "" {
		return errors.New("fleetdb.oidc.client.secret not defined")
	}

	if a.v.GetString("fleetdb.oidc.client.id") != "" {
		a.Config.FleetDBOptions.OidcClientID = a.v.GetString("fleetdb.oidc.client.id")
	}

	if a.Config.FleetDBOptions.OidcClientID == "" {
		return errors.New("fleetdb.oidc.client.id not defined")
	}

	if a.v.GetString("fleetdb.oidc.client.scopes") != "" {
		a.Config.FleetDBOptions.OidcClientScopes = a.v.GetStringSlice("fleetdb.oidc.client.scopes")
	}

	if len(a.Config.FleetDBOptions.OidcClientScopes) == 0 {
		return errors.New("serverservice oidc.client.scopes not defined")
	}

	return nil
}
