package metrics

import (
	"strings"
	"sync"
	"time"
)

const (
	// DefaultHistoryCapacity is the max samples retained per NeuralAutoscaler (Chronos context length).
	DefaultHistoryCapacity = 512
	// MinForecastSamples is the minimum buffered history required before running a forecast.
	MinForecastSamples = 16
)

// RingBuffer stores (timestamp, value) samples in a fixed-capacity ring; oldest samples are evicted when full.
type RingBuffer struct {
	values     []float64
	timestamps []time.Time
	capacity   int
	start      int
	count      int
}

// NewRingBuffer returns a ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultHistoryCapacity
	}
	return &RingBuffer{
		capacity: capacity,
	}
}

// Append adds a sample, evicting the oldest entry when the buffer is full.
func (b *RingBuffer) Append(value float64, ts time.Time) {
	if b.capacity <= 0 {
		return
	}
	if b.count < b.capacity {
		if b.count == 0 {
			b.values = make([]float64, b.capacity)
			b.timestamps = make([]time.Time, b.capacity)
		}
		idx := (b.start + b.count) % b.capacity
		b.values[idx] = value
		b.timestamps[idx] = ts
		b.count++
		return
	}
	b.values[b.start] = value
	b.timestamps[b.start] = ts
	b.start = (b.start + 1) % b.capacity
}

// Len returns the number of samples currently stored.
func (b *RingBuffer) Len() int {
	return b.count
}

// Series returns a copy of the buffered samples in chronological order.
func (b *RingBuffer) Series() Series {
	if b.count == 0 {
		return Series{}
	}
	values := make([]float64, b.count)
	timestamps := make([]time.Time, b.count)
	for i := 0; i < b.count; i++ {
		idx := (b.start + i) % b.capacity
		values[i] = b.values[idx]
		timestamps[i] = b.timestamps[idx]
	}
	return Series{Values: values, Timestamps: timestamps}
}

// HistoryStore holds per-key ring buffers for metrics-server history accumulation.
type HistoryStore struct {
	mu       sync.RWMutex
	buffers  map[string]*RingBuffer
	capacity int
}

// NewHistoryStore returns a thread-safe store with the given per-key capacity.
func NewHistoryStore(capacity int) *HistoryStore {
	if capacity <= 0 {
		capacity = DefaultHistoryCapacity
	}
	return &HistoryStore{
		buffers:  make(map[string]*RingBuffer),
		capacity: capacity,
	}
}

// Append adds a sample to the buffer for key (typically namespace/name).
func (s *HistoryStore) Append(key string, value float64, ts time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf, ok := s.buffers[key]
	if !ok {
		buf = NewRingBuffer(s.capacity)
		s.buffers[key] = buf
	}
	buf.Append(value, ts)
}

// Get returns the buffered series for key, or an empty series if none exists.
func (s *HistoryStore) Get(key string) Series {
	s.mu.RLock()
	defer s.mu.RUnlock()

	buf, ok := s.buffers[key]
	if !ok {
		return Series{}
	}
	return buf.Series()
}

// Len returns the number of buffered samples for key.
func (s *HistoryStore) Len(key string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	buf, ok := s.buffers[key]
	if !ok {
		return 0
	}
	return buf.Len()
}

// Delete removes the buffer for key.
func (s *HistoryStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buffers, key)
}

// AppendLatest adds the last sample from snapshot to the buffer for key.
func (s *HistoryStore) AppendLatest(key string, snapshot Series) {
	if len(snapshot.Values) == 0 {
		return
	}
	ts := snapshot.Timestamps[len(snapshot.Timestamps)-1]
	s.Append(key, snapshot.Values[len(snapshot.Values)-1], ts)
}

// RecentPeakSamples returns reconcile-interval accumulated samples when available,
// otherwise the fetched series. Prometheus range queries are coarse; the buffer
// preserves short bursts for RecentPeak resize floors.
func RecentPeakSamples(store *HistoryStore, key string, fetched Series) []float64 {
	if store != nil {
		if s := store.Get(key); len(s.Values) > 0 {
			return s.Values
		}
	}
	return fetched.Values
}

// DeleteByPrefix removes all buffers whose keys start with prefix.
func (s *HistoryStore) DeleteByPrefix(prefix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.buffers {
		if strings.HasPrefix(key, prefix) {
			delete(s.buffers, key)
		}
	}
}
