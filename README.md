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

### Prometheus metrics source

Instead of metrics-server, you can point at a Prometheus-compatible API (for example Kubecost). PromQL is built automatically from `targetRef` and `resources`; only `url`, optional `auth`, `lookback`, and `step` are required beyond the workload selector:

```yaml
spec:
  metrics:
    type: Prometheus
    prometheus:
      url: http://kubecost-prometheus-server.kubecost.svc
      targetRef:
        kind: Deployment
        name: api
      resources:
        - cpu
        - memory
      lookback: 1h
      step: 1m
```

## Quick start

### Prerequisites

- Kubernetes **1.27+** with [in-place pod vertical scaling](https://kubernetes.io/docs/concepts/workloads/pods/pod-resize/) (`InPlacePodVerticalScaling`; enabled by default on 1.33+). For local kind clusters on Kubernetes < 1.33, enable the feature gate with `hack/kind-inplace-config.yaml`.
- [metrics-server](https://github.com/kubernetes-sigs/metrics-server) installed in the cluster.

### Install the operator

The chart pulls the controller image from `ghcr.io/pluralsh/neural-autoscaler`. Set `image.tag` to the release version you are installing (chart default: `0.2.0`).

```bash
helm install neural-autoscaler ./charts/neural-autoscaler \
  --namespace neural-autoscaler-system \
  --create-namespace
```

### Local development

```bash
# Download the Chronos-2 ONNX model (needed for local runs and image builds)
make download-chronos-onnx

# Run the controller locally against your kubeconfig
make run-local
```

### Local development with Prometheus

When the operator runs on your host (`make run` or `make run-local`), cluster DNS URLs such as `http://kubecost-prometheus-server.kubecost.svc` are not reachable. Port-forward Kubecost Prometheus and apply the local sample CR, which points at `http://127.0.0.1:9090`.

**Terminal 1 — port-forward** (leave running):

```bash
kubectl port-forward -n kubecost svc/kubecost-prometheus-server 9090:80
```

Verify from the host:

```bash
curl -sG 'http://127.0.0.1:9090/api/v1/query' --data-urlencode 'query=up' | head
```

**Terminal 2 — workload, CR, and operator:**

```bash
# Install CRDs if needed
make install

# Demo workload (targetRef.name: api)
kubectl apply -f config/samples/workload-deployment.yaml

# Prometheus sample for local runs (127.0.0.1, not cluster DNS)
kubectl apply -f config/samples/autoscaling_v1alpha1_neuralautoscaler_prometheus_local.yaml

make download-chronos-onnx   # first time only
make run-local
```

For in-cluster deployments, use `config/samples/autoscaling_v1alpha1_neuralautoscaler_prometheus.yaml` with the cluster service URL instead.

### Database (PostgreSQL) demo

StatefulSet workloads such as databases are a common in-place resize target. Apply the bundled `pgbench` burst workload and matching Prometheus CR:

```bash
kubectl apply -f config/samples/workload-postgres.yaml
kubectl apply -f config/samples/autoscaling_v1alpha1_neuralautoscaler_postgres_prometheus_local.yaml  # local operator + port-forward
# or autoscaling_v1alpha1_neuralautoscaler_postgres_prometheus.yaml for in-cluster Prometheus
```

The workload runs `postgres:16-alpine` with `pgbench -i -s 5` init and a 45s burst / 15s sleep loop. Resize floors are `250m` CPU and `512Mi` memory with `minChangePercent: 20`.

### Upgrade / uninstall

```bash
helm upgrade neural-autoscaler ./charts/neural-autoscaler \
  --namespace neural-autoscaler-system \
  --set image.tag=<version>

helm uninstall neural-autoscaler --namespace neural-autoscaler-system
```
