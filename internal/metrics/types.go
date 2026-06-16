package metrics

import (
	"context"
	"time"
)

// Series is a metric time series returned by a Fetcher.
type Series struct {
	Values     []float64
	Timestamps []time.Time
}

// Fetcher retrieves metric samples for a configured source.
type Fetcher interface {
	Fetch(ctx context.Context) (Series, error)
}
