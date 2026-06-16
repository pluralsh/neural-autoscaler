package forecast

import "errors"

var (
	ErrInvalidRequest  = errors.New("invalid forecast request")
	ErrBackendNotReady = errors.New("forecast backend not ready")
)
