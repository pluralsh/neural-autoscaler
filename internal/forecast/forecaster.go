package forecast

import "context"

type Forecaster interface {
	Forecast(ctx context.Context, req Request) (Response, error)
	Close() error
}

type HealthChecker interface {
	Ready(ctx context.Context) error
}
