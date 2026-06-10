package gateway

import "errors"

// ErrInvalidOptions is returned when Options validation fails.
var ErrInvalidOptions = errors.New("gateway: invalid options")

// ErrInvalidKey is returned when an empty key is passed to a routing method.
var ErrInvalidKey = errors.New("gateway: invalid key")

// ErrUnknownNode is returned when ScanNode receives a node ID that is not configured.
var ErrUnknownNode = errors.New("gateway: unknown node")

// ErrClosed is returned when an operation is attempted on a closed Gateway.
var ErrClosed = errors.New("gateway: closed")
