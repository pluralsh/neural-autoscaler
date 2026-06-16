package metrics

import "errors"

var (
	ErrUnsupportedSource = errors.New("unsupported metrics source")
	ErrEmptySeries       = errors.New("metrics fetch returned no samples")
)
