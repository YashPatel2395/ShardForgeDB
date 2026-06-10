package proxy

import "errors"

// ErrInvalidOptions is returned when Open is called with invalid Options.
var ErrInvalidOptions = errors.New("proxy: invalid options")

// ErrClosed is returned when an operation is attempted on a closed Server.
var ErrClosed = errors.New("proxy: closed")
