// Package stats provides statistical calculation utilities.
package stats

import (
	"sort"
	"time"
)

// Percentile calculates the p-th percentile of the given durations.
// p should be between 0 and 100.
func Percentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	// Sort the durations
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Calculate the index
	index := (p / 100.0) * float64(len(sorted)-1)
	lower := int(index)
	upper := lower + 1

	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	// Linear interpolation
	weight := index - float64(lower)
	return time.Duration(float64(sorted[lower])*(1-weight) + float64(sorted[upper])*weight)
}

// PercentileMs calculates the p-th percentile and returns milliseconds.
func PercentileMs(durations []time.Duration, p float64) int64 {
	return Percentile(durations, p).Milliseconds()
}

// Average calculates the average of the given durations.
func Average(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	return sum / time.Duration(len(durations))
}

// AverageMs calculates the average and returns milliseconds as float64.
func AverageMs(durations []time.Duration) float64 {
	return float64(Average(durations).Microseconds()) / 1000.0
}

// Sum calculates the sum of the given integers.
func Sum(values []int) int {
	var sum int
	for _, v := range values {
		sum += v
	}
	return sum
}

// DurationsToMs converts a slice of durations to milliseconds.
func DurationsToMs(durations []time.Duration) []int64 {
	result := make([]int64, len(durations))
	for i, d := range durations {
		result[i] = d.Milliseconds()
	}
	return result
}
