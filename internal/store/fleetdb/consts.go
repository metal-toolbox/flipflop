package fleetdb

// TODO: move these consts into the hollow-toolbox to share between controllers.

const (
	// server service attribute to look up the BMC IP Address in
	bmcAttributeNamespace = "sh.hollow.bmc_info"

	// server server service BMC address attribute key found under the bmcAttributeNamespace
	bmcIPAddressAttributeKey = "address"

	// serverservice namespace prefix the data is stored in.
	serverServiceNSPrefix = "sh.hollow.alloy"

	// server vendor, model attributes are stored in this namespace.
	serverVendorAttributeNS = serverServiceNSPrefix + ".server_vendor_attributes"

	// additional server metadata are stored in this namespace.
	serverMetadataAttributeNS = serverServiceNSPrefix + ".server_metadata_attributes"

	// errors that occurred when connecting/collecting inventory from the bmc are stored here.
	serverBMCErrorsAttributeNS = serverServiceNSPrefix + ".server_bmc_errors"

	// server service server serial attribute key
	serverSerialAttributeKey = "serial"

	// server service server model attribute key
	serverModelAttributeKey = "model"

	// server service server vendor attribute key
	serverVendorAttributeKey = "vendor"
)
