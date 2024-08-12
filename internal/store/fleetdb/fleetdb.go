package fleetdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/google/uuid"
	"github.com/metal-toolbox/flipflop/internal/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"
)

const (
	pkgName = "internal/store"
)

var (
	errInventoryQuery = errors.New("fleetdb query returned error")
)

// Store is an asset inventory store
type Store struct {
	api    *fleetdbapi.Client
	logger *logrus.Logger
	config *Config
}

// NewStore returns a fleetdb store queryor to lookup and publish assets to, from the store.
func New(ctx context.Context, cfg *Config, logger *logrus.Logger) (*Store, error) {
	apiclient, err := NewFleetDBClient(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	s := &Store{
		api:    apiclient,
		logger: logger,
		config: cfg,
	}

	return s, nil
}

// assetByID queries serverService for the hardware asset by ID and returns an Asset object
func (s *Store) AssetByID(ctx context.Context, id string) (*model.Asset, error) {
	ctx, span := otel.Tracer(pkgName).Start(ctx, "fleetdb.AssetByID")
	defer span.End()

	sid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}

	// get server
	server, _, err := s.api.Get(ctx, sid)
	if err != nil {
		span.SetStatus(codes.Error, "Get() server failed")

		return nil, errors.Wrap(errInventoryQuery, "error querying server attributes: "+err.Error())
	}

	var credential *fleetdbapi.ServerCredential

	// get bmc credential
	credential, _, err = s.api.GetCredential(ctx, sid, fleetdbapi.ServerCredentialTypeBMC)
	if err != nil {
		span.SetStatus(codes.Error, "GetCredential() failed")

		return nil, errors.Wrap(errInventoryQuery, "error querying BMC credentials: "+err.Error())
	}

	return toAsset(server, credential)
}

func toAsset(server *fleetdbapi.Server, credential *fleetdbapi.ServerCredential) (*model.Asset, error) {
	if err := validateRequiredAttributes(server, credential); err != nil {
		return nil, errors.Wrap(ErrServerServiceObject, err.Error())
	}

	serverAttributes, err := serverAttributes(server.Attributes)
	if err != nil {
		fmt.Println(err.Error())
		return nil, errors.Wrap(ErrServerServiceObject, err.Error())
	}

	asset := &model.Asset{
		ID:     server.UUID,
		Serial: serverAttributes[serverSerialAttributeKey],
		Model:  serverAttributes[serverModelAttributeKey],
		Vendor: serverAttributes[serverVendorAttributeKey],
	}

	if credential != nil {
		asset.BmcUsername = credential.Username
		asset.BmcPassword = credential.Password
		asset.BmcAddress = net.ParseIP(serverAttributes[bmcIPAddressAttributeKey])
	}

	return asset, nil
}

func validateRequiredAttributes(server *fleetdbapi.Server, credential *fleetdbapi.ServerCredential) error {
	if server == nil {
		return errors.New("server object nil")
	}

	if credential == nil {
		return errors.New("server credential object nil")
	}

	if len(server.Attributes) == 0 {
		return errors.New("server attributes slice empty")
	}

	if credential.Username == "" {
		return errors.New("BMC username field empty")
	}

	if credential.Password == "" {
		return errors.New("BMC password field empty")
	}

	return nil
}

// serverAttributes parses the server service attribute data
// and returns a map containing the bmc address, server serial, vendor, model attributes
func serverAttributes(attributes []fleetdbapi.Attributes) (map[string]string, error) {
	// returned server attributes map
	sAttributes := map[string]string{}

	// bmc IP Address attribute data is unpacked into this map
	bmcData := map[string]string{}

	// server vendor, model attribute data is unpacked into this map
	serverVendorData := map[string]string{}

	for _, attribute := range attributes {
		// bmc address attribute
		if attribute.Namespace == bmcAttributeNamespace {
			if err := json.Unmarshal(attribute.Data, &bmcData); err != nil {
				return nil, errors.Wrap(ErrServerServiceObject, "bmc address attribute: "+err.Error())
			}
		}

		// server vendor, model attributes
		if attribute.Namespace == serverVendorAttributeNS {
			if err := json.Unmarshal(attribute.Data, &serverVendorData); err != nil {
				return nil, errors.Wrap(ErrServerServiceObject, "server vendor attribute: "+err.Error())
			}
		}
	}

	if len(bmcData) == 0 {
		return nil, errors.New("expected server attributes with BMC address, got none")
	}

	// set bmc address attribute
	sAttributes[bmcIPAddressAttributeKey] = bmcData[bmcIPAddressAttributeKey]
	if sAttributes[bmcIPAddressAttributeKey] == "" {
		return nil, errors.New("expected BMC address attribute empty")
	}

	// set server vendor, model attributes in the returned map
	serverAttributes := []string{
		serverSerialAttributeKey,
		serverModelAttributeKey,
		serverVendorAttributeKey,
	}

	for _, key := range serverAttributes {
		sAttributes[key] = serverVendorData[key]
	}

	return sAttributes, nil
}
