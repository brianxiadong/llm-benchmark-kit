package soaktest

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "embed"
)

//go:embed templates/soak_report.html
var soakReportTemplate string

//go:embed assets/js/echarts.min.js
var echartsJSData []byte

// ReportData holds all data needed to render the HTML report.
type ReportData struct {
	Report    *SoakReport
	EChartsJS template.JS
	Generated string

	// Pre-serialized JSON for chart data
	SnapshotsJSON       template.JS
	MetricsTimelineJSON template.JS
	ErrorCountsJSON     template.JS
}

// GenerateHTMLReport generates an HTML report file.
func GenerateHTMLReport(report *SoakReport, outputDir string) error {
	tmpl, err := template.New("soak_report").Parse(soakReportTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	snapshotsJSON, _ := json.Marshal(report.Snapshots)
	metricsJSON, _ := json.Marshal(report.MetricsTimeline)
	errorCountsJSON, _ := json.Marshal(report.ErrorCounts)

	data := ReportData{
		Report:              report,
		EChartsJS:           template.JS(echartsJSData),
		Generated:           time.Now().Format("2006-01-02 15:04:05"),
		SnapshotsJSON:       template.JS(snapshotsJSON),
		MetricsTimelineJSON: template.JS(metricsJSON),
		ErrorCountsJSON:     template.JS(errorCountsJSON),
	}

	outPath := filepath.Join(outputDir, "soak_report.html")
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create HTML file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

// GenerateMarkdownReport generates a Markdown summary report.
func GenerateMarkdownReport(report *SoakReport, outputDir string) error {
	outPath := filepath.Join(outputDir, "soak_report.md")
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create markdown file: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "# LLM Soak Test Report\n\n")
	fmt.Fprintf(f, "## Overview\n\n")
	fmt.Fprintf(f, "| Metric | Value |\n")
	fmt.Fprintf(f, "|--------|-------|\n")
	fmt.Fprintf(f, "| Model | %s |\n", report.Model)
	fmt.Fprintf(f, "| URL | %s |\n", report.URL)
	fmt.Fprintf(f, "| Duration | %ds |\n", report.DurationSec)
	fmt.Fprintf(f, "| Concurrency | %d |\n", report.Concurrency)
	fmt.Fprintf(f, "| Window Interval | %ds |\n", report.WindowSec)
	fmt.Fprintf(f, "| Start Time | %s |\n", report.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "| End Time | %s |\n", report.EndTime.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "\n")

	fmt.Fprintf(f, "## Results Summary\n\n")
	fmt.Fprintf(f, "| Metric | Value |\n")
	fmt.Fprintf(f, "|--------|-------|\n")
	fmt.Fprintf(f, "| Total Requests | %d |\n", report.TotalRequests)
	fmt.Fprintf(f, "| Success | %d |\n", report.TotalSuccess)
	fmt.Fprintf(f, "| Failure | %d |\n", report.TotalFailure)
	fmt.Fprintf(f, "| Success Rate | %.2f%% |\n", report.SuccessRate*100)
	fmt.Fprintf(f, "| Overall RPS | %.2f |\n", report.OverallRPS)
	fmt.Fprintf(f, "| Avg TTFT | %.2f ms |\n", report.AvgTTFTMs)
	fmt.Fprintf(f, "| Avg Latency | %.2f ms |\n", report.AvgLatencyMs)
	fmt.Fprintf(f, "\n")

	if len(report.Snapshots) > 0 {
		fmt.Fprintf(f, "## Time Window Details\n\n")
		fmt.Fprintf(f, "| Window | Requests | Success%% | Avg TTFT(ms) | Avg Latency(ms) | P95 Latency(ms) | RPS |\n")
		fmt.Fprintf(f, "|--------|----------|----------|-------------|----------------|-----------------|-----|\n")
		for _, s := range report.Snapshots {
			fmt.Fprintf(f, "| #%d | %d | %.1f%% | %.0f | %.0f | %d | %.1f |\n",
				s.WindowIndex, s.TotalRequests, s.SuccessRate*100,
				s.AvgTTFTMs, s.AvgLatencyMs, s.P95LatencyMs, s.RPS)
		}
		fmt.Fprintf(f, "\n")
	}

	if len(report.ErrorCounts) > 0 {
		fmt.Fprintf(f, "## Error Summary\n\n")
		fmt.Fprintf(f, "| Error | Count |\n")
		fmt.Fprintf(f, "|-------|-------|\n")
		for errMsg, count := range report.ErrorCounts {
			fmt.Fprintf(f, "| %s | %d |\n", errMsg, count)
		}
		fmt.Fprintf(f, "\n")
	}

	return nil
}

// keep for potential future use
var _ = base64.StdEncoding

// RebuildReportFromDir reads soak_log.jsonl and snapshots.jsonl from the given
// directory and regenerates the full report (JSON + HTML + Markdown).
// This allows downloading raw logs from a remote server and generating
// reports locally.
func RebuildReportFromDir(inputDir, outputDir string) (*SoakReport, error) {
	// If outputDir is empty, output to the same directory
	if outputDir == "" {
		outputDir = inputDir
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output dir: %w", err)
	}

	// Try to load existing soak_report.json first (if it was partially written)
	var report *SoakReport
	reportPath := filepath.Join(inputDir, "soak_report.json")
	if data, err := os.ReadFile(reportPath); err == nil {
		report = &SoakReport{}
		if err := json.Unmarshal(data, report); err != nil {
			log.Printf("Warning: existing soak_report.json is invalid, will rebuild from logs: %v", err)
			report = nil
		}
	}

	// If no valid report JSON, rebuild from log files
	if report == nil {
		var err error
		report, err = rebuildFromLogs(inputDir)
		if err != nil {
			return nil, err
		}
	}

	// Filter out incomplete final snapshot (test termination artifacts)
	// When the test stops, in-flight requests get context deadline exceeded errors,
	// creating a final snapshot with 0% success rate and zero metrics.
	if len(report.Snapshots) > 1 {
		last := report.Snapshots[len(report.Snapshots)-1]
		if last.Success == 0 && last.Failure > 0 {
			log.Printf("Filtering out incomplete final snapshot (window %d): %d failures, 0 successes",
				last.WindowIndex, last.Failure)
			report.Snapshots = report.Snapshots[:len(report.Snapshots)-1]
			report.TotalRequests -= last.TotalRequests
			report.TotalFailure -= last.Failure
			if report.TotalRequests > 0 {
				report.SuccessRate = float64(report.TotalSuccess) / float64(report.TotalRequests)
			}
			// Update end time to the last valid snapshot's end time
			if len(report.Snapshots) > 0 {
				report.EndTime = report.Snapshots[len(report.Snapshots)-1].EndTime
				report.DurationSec = int(report.EndTime.Sub(report.StartTime).Seconds())
				if report.DurationSec > 0 {
					report.OverallRPS = float64(report.TotalRequests) / float64(report.DurationSec)
				}
			}
			// Remove the termination errors from ErrorCounts
			for errKey, cnt := range last.ErrorCounts {
				report.ErrorCounts[errKey] -= cnt
				if report.ErrorCounts[errKey] <= 0 {
					delete(report.ErrorCounts, errKey)
				}
			}
		}
	}

	// Write report JSON
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	outReportPath := filepath.Join(outputDir, "soak_report.json")
	if err := os.WriteFile(outReportPath, reportJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write report JSON: %w", err)
	}
	log.Printf("✅ JSON report:    %s", outReportPath)

	// Generate HTML report
	if err := GenerateHTMLReport(report, outputDir); err != nil {
		return nil, fmt.Errorf("failed to generate HTML report: %w", err)
	}
	log.Printf("✅ HTML report:    %s", filepath.Join(outputDir, "soak_report.html"))

	// Generate Markdown report
	if err := GenerateMarkdownReport(report, outputDir); err != nil {
		return nil, fmt.Errorf("failed to generate Markdown report: %w", err)
	}
	log.Printf("✅ Markdown report: %s", filepath.Join(outputDir, "soak_report.md"))

	return report, nil
}

// rebuildFromLogs reconstructs a SoakReport from soak_log.jsonl and snapshots.jsonl.
func rebuildFromLogs(dir string) (*SoakReport, error) {
	report := &SoakReport{
		ErrorCounts: make(map[string]int),
	}

	// Read request records from soak_log.jsonl
	logPath := filepath.Join(dir, "soak_log.jsonl")
	records, err := readJSONLFile[RequestRecord](logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read soak_log.jsonl: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("no request records found in %s", logPath)
	}

	log.Printf("Loaded %d request records from soak_log.jsonl", len(records))

	// Read snapshots from snapshots.jsonl (if exists)
	snapPath := filepath.Join(dir, "snapshots.jsonl")
	snapshots, err := readJSONLFile[WindowSnapshot](snapPath)
	if err != nil {
		log.Printf("Warning: snapshots.jsonl not found or invalid, will recompute from logs")
		snapshots = nil
	} else {
		log.Printf("Loaded %d window snapshots from snapshots.jsonl", len(snapshots))
	}

	// Determine time range
	var earliest, latest time.Time
	for i, rec := range records {
		if i == 0 || rec.Timestamp.Before(earliest) {
			earliest = rec.Timestamp
		}
		endTime := rec.Timestamp.Add(rec.Latency)
		if i == 0 || endTime.After(latest) {
			latest = endTime
		}
	}

	report.StartTime = earliest
	report.EndTime = latest
	durationSec := int(latest.Sub(earliest).Seconds())
	if durationSec < 1 {
		durationSec = 1
	}
	report.DurationSec = durationSec

	// Compute overall stats from records
	report.TotalRequests = len(records)
	for _, rec := range records {
		if rec.Success {
			report.TotalSuccess++
		} else {
			report.TotalFailure++
			if rec.Error != "" {
				errKey := rec.Error
				if len(errKey) > 100 {
					errKey = errKey[:100]
				}
				report.ErrorCounts[errKey]++
			}
		}
	}

	if report.TotalRequests > 0 {
		report.SuccessRate = float64(report.TotalSuccess) / float64(report.TotalRequests)
	}

	wallSec := report.EndTime.Sub(report.StartTime).Seconds()
	if wallSec > 0 {
		report.OverallRPS = float64(report.TotalRequests) / wallSec
	}

	// Use existing snapshots or recompute
	if len(snapshots) > 0 {
		report.Snapshots = snapshots
	} else {
		// Recompute snapshots: default 30s windows
		windowSec := 30
		report.WindowSec = windowSec
		windowDur := time.Duration(windowSec) * time.Second

		windowStart := earliest
		windowIdx := 0
		for windowStart.Before(latest) {
			windowEnd := windowStart.Add(windowDur)
			if windowEnd.After(latest) {
				windowEnd = latest.Add(time.Second)
			}

			var windowRecs []RequestRecord
			for _, rec := range records {
				if !rec.Timestamp.Before(windowStart) && rec.Timestamp.Before(windowEnd) {
					windowRecs = append(windowRecs, rec)
				}
			}

			if len(windowRecs) > 0 {
				snap := ComputeSnapshot(windowIdx, windowStart, windowEnd, windowRecs, nil)
				report.Snapshots = append(report.Snapshots, snap)
				windowIdx++
			}

			windowStart = windowEnd
		}
	}

	// Set window sec from snapshots if we loaded them
	if len(report.Snapshots) >= 2 && report.WindowSec == 0 {
		delta := report.Snapshots[1].StartTime.Sub(report.Snapshots[0].StartTime)
		report.WindowSec = int(delta.Seconds())
	}

	// Compute overall averages
	var totalTTFT, totalLatency float64
	var count int
	for _, s := range report.Snapshots {
		if s.Success > 0 {
			totalTTFT += s.AvgTTFTMs * float64(s.Success)
			totalLatency += s.AvgLatencyMs * float64(s.Success)
			count += s.Success
		}
	}
	if count > 0 {
		report.AvgTTFTMs = totalTTFT / float64(count)
		report.AvgLatencyMs = totalLatency / float64(count)
	}

	return report, nil
}

// readJSONLFile reads a JSONL file and decodes each line into type T.
func readJSONLFile[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			log.Printf("Warning: failed to parse line %d: %v", lineNum, err)
			continue
		}
		results = append(results, item)
	}
	if err := scanner.Err(); err != nil {
		return results, fmt.Errorf("scanner error: %w", err)
	}
	return results, nil
}
