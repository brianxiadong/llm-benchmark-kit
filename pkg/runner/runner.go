// Package runner implements the benchmark runner with worker pool.
package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/provider"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/result"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/workload"
)

// MaxSampleSize is the maximum size for raw data sampling.
const MaxSampleSize = 64 * 1024 // 64KB

// Runner executes the benchmark.
type Runner struct {
	cfg      *config.GlobalConfig
	provider provider.Provider
	loader   *workload.Loader
}

// New creates a new benchmark runner.
func New(cfg *config.GlobalConfig, p provider.Provider) *Runner {
	return &Runner{
		cfg:      cfg,
		provider: p,
		loader:   workload.NewLoader(),
	}
}

// Run executes the benchmark and returns the report.
func (r *Runner) Run() (*result.BenchmarkReport, error) {
	// Load workloads
	var workloads []workload.WorkloadInput
	var err error

	if r.cfg.WorkloadFile != "" {
		workloads, err = r.loader.LoadFromFile(r.cfg.WorkloadFile, r.cfg.MaxTokens)
		if err != nil {
			return nil, fmt.Errorf("failed to load workloads: %w", err)
		}
	} else {
		workloads = r.loader.GenerateDefault(r.cfg.TotalRequests+r.cfg.Warmup, r.cfg.MaxTokens)
	}

	totalNeeded := r.cfg.TotalRequests + r.cfg.Warmup
	if len(workloads) < totalNeeded {
		// Repeat workloads if not enough
		original := workloads
		for len(workloads) < totalNeeded {
			for _, w := range original {
				if len(workloads) >= totalNeeded {
					break
				}
				newWorkload := w
				newWorkload.ID = fmt.Sprintf("req-%d", len(workloads)+1)
				workloads = append(workloads, newWorkload)
			}
		}
	}

	// Run warmup
	if r.cfg.Warmup > 0 {
		fmt.Printf("Running %d warmup requests...\n", r.cfg.Warmup)
		warmupWorkloads := workloads[:r.cfg.Warmup]
		r.runBatch(warmupWorkloads, false)
		workloads = workloads[r.cfg.Warmup:]
	}

	// Run benchmark
	fmt.Printf("Running %d benchmark requests with %d concurrency...\n", r.cfg.TotalRequests, r.cfg.Concurrency)
	startTime := time.Now()
	results := r.runBatch(workloads[:r.cfg.TotalRequests], true)
	wallTime := time.Since(startTime)

	// Generate report
	report := r.generateReport(results, wallTime)

	// Write output files
	if err := r.writeOutput(results, report); err != nil {
		return nil, fmt.Errorf("failed to write output: %w", err)
	}

	return report, nil
}

func (r *Runner) runBatch(workloads []workload.WorkloadInput, collect bool) []result.RequestResult {
	jobs := make(chan workload.WorkloadInput, len(workloads))
	results := make(chan result.RequestResult, len(workloads))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < r.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.worker(jobs, results)
		}()
	}

	// Setup RPS limiter if enabled
	var ticker *time.Ticker
	if r.cfg.RPS > 0 {
		interval := time.Duration(float64(time.Second) / r.cfg.RPS)
		ticker = time.NewTicker(interval)
		defer ticker.Stop()
	}

	// Send jobs
	go func() {
		for _, w := range workloads {
			if ticker != nil {
				<-ticker.C
			}
			jobs <- w
		}
		close(jobs)
	}()

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var collected []result.RequestResult
	for res := range results {
		if collect {
			collected = append(collected, res)
		}
	}

	return collected
}

func (r *Runner) worker(jobs <-chan workload.WorkloadInput, results chan<- result.RequestResult) {
	for job := range jobs {
		res := r.executeRequest(job)
		results <- res
	}
}

func (r *Runner) executeRequest(input workload.WorkloadInput) result.RequestResult {
	res := result.RequestResult{
		ID:        input.ID,
		StartTime: time.Now(),
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.cfg.TimeoutSec)*time.Second)
	defer cancel()

	// Execute streaming request
	events, err := r.provider.StreamChat(ctx, r.cfg, input)
	if err != nil {
		res.Status = result.StatusHTTPError
		res.Err = err.Error()
		res.EndTime = time.Now()
		res.Latency = res.EndTime.Sub(res.StartTime)
		return res
	}

	// Process events
	var totalContent string
	gotFirstContent := false
	var usage *provider.TokenUsage

	for event := range events {
		switch event.Type {
		case provider.EventContent:
			if !gotFirstContent {
				res.FirstContentTime = time.Now()
				res.TTFT = res.FirstContentTime.Sub(res.StartTime)
				res.FirstContentRaw = truncateString(event.Raw, MaxSampleSize)
				gotFirstContent = true
			}
			totalContent += event.Text

		case provider.EventUsage:
			usage = event.Usage

		case provider.EventEnd:
			res.FinalFrameRaw = truncateString(event.Raw, MaxSampleSize)

		case provider.EventError:
			res.Status = result.StatusParseError
			res.Err = event.Err.Error()
		}
	}

	res.EndTime = time.Now()
	res.Latency = res.EndTime.Sub(res.StartTime)

	if gotFirstContent {
		res.Decode = res.EndTime.Sub(res.FirstContentTime)
	}

	res.OutChars = len(totalContent)
	if usage != nil {
		res.OutTokens = usage.CompletionTokens
	}

	if res.Status == "" {
		if ctx.Err() == context.DeadlineExceeded {
			res.Status = result.StatusTimeout
			res.Err = "request timeout"
		} else if gotFirstContent {
			res.Status = result.StatusOK
		} else {
			res.Status = result.StatusParseError
			res.Err = "no content received"
		}
	}

	return res
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}
