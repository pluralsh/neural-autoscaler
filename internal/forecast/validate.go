package forecast

import (
	"fmt"
	"strings"
)

func ValidateRequest(req Request) error {
	if strings.TrimSpace(req.SeriesID) == "" {
		return fmt.Errorf("%w: series id is required", ErrInvalidRequest)
	}
	if len(req.Values) == 0 {
		return fmt.Errorf("%w: at least one historical value is required", ErrInvalidRequest)
	}
	if req.Horizon <= 0 {
		return fmt.Errorf("%w: horizon must be positive", ErrInvalidRequest)
	}
	if len(req.Timestamps) > 0 && len(req.Timestamps) != len(req.Values) {
		return fmt.Errorf("%w: timestamps length must match values length", ErrInvalidRequest)
	}
	for _, q := range req.Quantiles {
		if q <= 0 || q >= 1 {
			return fmt.Errorf("%w: quantile %v must be in (0, 1)", ErrInvalidRequest, q)
		}
	}
	return nil
}
