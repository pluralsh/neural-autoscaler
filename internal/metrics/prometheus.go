package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/log"
)

const (
	defaultPrometheusLookback = time.Hour
	defaultPrometheusStep     = time.Minute
)

// HTTPDoer performs HTTP requests. The default http.Client is used when unset.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type prometheusFetcher struct {
	factory   *Factory
	spec      autoscalingv1alpha1.PrometheusSourceSpec
	namespace string
}

func newPrometheusFetcher(factory *Factory, spec autoscalingv1alpha1.PrometheusSourceSpec, crNamespace string) Fetcher {
	namespace := crNamespace
	if spec.Namespace != "" {
		namespace = spec.Namespace
	}
	return &prometheusFetcher{
		factory:   factory,
		spec:      spec,
		namespace: namespace,
	}
}

func (f *prometheusFetcher) Fetch(ctx context.Context) (FetchResult, error) {
	if f.spec.TargetRef != nil {
		return f.fetchByResources(ctx)
	}
	return f.fetchLegacyQuery(ctx)
}

func (f *prometheusFetcher) fetchByResources(ctx context.Context) (FetchResult, error) {
	podNames, err := ResolvePodNames(ctx, f.factory.K8sClient, f.namespace, *f.spec.TargetRef)
	if err != nil {
		return FetchResult{}, err
	}

	podMatcher, exactPod := PodMatcher(podNames, *f.spec.TargetRef)
	out := FetchResult{
		ByResource: make(map[autoscalingv1alpha1.ResourceMetric]Series, len(f.spec.Resources)),
		PodNames:   append([]string(nil), podNames...),
	}

	for _, resource := range f.spec.Resources {
		query, err := BuildPrometheusQuery(f.namespace, resource, podMatcher, exactPod)
		if err != nil {
			return FetchResult{}, err
		}

		series, err := f.fetchSeries(ctx, query)
		if err != nil {
			return FetchResult{}, fmt.Errorf("prometheus %s: %w", resource, err)
		}
		if len(series.Values) == 0 {
			return FetchResult{}, fmt.Errorf("prometheus %s: %w", resource, ErrEmptySeries)
		}
		out.ByResource[resource] = series
	}

	return out, nil
}

func (f *prometheusFetcher) fetchLegacyQuery(ctx context.Context) (FetchResult, error) {
	series, err := f.fetchSeries(ctx, f.spec.Query)
	if err != nil {
		return FetchResult{}, err
	}
	if len(series.Values) == 0 {
		return FetchResult{}, ErrEmptySeries
	}

	resource := inferResourceFromQuery(f.spec.Query)
	return FetchResult{
		ByResource: map[autoscalingv1alpha1.ResourceMetric]Series{
			resource: series,
		},
	}, nil
}

func (f *prometheusFetcher) fetchSeries(ctx context.Context, promQL string) (Series, error) {
	queryType, start, end, step, err := f.resolveQueryWindow()
	if err != nil {
		return Series{}, err
	}
	return f.query(ctx, queryType, promQL, start, end, step)
}

func (f *prometheusFetcher) resolveQueryWindow() (autoscalingv1alpha1.PrometheusQueryType, time.Time, time.Time, time.Duration, error) {
	queryType := f.spec.QueryType
	if queryType == "" {
		queryType = autoscalingv1alpha1.PrometheusQueryRange
	}

	step, err := f.resolveStep(queryType)
	if err != nil {
		return "", time.Time{}, time.Time{}, 0, err
	}

	now := f.factory.now()
	start, end := now, now
	if queryType == autoscalingv1alpha1.PrometheusQueryRange {
		lookback, err := f.resolveLookback()
		if err != nil {
			return "", time.Time{}, time.Time{}, 0, err
		}
		start = now.Add(-lookback)
	}
	return queryType, start, end, step, nil
}

func (f *prometheusFetcher) resolveStep(queryType autoscalingv1alpha1.PrometheusQueryType) (time.Duration, error) {
	if queryType != autoscalingv1alpha1.PrometheusQueryRange {
		return 0, nil
	}
	return resolvePrometheusDuration("step", f.spec.Step, defaultPrometheusStep)
}

func (f *prometheusFetcher) resolveLookback() (time.Duration, error) {
	return resolvePrometheusDuration("lookback", f.spec.Lookback, defaultPrometheusLookback)
}

func resolvePrometheusDuration(field string, raw *string, fallback time.Duration) (time.Duration, error) {
	parsed, err := autoscalingv1alpha1.SetDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("prometheus.%s: %w", field, err)
	}
	if parsed != nil && *parsed > 0 {
		return *parsed, nil
	}
	return fallback, nil
}

func (f *prometheusFetcher) query(ctx context.Context, queryType autoscalingv1alpha1.PrometheusQueryType, promQL string, start, end time.Time, step time.Duration) (Series, error) {
	endpoint := "query_range"
	if queryType == autoscalingv1alpha1.PrometheusQueryInstant {
		endpoint = "query"
	}

	baseURL, err := normalizePrometheusURL(f.spec.URL)
	if err != nil {
		return Series{}, err
	}
	redactedURL := redactPrometheusURL(baseURL)

	client, err := f.prometheusHTTPClient(ctx)
	if err != nil {
		return Series{}, err
	}

	promClient, err := api.NewClient(api.Config{
		Address: baseURL,
		Client:  client,
	})
	if err != nil {
		return Series{}, fmt.Errorf("build prometheus client: %w", err)
	}

	apiClient := v1.NewAPI(promClient)

	var (
		value model.Value
		qerr  error
	)
	switch queryType {
	case autoscalingv1alpha1.PrometheusQueryInstant:
		value, _, qerr = apiClient.Query(ctx, promQL, end)
	default:
		value, _, qerr = apiClient.QueryRange(ctx, promQL, v1.Range{
			Start: start,
			End:   end,
			Step:  step,
		})
	}
	if qerr != nil {
		log.Error(qerr, "prometheus query failed", "endpoint", endpoint, "url", redactedURL)
		return Series{}, wrapPrometheusQueryError(endpoint, redactedURL, qerr)
	}

	series, err := prometheusValueToSeries(value, queryType)
	if err != nil {
		return Series{}, err
	}
	log.Debug("prometheus query succeeded", "endpoint", endpoint, "url", redactedURL, "samples", len(series.Values))
	return series, nil
}

func wrapPrometheusQueryError(endpoint, redactedURL string, err error) error {
	var apiErr *v1.Error
	if errors.As(err, &apiErr) {
		if apiErr.Msg != "" {
			return fmt.Errorf("prometheus query failed: %s", apiErr.Msg)
		}
		if apiErr.Detail != "" {
			return fmt.Errorf("prometheus %s GET %s returned HTTP error: %s", endpoint, redactedURL, truncateForError([]byte(apiErr.Detail)))
		}
	}
	return fmt.Errorf("prometheus %s GET %s: %w", endpoint, redactedURL, err)
}

func (f *prometheusFetcher) prometheusHTTPClient(ctx context.Context) (*http.Client, error) {
	base := http.DefaultClient
	if f.factory != nil && f.factory.HTTPClient != nil {
		if c, ok := f.factory.HTTPClient.(*http.Client); ok {
			base = c
		} else {
			base = &http.Client{Transport: doerRoundTripper{doer: f.factory.HTTPClient}}
		}
	}

	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	if f.spec.Auth != nil {
		token, err := f.resolveBearerToken(ctx)
		if err != nil {
			return nil, err
		}
		transport = bearerRoundTripper{base: transport, token: token}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   base.Timeout,
	}, nil
}

func (f *prometheusFetcher) resolveBearerToken(ctx context.Context) (string, error) {
	if f.spec.Auth == nil {
		return "", nil
	}
	if f.factory.K8sClient == nil {
		return "", fmt.Errorf("kubernetes client is required for prometheus auth")
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: f.namespace, Name: f.spec.Auth.Name}
	if err := f.factory.K8sClient.Get(ctx, key, secret); err != nil {
		return "", fmt.Errorf("get auth secret %q: %w", key, err)
	}

	token, ok := secret.Data[f.spec.Auth.Key]
	if !ok {
		return "", fmt.Errorf("auth secret %q is missing key %q", key, f.spec.Auth.Key)
	}
	value := strings.TrimSpace(string(token))
	if value == "" {
		return "", fmt.Errorf("auth secret %q key %q is empty", key, f.spec.Auth.Key)
	}

	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return value, nil
	}
	return "Bearer " + value, nil
}

type doerRoundTripper struct {
	doer HTTPDoer
}

func (d doerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return d.doer.Do(req)
}

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", b.token)
	return b.base.RoundTrip(req)
}

func normalizePrometheusURL(raw string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return "", fmt.Errorf("prometheus.url is required")
	}
	// Allow users to pass either the server root or an /api/v1 prefix.
	if strings.HasSuffix(baseURL, "/api/v1") {
		baseURL = strings.TrimSuffix(baseURL, "/api/v1")
	}
	return baseURL, nil
}

func redactPrometheusURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.RawQuery = ""
	if parsed.Path != "" && parsed.Path != "/" {
		return parsed.String()
	}
	return parsed.Scheme + "://" + parsed.Host
}

func truncateForError(body []byte) string {
	const maxLen = 256
	text := strings.TrimSpace(string(body))
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func inferResourceFromQuery(query string) autoscalingv1alpha1.ResourceMetric {
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "memory"), strings.Contains(q, "container_memory"), strings.Contains(q, "_bytes"):
		return autoscalingv1alpha1.ResourceMetricMemory
	default:
		return autoscalingv1alpha1.ResourceMetricCPU
	}
}

func prometheusValueToSeries(value model.Value, queryType autoscalingv1alpha1.PrometheusQueryType) (Series, error) {
	if value == nil {
		return Series{}, nil
	}

	switch queryType {
	case autoscalingv1alpha1.PrometheusQueryInstant:
		vec, ok := value.(model.Vector)
		if !ok {
			return Series{}, fmt.Errorf("prometheus query returned %T, want vector", value)
		}
		return prometheusVectorToSeries(vec), nil
	default:
		matrix, ok := value.(model.Matrix)
		if !ok {
			return Series{}, fmt.Errorf("prometheus query returned %T, want matrix", value)
		}
		return prometheusMatrixToSeries(matrix), nil
	}
}

func prometheusVectorToSeries(vec model.Vector) Series {
	if len(vec) == 0 {
		return Series{}
	}

	var (
		ts    time.Time
		total float64
		have  bool
	)
	for _, sample := range vec {
		if !have {
			ts = sample.Timestamp.Time().UTC()
			have = true
		}
		total += float64(sample.Value)
	}
	if !have {
		return Series{}
	}
	return Series{
		Values:     []float64{total},
		Timestamps: []time.Time{ts},
	}
}

func prometheusMatrixToSeries(matrix model.Matrix) Series {
	if len(matrix) == 0 {
		return Series{}
	}

	merged := Series{}
	for _, stream := range matrix {
		series := sampleStreamToSeries(stream)
		merged = mergeSeries(merged, series)
	}
	return merged
}

func sampleStreamToSeries(stream *model.SampleStream) Series {
	series := Series{
		Values:     make([]float64, 0, len(stream.Values)),
		Timestamps: make([]time.Time, 0, len(stream.Values)),
	}
	for _, pair := range stream.Values {
		series.Timestamps = append(series.Timestamps, pair.Timestamp.Time().UTC())
		series.Values = append(series.Values, float64(pair.Value))
	}
	return series
}

func mergeSeries(base, add Series) Series {
	if len(base.Values) == 0 {
		return add
	}
	if len(add.Values) == 0 {
		return base
	}
	if len(base.Values) != len(add.Values) {
		return add
	}

	out := Series{
		Values:     make([]float64, len(base.Values)),
		Timestamps: append([]time.Time(nil), base.Timestamps...),
	}
	for i := range base.Values {
		out.Values[i] = base.Values[i] + add.Values[i]
	}
	return out
}
