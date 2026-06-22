package args

import (
	"flag"
	"os"
	"strconv"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	EnvModelPath             = "MODEL_PATH"
	EnvModelFamily           = "FORECAST_MODEL_FAMILY"
	EnvONNXRuntimeLibPath    = "ONNX_RUNTIME_LIB_PATH"
	EnvONNXRuntimeAPIVersion = "ONNX_RUNTIME_API_VERSION"

	defaultMetricsAddress        = ":8080"
	defaultProbeAddress          = ":8081"
	defaultTimesFMMaxContext     = 1024
	defaultONNXRuntimeAPIVersion = 23
	defaultIntraOpThreads        = 4
)

var (
	argMetricsAddr          = flag.String("metrics-bind-address", defaultMetricsAddress, "The address the metric endpoint binds to.")
	argProbeAddr            = flag.String("health-probe-bind-address", defaultProbeAddress, "The address the probe endpoint binds to.")
	argEnableLeaderElection = flag.Bool("leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	argModelPath = flag.String("model-path", getEnv(EnvModelPath, ""),
		"Path to ONNX model file (model.onnx). Enables ML forecasting when set. Fallback env: MODEL_PATH.")
	argModelFamily = flag.String("model-family", getEnv(EnvModelFamily, ""),
		"ONNX model family: timesfm or chronos2 (optional; auto-discovered from model when unset). Fallback env: FORECAST_MODEL_FAMILY.")
	argTimesFMMaxContext = flag.Int("timesfm-max-context", defaultTimesFMMaxContext,
		"Maximum historical context length passed to TimesFM ONNX.")
	argONNXRuntimeLibPath = flag.String("onnx-runtime-lib-path", getEnv(EnvONNXRuntimeLibPath, ""),
		"Optional path to libonnxruntime shared library. Fallback env: ONNX_RUNTIME_LIB_PATH.")
	argONNXRuntimeAPIVersion = flag.Int("onnx-runtime-api-version", getEnvInt(EnvONNXRuntimeAPIVersion, defaultONNXRuntimeAPIVersion),
		"ONNX Runtime API version used by onnxruntime-purego (23).")
)

// Init parses flags and wires klog into controller-runtime.
// Logging verbosity is controlled by the standard klog -v flag:
//   - -v=0 (default): Info, Warning, and Error
//   - -v=4: adds Debug (Prometheus queries, forecast details, per-pod skips)
func Init() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	resolveEnvFallbacks()

	ctrl.SetLogger(klog.NewKlogr())
}

func resolveEnvFallbacks() {
	if *argModelPath == "" {
		*argModelPath = os.Getenv(EnvModelPath)
	}
	if *argModelFamily == "" {
		*argModelFamily = os.Getenv(EnvModelFamily)
	}
	if *argONNXRuntimeLibPath == "" {
		*argONNXRuntimeLibPath = os.Getenv(EnvONNXRuntimeLibPath)
	}
}

func MetricsAddr() string {
	return *argMetricsAddr
}

func ProbeAddr() string {
	return *argProbeAddr
}

func EnableLeaderElection() bool {
	return *argEnableLeaderElection
}

func ModelPath() string {
	return *argModelPath
}

func ModelFamily() string {
	return *argModelFamily
}

func TimesFMMaxContext() int {
	return *argTimesFMMaxContext
}

func ONNXRuntimeLibPath() string {
	return *argONNXRuntimeLibPath
}

func ONNXRuntimeAPIVersion() int {
	return *argONNXRuntimeAPIVersion
}

func ForecastEnabled() bool {
	return ModelPath() != ""
}

func ForecastOptions() map[string]string {
	return map[string]string{
		"model_path":       ModelPath(),
		"model_family":     ModelFamily(),
		"max_context":      strconv.Itoa(TimesFMMaxContext()),
		"runtime_lib_path": ONNXRuntimeLibPath(),
		"ort_api_version":  strconv.Itoa(ONNXRuntimeAPIVersion()),
		"intra_op_threads": strconv.Itoa(defaultIntraOpThreads),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
