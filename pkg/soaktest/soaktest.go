package soaktest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/provider"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/workload"
)

// SoakConfig holds configuration for the soak test.
type SoakConfig struct {
	DurationSec     int // Total test duration in seconds
	Concurrency     int // Number of concurrent workers
	WindowSec       int // Time window interval for snapshots (seconds)
	MetricsInterval int // System metrics collection interval (seconds)

	// Mixed workload: split workers into short and long request groups
	LongConcurrency int // Number of workers dedicated to long requests (0 = all short)
	LongMaxTokens   int // Max tokens for long requests (e.g., 2048)
}

// DefaultSoakConfig returns default soak test configuration.
func DefaultSoakConfig() *SoakConfig {
	return &SoakConfig{
		DurationSec:     300, // 5 minutes
		Concurrency:     5,
		WindowSec:       30, // snapshot every 30s
		MetricsInterval: 10, // collect system metrics every 10s
		LongConcurrency: 0,  // all short by default
		LongMaxTokens:   2048,
	}
}

// SoakReport holds the final soak test report.
type SoakReport struct {
	// Metadata
	Model       string    `json:"model"`
	URL         string    `json:"url"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	DurationSec int       `json:"duration_sec"`
	Concurrency int       `json:"concurrency"`
	WindowSec   int       `json:"window_sec"`

	// Overall stats
	TotalRequests int     `json:"total_requests"`
	TotalSuccess  int     `json:"total_success"`
	TotalFailure  int     `json:"total_failure"`
	SuccessRate   float64 `json:"success_rate"`
	OverallRPS    float64 `json:"overall_rps"`

	// Overall latency
	AvgTTFTMs    float64 `json:"avg_ttft_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`

	// Time-series data
	Snapshots       []WindowSnapshot `json:"snapshots"`
	MetricsTimeline []SystemMetrics  `json:"metrics_timeline"`

	// Error summary
	ErrorCounts map[string]int `json:"error_counts,omitempty"`
}

// Runner executes the soak test.
type Runner struct {
	cfg       *config.GlobalConfig
	soakCfg   *SoakConfig
	provider  provider.Provider
	loader    *workload.Loader
	outputDir string
}

// NewRunner creates a new soak test runner.
func NewRunner(cfg *config.GlobalConfig, soakCfg *SoakConfig, p provider.Provider, outputDir string) *Runner {
	return &Runner{
		cfg:       cfg,
		soakCfg:   soakCfg,
		provider:  p,
		loader:    workload.NewLoader(),
		outputDir: outputDir,
	}
}

// Run executes the soak test.
func (r *Runner) Run() (*SoakReport, error) {
	// Create output directory
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output dir: %w", err)
	}

	// Load workloads
	var shortWorkloads, longWorkloads []workload.WorkloadInput
	var err error
	if r.cfg.WorkloadFile != "" {
		shortWorkloads, err = r.loader.LoadFromFile(r.cfg.WorkloadFile, r.cfg.MaxTokens)
		if err != nil {
			return nil, fmt.Errorf("failed to load workloads: %w", err)
		}
		// For file-based workloads, use same set for long (they control their own tokens)
		longWorkloads = shortWorkloads
	} else {
		shortWorkloads = r.loader.GenerateDefault(100, r.cfg.MaxTokens)
		if r.soakCfg.LongConcurrency > 0 {
			longWorkloads = r.loader.GenerateLong(20, r.soakCfg.LongMaxTokens)
		}
	}

	// Determine worker split
	longWorkerCount := r.soakCfg.LongConcurrency
	if longWorkerCount > r.soakCfg.Concurrency {
		longWorkerCount = r.soakCfg.Concurrency
	}
	shortWorkerCount := r.soakCfg.Concurrency - longWorkerCount
	if longWorkerCount > 0 {
		log.Printf("[Soak] Workload mix: %d short workers (max_tokens=%d) + %d long workers (max_tokens=%d)",
			shortWorkerCount, r.cfg.MaxTokens, longWorkerCount, r.soakCfg.LongMaxTokens)
	}

	// Open log file for append
	logFile, err := os.Create(filepath.Join(r.outputDir, "soak_log.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFile.Close()

	duration := time.Duration(r.soakCfg.DurationSec) * time.Second
	windowDur := time.Duration(r.soakCfg.WindowSec) * time.Second
	metricsDur := time.Duration(r.soakCfg.MetricsInterval) * time.Second

	report := &SoakReport{
		Model:       r.cfg.ModelName,
		URL:         r.cfg.URL,
		StartTime:   time.Now(),
		DurationSec: r.soakCfg.DurationSec,
		Concurrency: r.soakCfg.Concurrency,
		WindowSec:   r.soakCfg.WindowSec,
		ErrorCounts: make(map[string]int),
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	// Channel to collect request records
	recordCh := make(chan RequestRecord, 1000)

	// All records for window aggregation
	var allRecords []RequestRecord
	var allRecordsMu sync.Mutex

	// Current window records
	var windowRecords []RequestRecord
	var windowMu sync.Mutex

	// Metrics timeline
	var metricsTimeline []SystemMetrics
	var metricsMu sync.Mutex

	// Request counter
	var reqCounter int64

	// Start workers
	var wg sync.WaitGroup

	// Short request workers
	for i := 0; i < shortWorkerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				reqID := atomic.AddInt64(&reqCounter, 1)
				wl := shortWorkloads[int(reqID-1)%len(shortWorkloads)]
				wl.ID = fmt.Sprintf("soak-short-%d", reqID)

				record := r.executeRequest(ctx, wl)
				record.WorkloadType = "short"
				select {
				case recordCh <- record:
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Long request workers
	for i := 0; i < longWorkerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				reqID := atomic.AddInt64(&reqCounter, 1)
				wl := longWorkloads[int(reqID-1)%len(longWorkloads)]
				wl.ID = fmt.Sprintf("soak-long-%d", reqID)

				record := r.executeRequest(ctx, wl)
				record.WorkloadType = "long"
				select {
				case recordCh <- record:
				case <-ctx.Done():
					return
				}
			}
		}(shortWorkerCount + i)
	}

	// Collector goroutine: gather records and write to log
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		encoder := json.NewEncoder(logFile)
		for rec := range recordCh {
			_ = encoder.Encode(rec)

			allRecordsMu.Lock()
			allRecords = append(allRecords, rec)
			allRecordsMu.Unlock()

			windowMu.Lock()
			windowRecords = append(windowRecords, rec)
			windowMu.Unlock()
		}
	}()

	// System metrics collector goroutine
	metricsCtx, metricsCancel := context.WithCancel(context.Background())
	var metricsWg sync.WaitGroup
	metricsWg.Add(1)
	go func() {
		defer metricsWg.Done()
		ticker := time.NewTicker(metricsDur)
		defer ticker.Stop()
		for {
			select {
			case <-metricsCtx.Done():
				return
			case <-ticker.C:
				m := CollectSystemMetrics()
				metricsMu.Lock()
				metricsTimeline = append(metricsTimeline, m)
				metricsMu.Unlock()
				log.Printf("[Soak] System: %s", FormatMetrics(m))
			}
		}
	}()

	// Window snapshot goroutine
	snapshotCtx, snapshotCancel := context.WithCancel(context.Background())
	var snapshotWg sync.WaitGroup
	snapshotWg.Add(1)
	go func() {
		defer snapshotWg.Done()
		windowTicker := time.NewTicker(windowDur)
		defer windowTicker.Stop()
		windowStart := time.Now()
		windowIdx := 0
		for {
			select {
			case <-snapshotCtx.Done():
				return
			case <-windowTicker.C:
				windowEnd := time.Now()

				// Swap window records
				windowMu.Lock()
				recs := windowRecords
				windowRecords = nil
				windowMu.Unlock()

				// Get latest system metrics
				metricsMu.Lock()
				var latestMetrics *SystemMetrics
				if len(metricsTimeline) > 0 {
					m := metricsTimeline[len(metricsTimeline)-1]
					latestMetrics = &m
				}
				metricsMu.Unlock()

				snap := ComputeSnapshot(windowIdx, windowStart, windowEnd, recs, latestMetrics)
				report.Snapshots = append(report.Snapshots, snap)

				// Print window summary
				log.Printf("[Soak] Window #%d | Requests: %d | Success: %.1f%% | AvgLatency: %.0fms | AvgTTFT: %.0fms | RPS: %.1f",
					windowIdx, snap.TotalRequests, snap.SuccessRate*100,
					snap.AvgLatencyMs, snap.AvgTTFTMs, snap.RPS)

				// Print error breakdown if any
				if len(snap.ErrorCounts) > 0 {
					log.Printf("[Soak] Window #%d errors: %s", windowIdx, formatErrorCounts(snap.ErrorCounts))
				}

				// Write snapshot to file
				snapFile := filepath.Join(r.outputDir, "snapshots.jsonl")
				if f, err := os.OpenFile(snapFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
					enc := json.NewEncoder(f)
					_ = enc.Encode(snap)
					f.Close()
				}

				windowStart = windowEnd
				windowIdx++
			}
		}
	}()

	// Wait for all workers to finish
	wg.Wait()
	close(recordCh)
	<-collectorDone

	// Stop metrics and snapshot collectors
	metricsCancel()
	snapshotCancel()
	metricsWg.Wait()
	snapshotWg.Wait()

	// Process final window remainder
	windowMu.Lock()
	remainingRecs := windowRecords
	windowMu.Unlock()
	if len(remainingRecs) > 0 {
		metricsMu.Lock()
		var latestMetrics *SystemMetrics
		if len(metricsTimeline) > 0 {
			m := metricsTimeline[len(metricsTimeline)-1]
			latestMetrics = &m
		}
		metricsMu.Unlock()

		snap := ComputeSnapshot(len(report.Snapshots), report.StartTime, time.Now(), remainingRecs, latestMetrics)
		report.Snapshots = append(report.Snapshots, snap)
	}

	// Finalize report
	report.EndTime = time.Now()

	allRecordsMu.Lock()
	report.TotalRequests = len(allRecords)
	for _, rec := range allRecords {
		if rec.Success {
			report.TotalSuccess++
		} else {
			report.TotalFailure++
			if rec.Error != "" {
				errKey := classifyError(rec.Error)
				report.ErrorCounts[errKey]++
			}
		}
	}
	allRecordsMu.Unlock()

	if report.TotalRequests > 0 {
		report.SuccessRate = float64(report.TotalSuccess) / float64(report.TotalRequests)
	}

	// Print final error summary
	if len(report.ErrorCounts) > 0 {
		log.Printf("[Soak] === Error Summary ===")
		log.Printf("[Soak] Total: %d success, %d failed (%.1f%% success rate)",
			report.TotalSuccess, report.TotalFailure, report.SuccessRate*100)
		// Sort errors by count descending
		type errEntry struct {
			Key   string
			Count int
		}
		var entries []errEntry
		for k, v := range report.ErrorCounts {
			entries = append(entries, errEntry{k, v})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Count > entries[j].Count })
		for _, e := range entries {
			pct := float64(e.Count) / float64(report.TotalFailure) * 100
			log.Printf("[Soak]   %s: %d (%.1f%% of failures)", e.Key, e.Count, pct)
		}
	}

	wallSec := report.EndTime.Sub(report.StartTime).Seconds()
	if wallSec > 0 {
		report.OverallRPS = float64(report.TotalRequests) / wallSec
	}

	// Compute overall averages from snapshots
	if len(report.Snapshots) > 0 {
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
	}

	metricsMu.Lock()
	report.MetricsTimeline = metricsTimeline
	metricsMu.Unlock()

	// Write final report JSON
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(r.outputDir, "soak_report.json"), reportJSON, 0644); err != nil {
		log.Printf("Warning: failed to write report JSON: %v", err)
	}

	// Generate HTML report
	if err := GenerateHTMLReport(report, r.outputDir); err != nil {
		log.Printf("Warning: failed to generate HTML report: %v", err)
	}

	// Generate Markdown report
	if err := GenerateMarkdownReport(report, r.outputDir); err != nil {
		log.Printf("Warning: failed to generate Markdown report: %v", err)
	}

	return report, nil
}

// formatErrorCounts formats error counts into a concise log string.
func formatErrorCounts(counts map[string]int) string {
	type entry struct {
		Key   string
		Count int
	}
	var entries []entry
	for k, v := range counts {
		entries = append(entries, entry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Count > entries[j].Count })

	var parts []string
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf("%s=%d", e.Key, e.Count))
	}
	return strings.Join(parts, ", ")
}

// executeRequest runs a single request and returns a record.
func (r *Runner) executeRequest(ctx context.Context, input workload.WorkloadInput) RequestRecord {
	rec := RequestRecord{
		Timestamp: time.Now(),
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.TimeoutSec)*time.Second)
	defer cancel()

	events, err := r.provider.StreamChat(reqCtx, r.cfg, input)
	if err != nil {
		rec.Error = err.Error()
		return rec
	}

	startTime := time.Now()
	gotFirst := false
	var usage *provider.TokenUsage

	gotAny := false // tracks if we got any output (content or reasoning)
	for event := range events {
		switch event.Type {
		case provider.EventContent:
			if !gotFirst {
				rec.TTFT = time.Since(startTime)
				gotFirst = true
			}
			gotAny = true
			rec.OutChars += len(event.Text)

		case provider.EventReasoning:
			// Reasoning/thinking tokens count as activity (model is working)
			if !gotFirst {
				rec.TTFT = time.Since(startTime)
				gotFirst = true
			}
			gotAny = true

		case provider.EventUsage:
			usage = event.Usage

		case provider.EventError:
			rec.Error = event.Err.Error()
		}
	}

	rec.Latency = time.Since(startTime)

	if usage != nil {
		rec.OutTokens = usage.CompletionTokens
	}

	if rec.Error == "" && gotAny {
		rec.Success = true
	} else if rec.Error == "" && !gotAny {
		rec.Error = "no_content_received"
	}

	return rec
}
