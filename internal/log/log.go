// Package log wraps klog with consistent verbosity levels for neural-autoscaler.
//
// Level policy:
//
//   - Info — reconcile outcomes, resize applied, startup/shutdown.
//   - Debug — Prometheus queries, forecast details, skip reasons, per-pod details.
//   - Warning — degraded/non-fatal (empty metrics buffer, threshold skip, insufficient history).
//   - Error — failures.
//
// Verbosity (-v flag, registered in cmd/args):
//
//   - -v=0 (default): Info, Warning, and Error.
//   - -v=4: adds Debug.
package log

import "k8s.io/klog/v2"

// debugLevel is the klog verbosity threshold for Debug messages.
const debugLevel = 4

// Info logs operational events visible at the default -v=0.
func Info(msg string, keysAndValues ...any) {
	klog.InfoS(msg, keysAndValues...)
}

// Debug logs detailed flow; visible when -v>=debugLevel (default: -v=4).
func Debug(msg string, keysAndValues ...any) {
	klog.V(debugLevel).InfoS(msg, keysAndValues...)
}

// Warning logs degraded but non-fatal conditions; always visible at -v=0.
func Warning(msg string, keysAndValues ...any) {
	klog.InfoS(msg, append([]any{"level", "warning"}, keysAndValues...)...)
}

// Error logs failures.
func Error(err error, msg string, keysAndValues ...any) {
	klog.ErrorS(err, msg, keysAndValues...)
}
