package wire

import (
	"errors"
)

var (
	ErrMagicMismatch   = errors.New("magic number mismatch")
	ErrUnknownVersion  = errors.New("unknown header version")
	ErrUnknownCmd      = errors.New("unknown command")
	ErrUnknownFlags    = errors.New("unknown flags")
	ErrPayloadTooLarge = errors.New("payload too large")
)
