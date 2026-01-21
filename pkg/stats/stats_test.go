package stats

import (
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	tests := []struct {
		name      string
		durations []time.Duration
		p         float64
		expected  time.Duration
	}{
		{
			name:      "empty",
			durations: []time.Duration{},
			p:         50,
			expected:  0,
		},
		{
			name:      "single value p50",
			durations: []time.Duration{100 * time.Millisecond},
			p:         50,
			expected:  100 * time.Millisecond,
		},
		{
			name:      "ordered values p50",
			durations: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond},
			p:         50,
			expected:  20 * time.Millisecond,
		},
		{
			name:      "ordered values p99",
			durations: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, 40 * time.Millisecond, 50 * time.Millisecond},
			p:         99,
			expected:  50 * time.Millisecond, // Near max
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Percentile(tt.durations, tt.p)
			// Allow small margin for interpolation
			diff := result - tt.expected
			if diff < 0 {
				diff = -diff
			}
			margin := time.Millisecond * 5
			if len(tt.durations) > 0 && diff > margin {
				t.Errorf("Percentile(%v, %.0f) = %v, want ~%v", tt.durations, tt.p, result, tt.expected)
			}
		})
	}
}

func TestAverage(t *testing.T) {
	tests := []struct {
		name      string
		durations []time.Duration
		expected  time.Duration
	}{
		{
			name:      "empty",
			durations: []time.Duration{},
			expected:  0,
		},
		{
			name:      "single value",
			durations: []time.Duration{100 * time.Millisecond},
			expected:  100 * time.Millisecond,
		},
		{
			name:      "multiple values",
			durations: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond},
			expected:  20 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Average(tt.durations)
			if result != tt.expected {
				t.Errorf("Average(%v) = %v, want %v", tt.durations, result, tt.expected)
			}
		})
	}
}

func TestSum(t *testing.T) {
	tests := []struct {
		name     string
		values   []int
		expected int
	}{
		{
			name:     "empty",
			values:   []int{},
			expected: 0,
		},
		{
			name:     "single value",
			values:   []int{5},
			expected: 5,
		},
		{
			name:     "multiple values",
			values:   []int{1, 2, 3, 4, 5},
			expected: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Sum(tt.values)
			if result != tt.expected {
				t.Errorf("Sum(%v) = %d, want %d", tt.values, result, tt.expected)
			}
		})
	}
}

func TestDurationsToMs(t *testing.T) {
	durations := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		1500 * time.Millisecond,
	}

	result := DurationsToMs(durations)
	expected := []int64{100, 200, 1500}

	if len(result) != len(expected) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(expected))
	}

	for i, v := range result {
		if v != expected[i] {
			t.Errorf("DurationsToMs[%d] = %d, want %d", i, v, expected[i])
		}
	}
}
