package resize

// DefaultRecentPeakWindow is the number of latest history samples used to floor
// forecast peaks. At a 20s reconcile interval this spans ~3 minutes, covering
// several burst cycles for bursty CPU workloads.
const DefaultRecentPeakWindow = 9

// RecentPeak returns the maximum value in the last window samples of history.
func RecentPeak(values []float64, window int) float64 {
	if len(values) == 0 {
		return 0
	}
	if window <= 0 {
		window = DefaultRecentPeakWindow
	}
	start := len(values) - window
	if start < 0 {
		start = 0
	}
	var peak float64
	for _, v := range values[start:] {
		if v > peak {
			peak = v
		}
	}
	return peak
}

// EffectivePeak returns the larger of the model forecast peak and recent observed peak.
func EffectivePeak(forecastPeak, observedPeak float64) float64 {
	if observedPeak > forecastPeak {
		return observedPeak
	}
	return forecastPeak
}
