package flipflop

/* These functions exist to provide a little nicer client experience around using raw bmclib
   types without going through the hassle of trying to get the default client of bmclib to
   play nice. */

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/bmc-toolbox/bmclib/v2/providers/supermicro"
	"github.com/go-logr/logr"
	"github.com/metal-toolbox/flipflop/internal/model"
	"golang.org/x/net/publicsuffix"
)

func defaultBMCTransport() *http.Transport {
	return &http.Transport{
		//nolint:gosec // BMCs use self-signed certs
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: true,
		Dial: (&net.Dialer{
			Timeout:   120 * time.Second,
			KeepAlive: 120 * time.Second,
		}).Dial,
		TLSHandshakeTimeout:   120 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}
}

func newHTTPClient(opts ...func(*http.Client)) *http.Client {
	// we ignore the error here because cookiejar.New always returns nil
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})

	client := &http.Client{
		Timeout:   time.Second * 120,
		Transport: defaultBMCTransport(),
		Jar:       jar,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}

	return client
}

func newSMCValidationHandle(srv *model.Asset) model.BMCBootMonitor {
	httpClient := newHTTPClient()
	hdl := supermicro.NewClient(
		srv.BmcAddress.String(),
		srv.BmcUsername,
		srv.BmcPassword,
		logr.Discard(),
		supermicro.WithHttpClient(httpClient),
	)

	return hdl
}
