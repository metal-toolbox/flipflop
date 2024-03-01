package fleetdb

import (
	"net"

	"testing"

	"github.com/google/uuid"
	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateRequiredAttributes(t *testing.T) {
	// nolint:govet // ignore struct alignment in test
	cases := []struct {
		name              string
		server            *fleetdbapi.Server
		secret            *fleetdbapi.ServerCredential
		expectCredentials bool
		expectedErr       string
	}{
		{
			"server object nil",
			nil,
			nil,
			true,
			"server object nil",
		},
		{
			"server credential object nil",
			&fleetdbapi.Server{},
			nil,
			true,
			"server credential object nil",
		},
		{
			"server attributes slice empty",
			&fleetdbapi.Server{},
			&fleetdbapi.ServerCredential{},
			true,
			"server attributes slice empty",
		},
		{
			"BMC password field empty",
			&fleetdbapi.Server{Attributes: []fleetdbapi.Attributes{{Namespace: bmcAttributeNamespace}}},
			&fleetdbapi.ServerCredential{Username: "foo", Password: ""},
			true,
			"BMC password field empty",
		},
		{
			"BMC username field empty",
			&fleetdbapi.Server{Attributes: []fleetdbapi.Attributes{{Namespace: bmcAttributeNamespace}}},
			&fleetdbapi.ServerCredential{Username: "", Password: "123"},
			true,
			"BMC username field empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRequiredAttributes(tc.server, tc.secret)
			if tc.expectedErr != "" {
				assert.Contains(t, err.Error(), tc.expectedErr)
				return
			}

			assert.Nil(t, err)
		})
	}
}

func TestToAsset(t *testing.T) {
	cases := []struct {
		name          string
		server        *fleetdbapi.Server
		secret        *fleetdbapi.ServerCredential
		expectedAsset *model.Asset
		expectedErr   string
	}{
		{
			"Expected attributes empty raises error",
			&fleetdbapi.Server{
				Attributes: []fleetdbapi.Attributes{
					{
						Namespace: "invalid",
					},
				},
			},
			&fleetdbapi.ServerCredential{Username: "foo", Password: "bar"},
			nil,
			"expected server attributes with BMC address, got none",
		},
		{
			"Attributes missing BMC IP Address raises error",
			&fleetdbapi.Server{
				Attributes: []fleetdbapi.Attributes{
					{
						Namespace: bmcAttributeNamespace,
						Data:      []byte(`{"namespace":"foo"}`),
					},
				},
			},
			&fleetdbapi.ServerCredential{Username: "user", Password: "hunter2"},
			nil,
			"expected BMC address attribute empty",
		},
		{
			"Valid server, secret objects returns *model.Asset object",
			&fleetdbapi.Server{
				Attributes: []fleetdbapi.Attributes{
					{
						Namespace: bmcAttributeNamespace,
						Data:      []byte(`{"address":"127.0.0.1"}`),
					},
				},
			},
			&fleetdbapi.ServerCredential{Username: "user", Password: "hunter2"},
			&model.Asset{
				ID:          uuid.Nil,
				BmcUsername: "user",
				BmcPassword: "hunter2",
				BmcAddress:  net.ParseIP("127.0.0.1"),
			},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asset, err := toAsset(tc.server, tc.secret)
			if tc.expectedErr != "" {
				assert.Contains(t, err.Error(), tc.expectedErr)
				return
			}

			assert.Nil(t, err)
			assert.Equal(t, tc.expectedAsset, asset)
		})
	}
}
