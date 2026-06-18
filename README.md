# Neural Autoscaler

A Kubernetes operator that forecasts workload resource usage and resizes pods in-place, no restarts, no replica churn.
The controller collects usage from metrics-server, runs a Chronos-2 ONNX time-series model over a rolling history buffer, 
and applies new container requests through the `pods/resize` subresource.

## How it works

On each reconcile tick the controller runs a predict→resize loop:

1. **Collect** — Fetch current CPU/memory usage from metrics-server for the workload named in `spec.metrics.metricsServer.targetRef`, and append each sample to an in-memory per-resource history buffer.
2. **Forecast** — Once the buffer holds enough samples (16+), run the bundled ONNX forecaster to produce a future usage series over `spec.forecast.horizon` at `spec.forecast.step` intervals.
3. **Derive targets** — When `spec.resize` is set, compute per-pod container requests from forecast peaks (max over horizon and quantiles, with headroom, divided by matching pod count) and clamp to `spec.resize.resources` min/max. Skips resize if the change is below `minChangePercent`.
4. **Apply** — Patch pod requests in place via `pods/resize`. Only requests are predicted; limits are raised only when they would fall below the new request.

## Example

```yaml
apiVersion: autoscaling.plural.sh/v1alpha1
kind: NeuralAutoscaler
metadata:
  name: neuralautoscaler-sample
spec:
  metrics:
    type: MetricsServer
    metricsServer:
      targetRef:
        kind: Deployment
        name: api
      resources:
        - cpu
        - memory
  forecast:
    horizon: 12
    step: 1m
  resize:
    minChangePercent: 5
    resources:
      cpu:
        min: 100m
        max: "8"
      memory:
        min: 128Mi
        max: 16Gi
```

Point `targetRef` at a Deployment, StatefulSet, or ReplicaSet in the same namespace. Omit `resize` to run forecast-only (metrics collection and logging, no pod changes).

## Quick start

**Prerequisites:** Kubernetes 1.27+ with in-place pod vertical scaling (`InPlacePodVerticalScaling`; enabled by default on 1.33+), and metrics-server installed in the cluster.

```bash
# Download the Chronos-2 ONNX model (needed for local runs and image builds)
make download-chronos-onnx

# Install the operator via Helm
helm install neural-autoscaler charts/neural-autoscaler -n neural-autoscaler --create-namespace

# Or run the controller locally against your kubeconfig
make run-local
```

Apply the sample CR after installing: `kubectl apply -f config/samples/autoscaling_v1alpha1_neuralautoscaler.yaml`

For local kind clusters on Kubernetes < 1.33, enable the feature gate with `hack/kind-inplace-config.yaml`.
