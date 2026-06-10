package cluster

import "errors"

// ErrInvalidConfig is returned when a cluster config fails validation.
var ErrInvalidConfig = errors.New("cluster: invalid config")

// ErrUnsupportedVersion is returned when the config version field is not CurrentVersion.
var ErrUnsupportedVersion = errors.New("cluster: unsupported version")

// ErrUnsupportedRouting is returned when the routing algorithm is not supported.
var ErrUnsupportedRouting = errors.New("cluster: unsupported routing")
