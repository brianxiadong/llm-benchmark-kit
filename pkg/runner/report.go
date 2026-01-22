// Package runner implements report generation.
package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/result"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/stats"
)

func (r *Runner) generateReport(results []result.RequestResult, wallTime time.Duration) *result.BenchmarkReport {
	report := &result.BenchmarkReport{
		Provider:      r.provider.Name(),
		Model:         r.cfg.ModelName,
		StartedAt:     time.Now().Format(time.RFC3339),
		WallTimeMs:    wallTime.Milliseconds(),
		TotalRequests: len(results),
		TokenMode:     r.cfg.TokenMode,
	}

	// Separate successful and failed requests
	var successResults []result.RequestResult
	var ttfts []time.Duration
	var latencies []time.Duration
	var totalTokens int
	var totalChars int
	errorCounts := make(map[string]int)

	for _, res := range results {
		if res.IsSuccess() {
			report.Success++
			successResults = append(successResults, res)
			ttfts = append(ttfts, res.TTFT)
			latencies = append(latencies, res.Latency)
			totalTokens += res.OutTokens
			totalChars += res.OutChars

			// Capture first sample
			if report.FirstContentRaw == "" && res.FirstContentRaw != "" {
				report.FirstContentRaw = res.FirstContentRaw
			}
			if len(report.MiddleFramesRaw) == 0 && len(res.MiddleFramesRaw) > 0 {
				report.MiddleFramesRaw = res.MiddleFramesRaw
			}
			if report.FinalFrameRaw == "" && res.FinalFrameRaw != "" {
				report.FinalFrameRaw = res.FinalFrameRaw
			}
		} else {
			report.Failure++
			errKey := string(res.Status)
			if res.Err != "" {
				errKey = fmt.Sprintf("%s: %s", res.Status, res.Err)
			}
			errorCounts[errKey]++
		}
	}

	// Calculate success rate
	if report.TotalRequests > 0 {
		report.SuccessRate = float64(report.Success) / float64(report.TotalRequests)
	}

	// Calculate statistics for successful requests
	if len(successResults) > 0 {
		// TTFT statistics
		report.AvgTTFTMs = stats.AverageMs(ttfts)
		report.P50TTFTMs = stats.PercentileMs(ttfts, 50)
		report.P95TTFTMs = stats.PercentileMs(ttfts, 95)
		report.P99TTFTMs = stats.PercentileMs(ttfts, 99)

		// Latency statistics
		report.AvgLatencyMs = stats.AverageMs(latencies)
		report.P50LatencyMs = stats.PercentileMs(latencies, 50)
		report.P95LatencyMs = stats.PercentileMs(latencies, 95)
		report.P99LatencyMs = stats.PercentileMs(latencies, 99)

		// Distributions for visualization
		report.TTFTDistribution = stats.DurationsToMs(ttfts)
		report.LatencyDistribution = stats.DurationsToMs(latencies)
	}

	// Calculate throughput
	if wallTime > 0 {
		report.RPS = float64(report.Success) / wallTime.Seconds()

		switch r.cfg.TokenMode {
		case "usage":
			if totalTokens > 0 {
				report.TokenThroughput = float64(totalTokens) / wallTime.Seconds()
			} else if totalChars > 0 {
				// Fallback to chars when API doesn't return usage
				report.TokenMode = "chars"
				report.TokenThroughput = float64(totalChars) / wallTime.Seconds()
			}
		case "chars":
			report.TokenThroughput = float64(totalChars) / wallTime.Seconds()
		}
	}

	// Error breakdown (top N)
	report.ErrorsTopN = r.topNErrors(errorCounts, 10)

	return report
}

func (r *Runner) topNErrors(errorCounts map[string]int, n int) []result.ErrorStat {
	var errors []result.ErrorStat
	for key, count := range errorCounts {
		errors = append(errors, result.ErrorStat{Key: key, Count: count})
	}

	sort.Slice(errors, func(i, j int) bool {
		return errors[i].Count > errors[j].Count
	})

	if len(errors) > n {
		errors = errors[:n]
	}
	return errors
}

func (r *Runner) writeOutput(results []result.RequestResult, report *result.BenchmarkReport) error {
	// Create output directory
	if err := os.MkdirAll(r.cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write results.jsonl
	resultsPath := filepath.Join(r.cfg.OutputDir, "results.jsonl")
	f, err := os.Create(resultsPath)
	if err != nil {
		return fmt.Errorf("failed to create results file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for _, res := range results {
		// Convert to output format
		output := map[string]interface{}{
			"request_id":       res.ID,
			"status":           res.Status,
			"ttft_ms":          res.TTFT.Milliseconds(),
			"latency_ms":       res.Latency.Milliseconds(),
			"decode_ms":        res.Decode.Milliseconds(),
			"out_tokens":       res.OutTokens,
			"out_chars":        res.OutChars,
			"start_ts":         res.StartTime.Format(time.RFC3339Nano),
			"first_content_ts": res.FirstContentTime.Format(time.RFC3339Nano),
			"end_ts":           res.EndTime.Format(time.RFC3339Nano),
			"provider":         r.provider.Name(),
		}
		if res.Err != "" {
			output["err"] = res.Err
		}
		if err := encoder.Encode(output); err != nil {
			return fmt.Errorf("failed to write result: %w", err)
		}
	}
	fmt.Printf("  - Results: %s\n", resultsPath)

	// Write summary.json
	summaryPath := filepath.Join(r.cfg.OutputDir, "summary.json")
	summaryData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal summary: %w", err)
	}
	if err := os.WriteFile(summaryPath, summaryData, 0644); err != nil {
		return fmt.Errorf("failed to write summary: %w", err)
	}
	fmt.Printf("  - Summary: %s\n", summaryPath)

	// Write report.html
	reportPath := filepath.Join(r.cfg.OutputDir, "report.html")
	if err := r.writeHTMLReport(report, reportPath); err != nil {
		return fmt.Errorf("failed to write HTML report: %w", err)
	}
	fmt.Printf("  - Report:  %s\n", reportPath)

	return nil
}
