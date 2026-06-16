# Manager image: Kubernetes controller with bundled ONNX Runtime for optional ML forecasting.
#
# Build:
#   docker build -t controller:latest .
#   make docker-build
#
# Run locally (requires kubeconfig; mount models for ML forecasting):
#   docker run --rm \
#     -v "$HOME/.kube/config:/etc/kubeconfig:ro" \
#     -v "$(pwd)/models:/models:ro" \
#     -e KUBECONFIG=/etc/kubeconfig \
#     -e MODEL_PATH=/models/timesfm-2.5-onnx/onnx/model.onnx \
#     controller:latest \
#     --leader-elect \
#     --model-path=/models/timesfm-2.5-onnx/onnx/model.onnx
#
# Bake models into the image (optional):
#   1. Remove `models/` from .dockerignore
#   2. docker build --target manager-baked -t controller:baked .
#
# Environment (forecast flags also accept CLI equivalents):
#   MODEL_PATH                Path to model.onnx
#   FORECAST_MODEL_FAMILY     timesfm | chronos2 (default: timesfm)
#   ONNX_RUNTIME_LIB_PATH     libonnxruntime.so (default: /opt/onnxruntime/lib/libonnxruntime.so)
#   ONNX_RUNTIME_API_VERSION  ORT C API version for purego (default: 23)

# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.4
ARG ONNXRUNTIME_VERSION=1.24.4

FROM golang:${GO_VERSION}-bookworm AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/manager cmd/main.go

FROM debian:bookworm-slim AS ort
ARG ONNXRUNTIME_VERSION

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && curl -fsSL \
      "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz" \
      | tar -xz -C /opt \
    && mv "/opt/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}" /opt/onnxruntime \
    && apt-get purge -y curl \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

FROM debian:bookworm-slim AS manager

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      libstdc++6 \
      libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ort /opt/onnxruntime/lib/ /opt/onnxruntime/lib/
COPY --from=builder /out/manager /manager

ENV LD_LIBRARY_PATH=/opt/onnxruntime/lib \
    ONNX_RUNTIME_LIB_PATH=/opt/onnxruntime/lib/libonnxruntime.so \
    ONNX_RUNTIME_API_VERSION=23 \
    FORECAST_MODEL_FAMILY=timesfm \
    MODEL_PATH=

ENTRYPOINT ["/manager"]
CMD ["--leader-elect"]

FROM manager AS manager-baked
COPY models/ /models/
