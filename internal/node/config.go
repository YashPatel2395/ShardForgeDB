package node

import (
	"errors"
	"fmt"
	"os"
)

// ErrInvalidOptions is returned when Options validation fails.
var ErrInvalidOptions = errors.New("node: invalid options")

// ErrClosed is returned when an operation is attempted on a closed Server.
var ErrClosed = errors.New("node: closed")

// validate checks that required Options fields are set and the DataDir can be created.
func (o Options) validate() error {
	if o.NodeID == "" {
		return fmt.Errorf("%w: NodeID is required", ErrInvalidOptions)
	}
	if o.Addr == "" {
		return fmt.Errorf("%w: Addr is required", ErrInvalidOptions)
	}
	if o.DataDir == "" {
		return fmt.Errorf("%w: DataDir is required", ErrInvalidOptions)
	}
	if err := os.MkdirAll(o.DataDir, 0o755); err != nil {
		return fmt.Errorf("%w: cannot create DataDir %q: %v", ErrInvalidOptions, o.DataDir, err)
	}
	return nil
}
