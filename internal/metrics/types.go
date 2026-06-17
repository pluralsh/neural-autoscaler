package metrics

import (
	"context"
	"time"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

// Series is a metric time series returned by a Fetcher.
type Series struct {
	Values     []float64
	Timestamps []time.Time
}

// FetchResult holds per-resource series from a metrics fetch.
type FetchResult struct {
	ByResource map[autoscalingv1alpha1.ResourceMetric]Series
}

// Fetcher retrieves metric samples for a configured source.
type Fetcher interface {
	Fetch(ctx context.Context) (FetchResult, error)
}
