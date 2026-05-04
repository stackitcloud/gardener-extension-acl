package allowedcidrs

import "errors"

var ErrNoAdvertisedAddresses = errors.New("advertised addresses are not available, likely because cluster creation has not yet completed")
