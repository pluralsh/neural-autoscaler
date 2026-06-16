package forecast

import "time"

type Request struct {
	SeriesID   string
	Values     []float64
	Timestamps []time.Time
	Horizon    int
	Step       time.Duration
	Quantiles  []float64
}

type QuantileSeries struct {
	Level  float64
	Values []float64
}

type Response struct {
	Point        []float64
	Quantiles    []QuantileSeries
	ModelVersion string
}

type Config struct {
	Options map[string]string
}
