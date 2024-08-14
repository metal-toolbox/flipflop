package fleetdb

import (
	"net/url"

	"github.com/pkg/errors"
)

// FleetDBConfig defines configuration for the Serverservice client.
// https://github.com/metal-toolbox/fleetdb
type Config struct {
	URL              string   `mapstructure:"url"`
	OidcIssuerURL    string   `mapstructure:"oidc_issuer_url"`
	OidcAudienceURL  string   `mapstructure:"oidc_audience_url"`
	OidcClientSecret string   `mapstructure:"oidc_client_secret"`
	OidcClientID     string   `mapstructure:"oidc_client_id"`
	OidcClientScopes []string `mapstructure:"oidc_client_scopes"`
	Authenticate     bool     `mapstructure:"authenticate"`
}

func (cfg *Config) validate() error {
	if cfg == nil {
		return errors.Wrap(ErrFleetDBConfig, "config was nil")
	}

	if cfg.URL == "" {
		return errors.Wrap(ErrFleetDBConfig, "url was empty")
	}

	_, err := url.Parse(cfg.URL)
	if err != nil {
		return errors.Wrap(ErrFleetDBConfig, "url failed to parse, isnt a valid url")
	}

	if !cfg.Authenticate {
		return nil
	}

	if cfg.OidcIssuerURL == "" {
		return errors.Wrap(ErrFleetDBConfig, "oidc issuer url was empty")
	}

	if cfg.OidcClientSecret == "" {
		return errors.Wrap(ErrFleetDBConfig, "oidc secret was empty")
	}

	if cfg.OidcClientID == "" {
		return errors.Wrap(ErrFleetDBConfig, "oidc client id was empty")
	}

	if len(cfg.OidcClientScopes) == 0 {
		return errors.Wrap(ErrFleetDBConfig, "oidc scopes was empty")
	}

	return nil
}
