package metrics

import (
	"testing"
	"time"
)

func TestRingBufferAppendAndSeries(t *testing.T) {
	t.Parallel()

	buf := NewRingBuffer(3)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	buf.Append(1, ts)
	buf.Append(2, ts.Add(time.Minute))
	buf.Append(3, ts.Add(2*time.Minute))

	series := buf.Series()
	if len(series.Values) != 3 {
		t.Fatalf("len(Values) = %d, want 3", len(series.Values))
	}
	if series.Values[0] != 1 || series.Values[2] != 3 {
		t.Fatalf("Values = %v, want [1 2 3]", series.Values)
	}
	if series.Timestamps[1] != ts.Add(time.Minute) {
		t.Fatalf("Timestamps[1] = %v, want %v", series.Timestamps[1], ts.Add(time.Minute))
	}
}

func TestRingBufferEvictsOldestWhenFull(t *testing.T) {
	t.Parallel()

	buf := NewRingBuffer(3)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		buf.Append(float64(i), ts.Add(time.Duration(i)*time.Minute))
	}

	series := buf.Series()
	if len(series.Values) != 3 {
		t.Fatalf("len(Values) = %d, want 3", len(series.Values))
	}
	want := []float64{2, 3, 4}
	for i, v := range want {
		if series.Values[i] != v {
			t.Fatalf("Values[%d] = %v, want %v", i, series.Values[i], v)
		}
	}
}

func TestHistoryStorePerKeyIsolation(t *testing.T) {
	t.Parallel()

	store := NewHistoryStore(4)
	ts := time.Now()

	store.Append("ns/a/cpu", 10, ts)
	store.Append("ns/b/cpu", 20, ts)

	if got := store.Len("ns/a/cpu"); got != 1 {
		t.Fatalf("Len(ns/a/cpu) = %d, want 1", got)
	}
	if got := store.Len("ns/b/cpu"); got != 1 {
		t.Fatalf("Len(ns/b/cpu) = %d, want 1", got)
	}

	store.DeleteByPrefix("ns/a/")
	if got := store.Len("ns/a/cpu"); got != 0 {
		t.Fatalf("Len(ns/a/cpu) after delete = %d, want 0", got)
	}
	if got := store.Len("ns/b/cpu"); got != 1 {
		t.Fatalf("Len(ns/b/cpu) after delete = %d, want 1", got)
	}
}

func TestHistoryStoreConcurrentAppend(t *testing.T) {
	t.Parallel()

	store := NewHistoryStore(512)
	ts := time.Now()
	done := make(chan struct{})

	for i := 0; i < 8; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				store.Append("ns/workload", float64(n*100+j), ts)
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 8; i++ {
		<-done
	}

	if got := store.Len("ns/workload"); got != 512 {
		t.Fatalf("Len() = %d, want 512 (capacity cap)", got)
	}
}

func TestHistoryStoreAppendLatest(t *testing.T) {
	t.Parallel()

	store := NewHistoryStore(8)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	key := "ns/workload/cpu"

	store.AppendLatest(key, Series{
		Values:     []float64{100, 200, 300},
		Timestamps: []time.Time{ts, ts.Add(time.Minute), ts.Add(2 * time.Minute)},
	})

	got := store.Get(key)
	if len(got.Values) != 1 || got.Values[0] != 300 {
		t.Fatalf("AppendLatest() = %v, want [300]", got.Values)
	}
}

func TestRecentPeakSamplesPrefersAccumulatedBuffer(t *testing.T) {
	t.Parallel()

	store := NewHistoryStore(512)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	key := "ns/workload/cpu"

	for i := 0; i < 10; i++ {
		v := 200.0
		if i >= 5 {
			v = 2000.0
		}
		store.Append(key, v, ts.Add(time.Duration(i)*20*time.Second))
	}

	smoothed := Series{Values: []float64{200, 250, 300, 350, 400, 450, 500, 550, 600}}
	samples := RecentPeakSamples(store, key, smoothed)
	if len(samples) != 10 {
		t.Fatalf("len(samples) = %d, want 10 accumulated reconcile samples", len(samples))
	}

	var max float64
	for _, v := range samples {
		if v > max {
			max = v
		}
	}
	if max != 2000 {
		t.Fatalf("accumulated max = %v, want 2000", max)
	}

	var rangeMax float64
	for _, v := range smoothed.Values {
		if v > rangeMax {
			rangeMax = v
		}
	}
	if rangeMax >= 2000 {
		t.Fatalf("smoothed range max = %v, should under-estimate burst", rangeMax)
	}
}
