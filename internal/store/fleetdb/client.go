package fleetdb

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/coreos/go-oidc"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2/clientcredentials"

	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"
)

var (
	// timeout for requests made by this client.
	timeout = 30 * time.Second
)

// NewFleetDBClient instantiates and returns a serverService client
func NewFleetDBClient(ctx context.Context, cfg *Config, logger *logrus.Logger) (*fleetdbapi.Client, error) {
	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	if cfg.Authenticate {
		return newFleetDBClientWithOAuthOtel(ctx, cfg, logger)
	}

	return newFleetDBClientWithOtel(cfg, logger)
}

// returns a fleetdb retryable client with Otel
func newFleetDBClientWithOtel(cfg *Config, logger *logrus.Logger) (*fleetdbapi.Client, error) {
	// init retryable http client
	retryableClient := retryablehttp.NewClient()

	// log hook fo 500 errors since the the retryablehttp client masks them
	logHookFunc := func(_ retryablehttp.Logger, r *http.Response) {
		if r.StatusCode == http.StatusInternalServerError {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				logger.Warn("fleetdb query returned 500 error, got error reading body: ", err.Error())
				return
			}

			logger.Warn("fleetdb query returned 500 error, body: ", string(b))
		}
	}

	retryableClient.ResponseLogHook = logHookFunc

	// set retryable HTTP client to be the otel http client to collect telemetry
	retryableClient.HTTPClient = otelhttp.DefaultClient

	// requests taking longer than timeout value should be canceled.
	client := retryableClient.StandardClient()
	client.Timeout = timeout

	return fleetdbapi.NewClientWithToken(
		"dummy",
		cfg.URL,
		client,
	)
}

// returns a fleetdb retryable http client with Otel and Oauth wrapped in
func newFleetDBClientWithOAuthOtel(ctx context.Context, cfg *Config, logger *logrus.Logger) (*fleetdbapi.Client, error) {
	logger.Info("fleetdb client ctor")

	// init retryable http client
	retryableClient := retryablehttp.NewClient()

	// set retryable HTTP client to be the otel http client to collect telemetry
	retryableClient.HTTPClient = otelhttp.DefaultClient

	// setup oidc provider
	provider, err := oidc.NewProvider(ctx, cfg.OidcIssuerURL)
	if err != nil {
		return nil, err
	}

	// clientID defaults to 'flipflop'
	clientID := "flipflop"

	if cfg.OidcClientID != "" {
		clientID = cfg.OidcClientID
	}

	// setup oauth configuration
	oauthConfig := clientcredentials.Config{
		ClientID:       clientID,
		ClientSecret:   cfg.OidcClientSecret,
		TokenURL:       provider.Endpoint().TokenURL,
		Scopes:         cfg.OidcClientScopes,
		EndpointParams: url.Values{"audience": []string{cfg.OidcAudienceURL}},
	}

	// wrap OAuth transport, cookie jar in the retryable client
	oAuthclient := oauthConfig.Client(ctx)

	retryableClient.HTTPClient.Transport = oAuthclient.Transport
	retryableClient.HTTPClient.Jar = oAuthclient.Jar

	// requests taking longer than timeout value should be canceled.
	client := retryableClient.StandardClient()
	client.Timeout = timeout

	return fleetdbapi.NewClientWithToken(
		cfg.OidcClientSecret,
		cfg.URL,
		client,
	)
}
