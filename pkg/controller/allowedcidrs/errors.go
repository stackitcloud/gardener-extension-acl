package allowedcidrs

import "errors"

// ErrNoAdvertisedAddresses is returned if a cluster or garden does not contain advertised addresses
var ErrNoAdvertisedAddresses = errors.New("advertised addresses are not available, likely because cluster creation has not yet completed")
