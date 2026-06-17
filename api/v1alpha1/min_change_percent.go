package v1alpha1

// EffectiveMinChangePercent returns the per-resource override when set, otherwise
// the global resize value, otherwise DefaultMinChangePercent.
func EffectiveMinChangePercent(global, perResource *int32) int32 {
	if perResource != nil {
		return *perResource
	}
	if global != nil {
		return *global
	}
	return DefaultMinChangePercent
}
