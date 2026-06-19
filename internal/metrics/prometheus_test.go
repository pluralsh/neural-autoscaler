package metrics

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPrometheusValueToSeriesQueryRange(t *testing.T) {
	t.Parallel()

	value := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{"pod": "api-1"},
			Values: []model.SamplePair{
				{Timestamp: model.TimeFromUnix(1700000000), Value: 250},
				{Timestamp: model.TimeFromUnix(1700000060), Value: 300},
			},
		},
	}

	series, err := prometheusValueToSeries(value, autoscalingv1alpha1.PrometheusQueryRange)
	if err != nil {
		t.Fatalf("prometheusValueToSeries() error = %v", err)
	}
	if len(series.Values) != 2 {
		t.Fatalf("len(series.Values) = %d, want 2", len(series.Values))
	}
	if series.Values[0] != 250 || series.Values[1] != 300 {
		t.Fatalf("series.Values = %v, want [250 300]", series.Values)
	}
	if series.Timestamps[0].Unix() != 1700000000 {
		t.Fatalf("first timestamp = %v, want unix 1700000000", series.Timestamps[0])
	}
}

func TestPrometheusValueToSeriesQueryInstant(t *testing.T) {
	t.Parallel()

	value := model.Vector{
		{Metric: model.Metric{"pod": "api-1"}, Timestamp: model.TimeFromUnix(1700000000), Value: 100},
		{Metric: model.Metric{"pod": "api-2"}, Timestamp: model.TimeFromUnix(1700000000), Value: 150},
	}

	series, err := prometheusValueToSeries(value, autoscalingv1alpha1.PrometheusQueryInstant)
	if err != nil {
		t.Fatalf("prometheusValueToSeries() error = %v", err)
	}
	if len(series.Values) != 1 || series.Values[0] != 250 {
		t.Fatalf("series.Values = %v, want [250]", series.Values)
	}
}

func TestWrapPrometheusQueryError(t *testing.T) {
	t.Parallel()

	err := wrapPrometheusQueryError("query", "http://prometheus:9090", &v1.Error{Msg: "invalid query"})
	if err == nil || !strings.Contains(err.Error(), "invalid query") {
		t.Fatalf("wrapPrometheusQueryError() error = %v, want invalid query", err)
	}
}

func TestInferResourceFromQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query string
		want  autoscalingv1alpha1.ResourceMetric
	}{
		{`sum(rate(container_cpu_usage_seconds_total[5m]))`, autoscalingv1alpha1.ResourceMetricCPU},
		{`sum(container_memory_working_set_bytes)`, autoscalingv1alpha1.ResourceMetricMemory},
	}
	for _, tt := range tests {
		if got := inferResourceFromQuery(tt.query); got != tt.want {
			t.Fatalf("inferResourceFromQuery(%q) = %q, want %q", tt.query, got, tt.want)
		}
	}
}

func TestPrometheusFetcherFetch(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	body := `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [{
				"metric": {"pod": "api-1"},
				"values": [
					[1700000000, "250"],
					[1700000060, "300"]
				]
			}]
		}
	}`

	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/api/v1/query_range") {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		params, err := prometheusRequestParams(req)
		if err != nil {
			t.Fatalf("prometheusRequestParams() error = %v", err)
		}
		if params.Get("query") == "" {
			t.Fatal("expected query parameter")
		}
		step := params.Get("step")
		if step != "60" && step != "60s" {
			t.Fatalf("step = %q, want 60 or 60s", step)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})

	factory := &Factory{
		HTTPClient: &http.Client{Transport: client},
		Now:        func() time.Time { return fixedNow },
	}
	fetcher, err := factory.NewFetcher(autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourcePrometheus,
		Prometheus: &autoscalingv1alpha1.PrometheusSourceSpec{
			URL:   "http://prometheus:9090",
			Query: `sum(rate(container_cpu_usage_seconds_total{namespace="default"}[5m]))`,
		},
	}, "default")
	if err != nil {
		t.Fatalf("NewFetcher() error = %v", err)
	}

	result, err := fetcher.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	cpu, ok := result.ByResource[autoscalingv1alpha1.ResourceMetricCPU]
	if !ok {
		t.Fatalf("ByResource = %#v, want cpu series", result.ByResource)
	}
	if len(cpu.Values) != 2 {
		t.Fatalf("len(cpu.Values) = %d, want 2", len(cpu.Values))
	}
}

func TestPrometheusQueryHTTPError(t *testing.T) {
	t.Parallel()

	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(`{"status":"error","error":"bad query"}`)),
			Header:     make(http.Header),
		}, nil
	})

	f := &prometheusFetcher{
		factory: &Factory{HTTPClient: &http.Client{Transport: client}},
		spec: autoscalingv1alpha1.PrometheusSourceSpec{
			URL: "http://prometheus:9090",
		},
	}

	_, err := f.query(context.Background(), autoscalingv1alpha1.PrometheusQueryInstant, "up", time.Now(), time.Now(), 0)
	if err == nil {
		t.Fatal("query() error = nil, want HTTP error")
	}
	if !strings.Contains(err.Error(), "bad query") || !strings.Contains(err.Error(), "prometheus query failed") {
		t.Fatalf("query() error = %v, want prometheus query failed with bad query", err)
	}
}

func TestPrometheusQueryConnectionError(t *testing.T) {
	t.Parallel()

	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, io.EOF
	})

	f := &prometheusFetcher{
		factory: &Factory{HTTPClient: &http.Client{Transport: client}},
		spec: autoscalingv1alpha1.PrometheusSourceSpec{
			URL: "http://prometheus:9090",
		},
	}

	_, err := f.query(context.Background(), autoscalingv1alpha1.PrometheusQueryInstant, "up", time.Now(), time.Now(), 0)
	if err == nil {
		t.Fatal("query() error = nil, want connection error")
	}
	if !strings.Contains(err.Error(), "GET http://prometheus:9090") {
		t.Fatalf("query() error = %v, want URL in error", err)
	}
}

func TestNormalizePrometheusURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"http://prometheus:9090", "http://prometheus:9090"},
		{"http://prometheus:9090/", "http://prometheus:9090"},
		{"http://prometheus:9090/api/v1", "http://prometheus:9090"},
		{"http://prometheus:9090/api/v1/", "http://prometheus:9090"},
	}
	for _, tt := range tests {
		got, err := normalizePrometheusURL(tt.in)
		if err != nil {
			t.Fatalf("normalizePrometheusURL(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("normalizePrometheusURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPrometheusFetcherAuth(t *testing.T) {
	t.Parallel()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "prom-auth"},
		Data:       map[string][]byte{"token": []byte("secret-token")},
	}
	k8sClient := fake.NewClientBuilder().WithObjects(secret).Build()

	var authHeader string
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		authHeader = req.Header.Get("Authorization")
		body := `{"status":"success","data":{"resultType":"vector","result":[{"value":[1700000000,"1"]}]}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})

	factory := &Factory{
		K8sClient:  k8sClient,
		HTTPClient: &http.Client{Transport: client},
		Now:        func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
	fetcher, err := factory.NewFetcher(autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourcePrometheus,
		Prometheus: &autoscalingv1alpha1.PrometheusSourceSpec{
			URL:       "http://prometheus:9090",
			Query:     "up",
			QueryType: autoscalingv1alpha1.PrometheusQueryInstant,
			Auth:      &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "prom-auth"}, Key: "token"},
		},
	}, "default")
	if err != nil {
		t.Fatalf("NewFetcher() error = %v", err)
	}
	if _, err := fetcher.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if authHeader != "Bearer secret-token" {
		t.Fatalf("Authorization = %q, want Bearer secret-token", authHeader)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func prometheusRequestParams(req *http.Request) (url.Values, error) {
	if req.Method == http.MethodPost {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		return url.ParseQuery(string(body))
	}
	return req.URL.Query(), nil
}
