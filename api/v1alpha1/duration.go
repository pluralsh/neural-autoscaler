package v1alpha1

import (
	"strings"
	"time"
)

// SetDuration parses an optional Go duration string into a time.Duration pointer.
// Nil or blank strings return nil without error.
func SetDuration(interval *string) (*time.Duration, error) {
	if interval == nil || strings.TrimSpace(*interval) == "" {
		return nil, nil
	}
	duration, err := time.ParseDuration(strings.TrimSpace(*interval))
	if err != nil {
		return nil, err
	}
	return &duration, nil
}
