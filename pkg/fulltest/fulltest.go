// Package fulltest provides a complete test runner that executes
// performance benchmark, function call test, and meeting summary tests.
package fulltest

import (
	"bytes"
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/provider"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/result"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/runner"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/summarizer"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/workload"
)

//go:embed templates/fulltest_report.html
var fullTestReportTemplate string

//go:embed assets/js/echarts.min.js
var echartsJS []byte

//go:embed assets/fonts/JetBrainsMono-Regular.woff2
var jetBrainsMonoFont []byte

//go:embed assets/fonts/PlusJakartaSans-Variable.woff2
var plusJakartaSansFont []byte

// TestResult holds result for a single test request.
type TestResult struct {
	Name      string  `json:"name"`
	Success   bool    `json:"success"`
	LatencyMs float64 `json:"latency_ms"`
	Tokens    int     `json:"tokens"`
	Error     string  `json:"error,omitempty"`
}

// PhaseResult holds results for a test phase.
type PhaseResult struct {
	PhaseName    string       `json:"phase_name"`
	Success      int          `json:"success"`
	Failure      int          `json:"failure"`
	AvgLatencyMs float64      `json:"avg_latency_ms"`
	TotalTokens  int          `json:"total_tokens"`
	Throughput   float64      `json:"throughput"`
	Results      []TestResult `json:"results"`
}

// FunctionCallResult holds function call test results.
type FunctionCallResult struct {
	Supported       bool    `json:"supported"`
	CorrectFunction bool    `json:"correct_function"`
	CorrectArgs     bool    `json:"correct_args"`
	LatencyMs       float64 `json:"latency_ms"`
	FunctionName    string  `json:"function_name"`
	Arguments       string  `json:"arguments"`
	Error           string  `json:"error,omitempty"`
}

// LongContextTestResult holds a single long context test result.
type LongContextTestResult struct {
	ContextLength int     `json:"context_length"` // Input context length in chars
	InputTokens   int     `json:"input_tokens"`   // Estimated input tokens
	OutputTokens  int     `json:"output_tokens"`  // Output tokens
	TTFTMs        float64 `json:"ttft_ms"`        // Time to first token
	LatencyMs     float64 `json:"latency_ms"`     // Total latency
	Throughput    float64 `json:"throughput"`     // Output tokens per second
	Success       bool    `json:"success"`
	Error         string  `json:"error,omitempty"`
}

// LongContextResult holds all long context test results.
type LongContextResult struct {
	Results       []LongContextTestResult `json:"results"`
	MaxSupported  int                     `json:"max_supported"` // Maximum supported context length
	AvgTTFTMs     float64                 `json:"avg_ttft_ms"`
	AvgLatencyMs  float64                 `json:"avg_latency_ms"`
	AvgThroughput float64                 `json:"avg_throughput"`
}

// LongContextConcurrentLevelResult holds results for one context length at one concurrency level.
type LongContextConcurrentLevelResult struct {
	ContextLength int     `json:"context_length"`
	Concurrency   int     `json:"concurrency"`
	TotalRequests int     `json:"total_requests"`
	SuccessCount  int     `json:"success_count"`
	AvgTTFTMs     float64 `json:"avg_ttft_ms"`
	P95TTFTMs     float64 `json:"p95_ttft_ms"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	Throughput    float64 `json:"throughput"` // tokens/s sum
	RPS           float64 `json:"rps"`
	WallTimeMs    float64 `json:"wall_time_ms"`
}

// LongContextConcurrentResult holds the full concurrent long context test results.
type LongContextConcurrentResult struct {
	Levels []LongContextConcurrentLevelResult `json:"levels"`
}

// EnvironmentInfo holds system environment information.
type EnvironmentInfo struct {
	Hostname    string            `json:"hostname"`
	OS          string            `json:"os"`
	Arch        string            `json:"arch"`
	Kernel      string            `json:"kernel"`
	CPUModel    string            `json:"cpu_model"`
	CPUCores    int               `json:"cpu_cores"`
	CPUThreads  int               `json:"cpu_threads"`
	TotalMemory string            `json:"total_memory"`
	GoVersion   string            `json:"go_version"`
	ToolVersion string            `json:"tool_version"`
	CollectTime string            `json:"collect_time"`
	GPUInfo     string            `json:"gpu_info,omitempty"`
	ExtraInfo   map[string]string `json:"extra_info,omitempty"`
}

// ConcurrencyLevelResult holds results for a single concurrency level.
type ConcurrencyLevelResult struct {
	Concurrency   int     `json:"concurrency"`
	TotalRequests int     `json:"total_requests"`
	SuccessCount  int     `json:"success_count"`
	FailureCount  int     `json:"failure_count"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	MinLatencyMs  float64 `json:"min_latency_ms"`
	MaxLatencyMs  float64 `json:"max_latency_ms"`
	AvgTTFTMs     float64 `json:"avg_ttft_ms"`
	Throughput    float64 `json:"throughput"` // tokens/s
	RPS           float64 `json:"rps"`        // requests/s
	TotalTokens   int     `json:"total_tokens"`
	WallTimeMs    float64 `json:"wall_time_ms"`
}

// GraduatedConcurrencyResult holds results for all concurrency levels.
type GraduatedConcurrencyResult struct {
	Levels           []ConcurrencyLevelResult `json:"levels"`
	RequestsPerLevel int                      `json:"requests_per_level"`
}

// FullTestReport contains the combined results from all test phases.
type FullTestReport struct {
	ModelName     string        `json:"model_name"`
	APIURL        string        `json:"api_url"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	TotalDuration time.Duration `json:"total_duration"`

	// Environment Info
	Environment *EnvironmentInfo `json:"environment,omitempty"`

	// Phase 1: Performance
	FirstCallResults  *PhaseResult            `json:"first_call_results"`
	ConcurrentResults *PhaseResult            `json:"concurrent_results"`
	MultiTurnResults  *PhaseResult            `json:"multi_turn_results"`
	BenchmarkReport   *result.BenchmarkReport `json:"benchmark_report,omitempty"`

	// Phase 1.5: Graduated Concurrency Test
	GraduatedConcurrency *GraduatedConcurrencyResult `json:"graduated_concurrency,omitempty"`

	// Phase 2: Function Call
	FunctionCallResult *FunctionCallResult `json:"function_call_result,omitempty"`

	// Phase 3: Long Context Test
	LongContextResult *LongContextResult `json:"long_context_result,omitempty"`

	// Phase 3.5: Long Context Concurrent Test
	LongContextConcurrentResult *LongContextConcurrentResult `json:"long_context_concurrent_result,omitempty"`

	// Phase 4: Summary
	SummaryMetrics *summarizer.SummaryMetrics `json:"summary_metrics,omitempty"`
	SummaryContent string                     `json:"summary_content,omitempty"`

	// Output directories
	BenchmarkOutputDir string `json:"benchmark_output_dir"`
	SummaryOutputDir   string `json:"summary_output_dir"`
}

// Runner executes the full test suite.
type Runner struct {
	cfg            *config.GlobalConfig
	transcriptFile string
	outputDir      string
	p              provider.Provider
	httpClient     *http.Client
	logFile        *os.File
}

// NewRunner creates a new full test runner.
func NewRunner(cfg *config.GlobalConfig, p provider.Provider, transcriptFile, outputDir string) *Runner {
	// Create HTTP client for function call test
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureTLS},
	}

	return &Runner{
		cfg:            cfg,
		p:              p,
		transcriptFile: transcriptFile,
		outputDir:      outputDir,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
		},
	}
}

// Run executes the full test suite and returns the combined report.
func (r *Runner) Run() (*FullTestReport, error) {
	report := &FullTestReport{
		ModelName: r.cfg.ModelName,
		APIURL:    r.cfg.URL,
		StartTime: time.Now(),
	}

	// Create output directory
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create log file for request/response tracking
	logPath := filepath.Join(r.outputDir, "request_response.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}
	r.logFile = logFile
	defer func() {
		if r.logFile != nil {
			r.logFile.Close()
		}
	}()
	r.writeLog("=" + strings.Repeat("=", 79))
	r.writeLog("LLM Benchmark Kit - Request/Response Log")
	r.writeLog("Model: %s", r.cfg.ModelName)
	r.writeLog("URL: %s", r.cfg.URL)
	r.writeLog("Time: %s", time.Now().Format("2006-01-02 15:04:05"))
	r.writeLog("=" + strings.Repeat("=", 79))

	r.printHeader()

	// ===== Collect Environment Info =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🖥️  Collecting Environment Info")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	report.Environment = r.collectEnvironmentInfo()
	r.printEnvironmentInfo(report.Environment)
	fmt.Println()

	// ===== Phase 1: Performance Benchmark =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 Phase 1: Performance Benchmark")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	benchmarkDir := filepath.Join(r.outputDir, "benchmark")
	if err := os.MkdirAll(benchmarkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create benchmark directory: %w", err)
	}

	// Set appropriate max_tokens for full-test (balanced for complete answers)
	// For thinking models that use reasoning tokens, 512 may be too small,
	// but performance tests focus on throughput measurement, so this is acceptable.
	originalMaxTokens := r.cfg.MaxTokens
	if r.cfg.MaxTokens > 1024 || r.cfg.MaxTokens == 0 {
		r.cfg.MaxTokens = 1024
		fmt.Printf("📝 Note: Set max_tokens to %d for full-test\n\n", r.cfg.MaxTokens)
	}

	// 1.1 First Call Test
	fmt.Println("📌 1.1 First Call Test (冷启动测试)")
	report.FirstCallResults = r.runFirstCallTest(3)
	r.printPhaseResults(report.FirstCallResults)

	// 1.2 Concurrent Test
	fmt.Println("📌 1.2 Concurrent Test (并发测试, 2并发)")
	report.ConcurrentResults = r.runConcurrentTest(2, 2)
	r.printPhaseResults(report.ConcurrentResults)

	// 1.3 Multi-turn Test
	fmt.Println("📌 1.3 Multi-turn Test (多轮对话)")
	report.MultiTurnResults = r.runMultiTurnTest(5)
	r.printPhaseResults(report.MultiTurnResults)

	// Also run the standard benchmark for detailed stats
	benchCfg := *r.cfg
	benchCfg.OutputDir = benchmarkDir
	benchCfg.Concurrency = 3
	benchCfg.TotalRequests = 10
	benchCfg.Warmup = 0
	benchRunner := runner.New(&benchCfg, r.p)
	benchReport, err := benchRunner.Run()
	if err != nil {
		fmt.Printf("⚠️  Standard benchmark failed: %v\n", err)
	} else {
		report.BenchmarkReport = benchReport
		report.BenchmarkOutputDir = benchmarkDir
	}

	// Restore original max_tokens
	r.cfg.MaxTokens = originalMaxTokens

	fmt.Println("✅ Phase 1 Complete!")
	fmt.Println()

	// ===== Phase 1.5: Graduated Concurrency Test =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📈 Phase 1.5: Graduated Concurrency Test (逐级并发测试)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	report.GraduatedConcurrency = r.runGraduatedConcurrencyTest()

	fmt.Println("✅ Phase 1.5 Complete!")
	fmt.Println()

	// ===== Phase 2: Function Call Test =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🔧 Phase 2: Function Call Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	report.FunctionCallResult = r.runFunctionCallTest()
	r.printFunctionCallResult(report.FunctionCallResult)

	fmt.Println("✅ Phase 2 Complete!")
	fmt.Println()

	// ===== Phase 3: Long Context Test =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📏 Phase 3: Long Context Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	report.LongContextResult = r.runLongContextTest()
	r.printLongContextResult(report.LongContextResult)

	fmt.Println("✅ Phase 3 Complete!")
	fmt.Println()

	// ===== Phase 3.5: Long Context Concurrent Test =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📏 Phase 3.5: Long Context Concurrent Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Testing concurrent long context requests with varied prompts (defeats prefix caching)...")
	fmt.Println()

	report.LongContextConcurrentResult = r.runLongContextConcurrentTest()
	r.printLongContextConcurrentResult(report.LongContextConcurrentResult)

	fmt.Println("✅ Phase 3.5 Complete!")
	fmt.Println()

	// ===== Phase 4: Meeting Summary Test =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📝 Phase 4: Meeting Summary Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	if r.transcriptFile != "" {
		summaryDir := filepath.Join(r.outputDir, "summary")
		summaryContent, summaryMetrics, err := r.runSummary(summaryDir)
		if err != nil {
			fmt.Printf("⚠️  Summary test failed: %v\n", err)
		} else {
			report.SummaryOutputDir = summaryDir
			report.SummaryMetrics = summaryMetrics
			report.SummaryContent = summaryContent
			fmt.Println("✅ Phase 4 Complete!")
		}
	} else {
		fmt.Println("⚠️  No transcript file provided, skipping summary test")
	}
	fmt.Println()

	// Finalize report
	report.EndTime = time.Now()
	report.TotalDuration = report.EndTime.Sub(report.StartTime)

	// Generate final report
	if err := r.generateFinalReport(report); err != nil {
		return nil, fmt.Errorf("failed to generate final report: %w", err)
	}

	return report, nil
}

func (r *Runner) printHeader() {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              LLM Benchmark Kit - Full Test Mode                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📋 Model:     %s\n", r.cfg.ModelName)
	fmt.Printf("🔗 URL:       %s\n", r.cfg.URL)
	fmt.Printf("📁 Output:    %s\n", r.outputDir)
	fmt.Println()
}

// writeLog writes a formatted message to the log file
func (r *Runner) writeLog(format string, args ...interface{}) {
	if r.logFile != nil {
		msg := fmt.Sprintf(format, args...)
		r.logFile.WriteString(msg + "\n")
	}
}

// ========== Phase 1: Performance Tests ==========

func (r *Runner) runFirstCallTest(count int) *PhaseResult {
	results := make([]TestResult, 0, count)

	// Questions that require a short paragraph answer (50-100 tokens)
	// Avoid complex reasoning, focus on factual descriptions
	prompts := []string{
		"请用三句话介绍一下人工智能的主要应用场景。",
		"请用三句话说明云计算的主要优势。",
		"请用三句话描述电子商务的发展趋势。",
	}

	for i := 0; i < count && i < len(prompts); i++ {
		name := fmt.Sprintf("first_call_%d", i+1)
		result := r.executeSingleRequest(name, prompts[i])
		results = append(results, result)
		time.Sleep(100 * time.Millisecond) // Small delay between calls
	}

	return r.aggregateResults("First Call Test", results)
}

func (r *Runner) runConcurrentTest(concurrency, rounds int) *PhaseResult {
	results := make([]TestResult, 0, concurrency*rounds)

	// Tasks that generate moderate output (30-80 tokens)
	prompts := []string{
		"请用两句话解释什么是机器学习。",
		"请用两句话说明5G网络的特点。",
		"请用两句话介绍区块链技术。",
		"请用两句话描述物联网的应用。",
	}

	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup
		var mu sync.Mutex
		roundResults := make([]TestResult, concurrency)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				name := fmt.Sprintf("concurrent_%d_%d", round, idx)
				promptIdx := (round*concurrency + idx) % len(prompts)
				result := r.executeSingleRequest(name, prompts[promptIdx])
				mu.Lock()
				roundResults[idx] = result
				mu.Unlock()
			}(i)
		}
		wg.Wait()
		results = append(results, roundResults...)
	}

	return r.aggregateResults("Concurrent Test", results)
}

func (r *Runner) runMultiTurnTest(turns int) *PhaseResult {
	results := make([]TestResult, 0, turns)

	// Questions requiring complete paragraph answers (40-80 tokens each)
	prompts := []string{
		"请用两句话介绍一下你自己。",
		"请用三句话说明为什么编程很重要。",
		"请用两句话描述一下春天的景色。",
		"请用三句话说明健康饮食的重要性。",
		"请用两句话介绍一本你推荐的书。",
	}

	for i := 0; i < turns && i < len(prompts); i++ {
		name := fmt.Sprintf("turn_%d", i+1)
		result := r.executeSingleRequest(name, prompts[i])
		results = append(results, result)
	}

	return r.aggregateResults("Multi-turn Test", results)
}

func (r *Runner) executeSingleRequest(name, prompt string) TestResult {
	start := time.Now()

	// Build raw request body for logging
	requestBody := map[string]interface{}{
		"model": r.cfg.ModelName,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": r.cfg.MaxTokens,
		"stream":     true,
	}
	rawRequestBody, _ := json.MarshalIndent(requestBody, "", "  ")

	// Log raw request
	r.writeLog("")
	r.writeLog("════════════════════════════════════════════════════════════════")
	r.writeLog("[%s] REQUEST", name)
	r.writeLog("════════════════════════════════════════════════════════════════")
	r.writeLog("Time: %s", start.Format("2006-01-02 15:04:05.000"))
	r.writeLog("URL: %s", r.cfg.URL)
	r.writeLog("Method: POST")
	r.writeLog("Headers:")
	r.writeLog("  Content-Type: application/json")
	if r.cfg.Token != "" {
		r.writeLog("  Authorization: Bearer %s...", r.cfg.Token[:min(10, len(r.cfg.Token))])
	}
	r.writeLog("Body:")
	r.writeLog("%s", string(rawRequestBody))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.cfg.TimeoutSec)*time.Second)
	defer cancel()

	// Create workload input using the proper type
	input := workload.NewChatWorkload(name, []workload.ChatMessage{
		{Role: "user", Content: prompt},
	}, r.cfg.MaxTokens)

	// Use the provider's StreamChat
	events, err := r.p.StreamChat(ctx, r.cfg, input)

	if err != nil {
		r.writeLog("")
		r.writeLog("[%s] RESPONSE (ERROR)", name)
		r.writeLog("Error: %s", err.Error())
		r.writeLog("Latency: %.2f ms", float64(time.Since(start).Milliseconds()))
		return TestResult{
			Name:      name,
			Success:   false,
			LatencyMs: float64(time.Since(start).Milliseconds()),
			Error:     err.Error(),
		}
	}

	// Log raw response
	r.writeLog("")
	r.writeLog("────────────────────────────────────────────────────────────────")
	r.writeLog("[%s] RESPONSE (SSE Stream)", name)
	r.writeLog("────────────────────────────────────────────────────────────────")

	var tokens int
	var responseContent strings.Builder
	var rawFrames []string
	for event := range events {
		// Capture raw SSE frame
		if event.Raw != "" {
			rawFrames = append(rawFrames, event.Raw)
			r.writeLog("data: %s", event.Raw)
		}
		if event.Type == provider.EventContent || event.Type == provider.EventReasoning {
			responseContent.WriteString(event.Text)
		}
		if event.Type == provider.EventUsage && event.Usage != nil {
			tokens = event.Usage.CompletionTokens
		}
		if event.Type == provider.EventError {
			r.writeLog("Error: %s", event.Err.Error())
			r.writeLog("Latency: %.2f ms", float64(time.Since(start).Milliseconds()))
			return TestResult{
				Name:      name,
				Success:   false,
				LatencyMs: float64(time.Since(start).Milliseconds()),
				Error:     event.Err.Error(),
			}
		}
	}

	latency := float64(time.Since(start).Milliseconds())

	// Log summary
	r.writeLog("")
	r.writeLog("[%s] SUMMARY", name)
	r.writeLog("Total SSE Frames: %d", len(rawFrames))
	r.writeLog("Output Tokens: %d", tokens)
	r.writeLog("Latency: %.2f ms", latency)
	r.writeLog("Status: SUCCESS")

	return TestResult{
		Name:      name,
		Success:   true,
		LatencyMs: latency,
		Tokens:    tokens,
	}
}

func (r *Runner) aggregateResults(phaseName string, results []TestResult) *PhaseResult {
	phase := &PhaseResult{
		PhaseName: phaseName,
		Results:   results,
	}

	var totalLatency float64
	var totalTokens int

	for _, res := range results {
		if res.Success {
			phase.Success++
			totalLatency += res.LatencyMs
			totalTokens += res.Tokens
		} else {
			phase.Failure++
		}
	}

	if phase.Success > 0 {
		phase.AvgLatencyMs = totalLatency / float64(phase.Success)
		phase.TotalTokens = totalTokens
		phase.Throughput = float64(totalTokens) / (totalLatency / 1000.0)
	}

	return phase
}

func (r *Runner) printPhaseResults(phase *PhaseResult) {
	for _, res := range phase.Results {
		if res.Success {
			fmt.Printf("   ✅ %-15s | %8.2f ms | %4d tokens\n", res.Name, res.LatencyMs, res.Tokens)
		} else {
			fmt.Printf("   ❌ %-15s | %8.2f ms | Error: %s\n", res.Name, res.LatencyMs, res.Error)
		}
	}
	fmt.Printf("   平均延迟: %.2f ms | 成功: %d/%d\n\n", phase.AvgLatencyMs, phase.Success, phase.Success+phase.Failure)
}

// ========== Environment Info Collection ==========

func (r *Runner) collectEnvironmentInfo() *EnvironmentInfo {
	env := &EnvironmentInfo{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPUThreads:  runtime.NumCPU(),
		GoVersion:   runtime.Version(),
		ToolVersion: "1.0.0",
		CollectTime: time.Now().Format("2006-01-02 15:04:05"),
		ExtraInfo:   make(map[string]string),
	}

	// Hostname
	if hostname, err := os.Hostname(); err == nil {
		env.Hostname = hostname
	}

	// Kernel version
	if runtime.GOOS == "linux" {
		if out, err := exec.Command("uname", "-r").Output(); err == nil {
			env.Kernel = strings.TrimSpace(string(out))
		}
		// CPU model
		if out, err := exec.Command("bash", "-c", "grep 'model name' /proc/cpuinfo | head -1 | cut -d: -f2").Output(); err == nil {
			env.CPUModel = strings.TrimSpace(string(out))
		}
		// CPU physical cores
		if out, err := exec.Command("bash", "-c", "grep 'cpu cores' /proc/cpuinfo | head -1 | cut -d: -f2").Output(); err == nil {
			coresStr := strings.TrimSpace(string(out))
			fmt.Sscanf(coresStr, "%d", &env.CPUCores)
		}
		// Total memory
		if out, err := exec.Command("bash", "-c", "grep 'MemTotal' /proc/meminfo | awk '{printf \"%.1f GB\", $2/1024/1024}'").Output(); err == nil {
			env.TotalMemory = strings.TrimSpace(string(out))
		}
		// GPU info (nvidia-smi)
		if out, err := exec.Command("bash", "-c", "nvidia-smi --query-gpu=name,memory.total --format=csv,noheader,nounits 2>/dev/null | head -4").Output(); err == nil {
			gpuInfo := strings.TrimSpace(string(out))
			if gpuInfo != "" {
				env.GPUInfo = gpuInfo
			}
		}
		// OS release
		if out, err := exec.Command("bash", "-c", "cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d'\"' -f2").Output(); err == nil {
			osRelease := strings.TrimSpace(string(out))
			if osRelease != "" {
				env.ExtraInfo["os_release"] = osRelease
			}
		}
	} else if runtime.GOOS == "darwin" {
		if out, err := exec.Command("uname", "-r").Output(); err == nil {
			env.Kernel = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			env.CPUModel = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("sysctl", "-n", "hw.physicalcpu").Output(); err == nil {
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &env.CPUCores)
		}
		if out, err := exec.Command("bash", "-c", "sysctl -n hw.memsize | awk '{printf \"%.1f GB\", $1/1024/1024/1024}'").Output(); err == nil {
			env.TotalMemory = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			env.ExtraInfo["os_release"] = "macOS " + strings.TrimSpace(string(out))
		}
	}

	// If CPU cores not detected, use threads count
	if env.CPUCores == 0 {
		env.CPUCores = env.CPUThreads
	}

	return env
}

func (r *Runner) printEnvironmentInfo(env *EnvironmentInfo) {
	fmt.Println("   ┌─────────────────────────────────────────────────────────────┐")
	fmt.Printf("   │ 主机名:    %-48s│\n", env.Hostname)
	if env.ExtraInfo["os_release"] != "" {
		fmt.Printf("   │ 操作系统:  %-48s│\n", env.ExtraInfo["os_release"])
	}
	fmt.Printf("   │ OS/Arch:   %-48s│\n", env.OS+"/"+env.Arch)
	fmt.Printf("   │ 内核:      %-48s│\n", env.Kernel)
	fmt.Printf("   │ CPU型号:   %-48s│\n", env.CPUModel)
	fmt.Printf("   │ CPU核心:   %-3d 核 / %-3d 线程%-30s│\n", env.CPUCores, env.CPUThreads, "")
	fmt.Printf("   │ 总内存:    %-48s│\n", env.TotalMemory)
	if env.GPUInfo != "" {
		lines := strings.Split(env.GPUInfo, "\n")
		for i, line := range lines {
			if i == 0 {
				fmt.Printf("   │ GPU:       %-48s│\n", strings.TrimSpace(line))
			} else {
				fmt.Printf("   │            %-48s│\n", strings.TrimSpace(line))
			}
		}
	}
	fmt.Printf("   │ Go版本:    %-48s│\n", env.GoVersion)
	fmt.Println("   └─────────────────────────────────────────────────────────────┘")
}

// ========== Graduated Concurrency Test ==========

func (r *Runner) runGraduatedConcurrencyTest() *GraduatedConcurrencyResult {
	// Default concurrency levels - start from higher levels since 1-3 already tested in Phase 1
	concurrencyLevels := []int{4, 8, 16, 32, 64, 128}

	result := &GraduatedConcurrencyResult{
		Levels:           make([]ConcurrencyLevelResult, 0, len(concurrencyLevels)),
		RequestsPerLevel: 0, // dynamic, shown per level
	}

	// Prompts for concurrency test (short, consistent tasks)
	prompts := []string{
		"请用一句话解释什么是人工智能。",
		"请用一句话介绍云计算。",
		"请用一句话说明区块链。",
		"请用一句话描述物联网。",
		"请用一句话解释大数据。",
		"请用一句话说明5G技术。",
		"请用一句话介绍机器学习。",
		"请用一句话描述深度学习。",
		"请用一句话解释自然语言处理。",
		"请用一句话介绍计算机视觉。",
		"请用一句话说明边缘计算。",
		"请用一句话描述微服务架构。",
		"请用一句话解释容器化技术。",
		"请用一句话介绍DevOps。",
		"请用一句话说明网络安全。",
		"请用一句话描述数字孪生。",
	}

	fmt.Printf("   并发级别: %v | 每级请求数: max(并发数×2, 12)\n\n", concurrencyLevels)
	fmt.Println("   ┌───────────┬──────────┬──────────┬──────────────┬──────────────┬──────────────┬──────────┬──────────┐")
	fmt.Println("   │ 并发数    │ 成功/总数│ 平均延迟 │ 最小延迟     │ 最大延迟     │ 吞吐(tok/s)  │ RPS      │ 耗时(ms) │")
	fmt.Println("   ├───────────┼──────────┼──────────┼──────────────┼──────────────┼──────────────┼──────────┼──────────┤")

	for _, concurrency := range concurrencyLevels {
		// Dynamic requests: at least 2x concurrency to fully saturate, minimum 12
		requestsForLevel := concurrency * 2
		if requestsForLevel < 12 {
			requestsForLevel = 12
		}
		levelResult := r.runSingleConcurrencyLevel(concurrency, requestsForLevel, prompts)
		result.Levels = append(result.Levels, levelResult)

		fmt.Printf("   │ %-9d │ %4d/%-4d│ %8.0f │ %10.0f   │ %10.0f   │ %10.1f   │ %6.2f   │ %8.0f │\n",
			levelResult.Concurrency,
			levelResult.SuccessCount, levelResult.TotalRequests,
			levelResult.AvgLatencyMs,
			levelResult.MinLatencyMs,
			levelResult.MaxLatencyMs,
			levelResult.Throughput,
			levelResult.RPS,
			levelResult.WallTimeMs)
	}

	fmt.Println("   └───────────┴──────────┴──────────┴──────────────┴──────────────┴──────────────┴──────────┴──────────┘")
	fmt.Println()

	return result
}

func (r *Runner) runSingleConcurrencyLevel(concurrency, totalRequests int, prompts []string) ConcurrencyLevelResult {
	levelResult := ConcurrencyLevelResult{
		Concurrency:   concurrency,
		TotalRequests: totalRequests,
	}

	type singleResult struct {
		latencyMs float64
		ttftMs    float64
		tokens    int
		success   bool
	}

	results := make([]singleResult, totalRequests)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)

	wallStart := time.Now()

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			prompt := prompts[idx%len(prompts)]
			start := time.Now()
			var firstTokenTime time.Time
			gotFirstToken := false

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.cfg.TimeoutSec)*time.Second)
			defer cancel()

			input := workload.NewChatWorkload(
				fmt.Sprintf("grad_c%d_%d", concurrency, idx),
				[]workload.ChatMessage{{Role: "user", Content: prompt}},
				256,
			)

			events, err := r.p.StreamChat(ctx, r.cfg, input)
			if err != nil {
				mu.Lock()
				results[idx] = singleResult{
					latencyMs: float64(time.Since(start).Milliseconds()),
					success:   false,
				}
				mu.Unlock()
				return
			}

			var tokens int
			for event := range events {
				if (event.Type == provider.EventContent || event.Type == provider.EventReasoning) && !gotFirstToken {
					firstTokenTime = time.Now()
					gotFirstToken = true
				}
				if event.Type == provider.EventUsage && event.Usage != nil {
					tokens = event.Usage.CompletionTokens
				}
				if event.Type == provider.EventError {
					mu.Lock()
					results[idx] = singleResult{
						latencyMs: float64(time.Since(start).Milliseconds()),
						success:   false,
					}
					mu.Unlock()
					return
				}
			}

			latency := float64(time.Since(start).Milliseconds())
			ttft := latency
			if gotFirstToken {
				ttft = float64(firstTokenTime.Sub(start).Milliseconds())
			}

			mu.Lock()
			results[idx] = singleResult{
				latencyMs: latency,
				ttftMs:    ttft,
				tokens:    tokens,
				success:   true,
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	wallTime := float64(time.Since(wallStart).Milliseconds())
	levelResult.WallTimeMs = wallTime

	// Aggregate results
	var totalLatency, totalTTFT float64
	var totalTokens int
	minLatency := float64(1<<63 - 1)
	maxLatency := float64(0)

	for _, res := range results {
		if res.success {
			levelResult.SuccessCount++
			totalLatency += res.latencyMs
			totalTTFT += res.ttftMs
			totalTokens += res.tokens
			if res.latencyMs < minLatency {
				minLatency = res.latencyMs
			}
			if res.latencyMs > maxLatency {
				maxLatency = res.latencyMs
			}
		} else {
			levelResult.FailureCount++
		}
	}

	if levelResult.SuccessCount > 0 {
		levelResult.AvgLatencyMs = totalLatency / float64(levelResult.SuccessCount)
		levelResult.AvgTTFTMs = totalTTFT / float64(levelResult.SuccessCount)
		levelResult.MinLatencyMs = minLatency
		levelResult.MaxLatencyMs = maxLatency
		levelResult.TotalTokens = totalTokens
		levelResult.Throughput = float64(totalTokens) / (wallTime / 1000.0)
		levelResult.RPS = float64(levelResult.SuccessCount) / (wallTime / 1000.0)
	}

	return levelResult
}

// ========== Phase 2: Function Call Test ==========

func (r *Runner) runFunctionCallTest() *FunctionCallResult {
	fmt.Println("   测试 Query: \"北京今天天气怎么样？\"")
	fmt.Println("   期望调用: get_weather(city=\"北京\")")
	fmt.Println()

	result := &FunctionCallResult{}
	start := time.Now()

	// Build request with tools
	requestBody := map[string]interface{}{
		"model": r.cfg.ModelName,
		"messages": []map[string]string{
			{"role": "user", "content": "北京今天天气怎么样？"},
		},
		"max_tokens": 512, // Enough for function call response
		"stream":     false,
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "获取指定城市的天气信息",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"city": map[string]string{
								"type":        "string",
								"description": "城市名称",
							},
						},
						"required": []string{"city"},
					},
				},
			},
		},
		"tool_choice": "auto",
	}

	jsonBody, _ := json.Marshal(requestBody)
	prettyReq, _ := json.MarshalIndent(requestBody, "", "  ")

	// Log raw request
	r.writeLog("")
	r.writeLog("════════════════════════════════════════════════════════════════")
	r.writeLog("[Function Call Test] REQUEST")
	r.writeLog("════════════════════════════════════════════════════════════════")
	r.writeLog("Time: %s", start.Format("2006-01-02 15:04:05.000"))
	r.writeLog("URL: %s", r.cfg.URL)
	r.writeLog("Method: POST")
	r.writeLog("Headers:")
	r.writeLog("  Content-Type: application/json")
	if r.cfg.Token != "" {
		r.writeLog("  Authorization: Bearer %s...", r.cfg.Token[:min(10, len(r.cfg.Token))])
	}
	r.writeLog("Body:")
	r.writeLog("%s", string(prettyReq))

	req, err := http.NewRequest("POST", r.cfg.URL, bytes.NewBuffer(jsonBody))
	if err != nil {
		result.Error = err.Error()
		r.writeLog("Error: %s", err.Error())
		return result
	}

	req.Header.Set("Content-Type", "application/json")
	if r.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		result.Error = err.Error()
		result.LatencyMs = float64(time.Since(start).Milliseconds())
		r.writeLog("")
		r.writeLog("[Function Call Test] RESPONSE (ERROR)")
		r.writeLog("Error: %s", err.Error())
		r.writeLog("Latency: %.2f ms", result.LatencyMs)
		return result
	}
	defer resp.Body.Close()

	result.LatencyMs = float64(time.Since(start).Milliseconds())

	// Read raw response body
	body, _ := io.ReadAll(resp.Body)

	// Log raw response
	r.writeLog("")
	r.writeLog("────────────────────────────────────────────────────────────────")
	r.writeLog("[Function Call Test] RESPONSE")
	r.writeLog("────────────────────────────────────────────────────────────────")
	r.writeLog("HTTP Status: %d %s", resp.StatusCode, resp.Status)
	r.writeLog("Headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			r.writeLog("  %s: %s", key, value)
		}
	}
	r.writeLog("Body:")
	// Pretty print if JSON
	var prettyResp bytes.Buffer
	if json.Indent(&prettyResp, body, "", "  ") == nil {
		r.writeLog("%s", prettyResp.String())
	} else {
		r.writeLog("%s", string(body))
	}
	r.writeLog("Latency: %.2f ms", result.LatencyMs)

	if resp.StatusCode != 200 {
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		r.writeLog("Status: FAILED")
		return result
	}

	// Parse response
	var respData struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &respData); err != nil {
		result.Error = fmt.Sprintf("Failed to parse response: %v", err)
		r.writeLog("Parse Error: %s", result.Error)
		r.writeLog("Status: FAILED")
		return result
	}

	// Check if function call is supported
	r.writeLog("")
	r.writeLog("[Function Call Test] SUMMARY")
	if len(respData.Choices) > 0 && len(respData.Choices[0].Message.ToolCalls) > 0 {
		result.Supported = true
		toolCall := respData.Choices[0].Message.ToolCalls[0]
		result.FunctionName = toolCall.Function.Name
		result.Arguments = toolCall.Function.Arguments

		// Verify function name
		result.CorrectFunction = toolCall.Function.Name == "get_weather"

		// Verify arguments contain city
		var args map[string]interface{}
		if json.Unmarshal([]byte(toolCall.Function.Arguments), &args) == nil {
			if city, ok := args["city"]; ok {
				result.CorrectArgs = city != nil && city != ""
			}
		}
		r.writeLog("Function Call Supported: YES")
		r.writeLog("Function Name: %s", result.FunctionName)
		r.writeLog("Arguments: %s", result.Arguments)
		r.writeLog("Correct Function: %v", result.CorrectFunction)
		r.writeLog("Correct Args: %v", result.CorrectArgs)
		r.writeLog("Status: SUCCESS")
	} else {
		result.Supported = false
		r.writeLog("Function Call Supported: NO")
		r.writeLog("Status: Function Call NOT SUPPORTED")
	}

	return result
}

func (r *Runner) printFunctionCallResult(result *FunctionCallResult) {
	if result.Error != "" {
		fmt.Printf("   ❌ 测试失败: %s\n", result.Error)
		return
	}

	if result.Supported {
		fmt.Printf("   ✅ Function Call 支持: 是\n")
		if result.CorrectFunction {
			fmt.Printf("   ✅ 正确识别函数: %s\n", result.FunctionName)
		} else {
			fmt.Printf("   ❌ 函数名不匹配: %s (期望: get_weather)\n", result.FunctionName)
		}
		if result.CorrectArgs {
			fmt.Printf("   ✅ 参数解析正确: %s\n", result.Arguments)
		} else {
			fmt.Printf("   ⚠️  参数可能不完整: %s\n", result.Arguments)
		}
	} else {
		fmt.Printf("   ❌ Function Call 支持: 否 (模型未返回 tool_calls)\n")
	}
	fmt.Printf("   ⏱️  响应延迟: %.2f ms\n\n", result.LatencyMs)
}

// ========== Phase 3: Long Context Test ==========

// generateLongContext generates a context of specified character length
func (r *Runner) generateLongContext(targetChars int) string {
	// Base content to repeat (approximately 500 chars per block)
	baseContent := `这是一段用于测试长上下文能力的文本内容。在人工智能和大语言模型的发展过程中，处理长文本的能力变得越来越重要。
现代的大语言模型需要能够理解和处理长达数万甚至数十万字符的输入文本。这对于文档摘要、长篇对话、代码理解等任务至关重要。
我们通过不同长度的上下文来测试模型的处理能力，包括响应时间、首字延迟和输出质量等指标。`

	// Calculate how many times to repeat
	repeats := (targetChars / len(baseContent)) + 1

	var sb strings.Builder
	for i := 0; i < repeats && sb.Len() < targetChars; i++ {
		sb.WriteString(fmt.Sprintf("\n[段落 %d]\n%s\n", i+1, baseContent))
	}

	result := sb.String()
	if len(result) > targetChars {
		result = result[:targetChars]
	}
	return result
}

func (r *Runner) runLongContextTest() *LongContextResult {
	result := &LongContextResult{
		Results: make([]LongContextTestResult, 0),
	}

	// Test different context lengths: 1K, 4K, 8K, 16K, 32K chars
	// Approximately 1 Chinese char ≈ 0.7 token, 1 English word ≈ 1.3 token
	contextLengths := []int{1000, 4000, 8000, 16000, 32000}

	fmt.Println("   测试不同上下文长度下的模型性能...")
	fmt.Println("   ┌─────────────┬──────────────┬──────────────┬──────────────┬──────────────┬────────┐")
	fmt.Println("   │ 上下文长度  │ 估算Tokens   │ TTFT (ms)    │ Latency (ms) │ 吞吐 (tok/s) │ 状态   │")
	fmt.Println("   ├─────────────┼──────────────┼──────────────┼──────────────┼──────────────┼────────┤")

	var totalTTFT, totalLatency, totalThroughput float64
	successCount := 0

	for _, length := range contextLengths {
		testResult := r.executeLongContextRequest(length)
		result.Results = append(result.Results, testResult)

		// Print result row
		status := "✅"
		if !testResult.Success {
			status = "❌"
		} else {
			successCount++
			totalTTFT += testResult.TTFTMs
			totalLatency += testResult.LatencyMs
			totalThroughput += testResult.Throughput
			result.MaxSupported = length
		}

		fmt.Printf("   │ %9d字 │ %10d   │ %10.2f   │ %10.2f   │ %10.2f   │ %s     │\n",
			length, testResult.InputTokens, testResult.TTFTMs, testResult.LatencyMs, testResult.Throughput, status)
	}

	fmt.Println("   └─────────────┴──────────────┴──────────────┴──────────────┴──────────────┴────────┘")

	// Calculate averages
	if successCount > 0 {
		result.AvgTTFTMs = totalTTFT / float64(successCount)
		result.AvgLatencyMs = totalLatency / float64(successCount)
		result.AvgThroughput = totalThroughput / float64(successCount)
	}

	fmt.Printf("\n   📊 最大支持上下文: %d 字符\n", result.MaxSupported)
	fmt.Printf("   📊 平均 TTFT: %.2f ms | 平均 Latency: %.2f ms | 平均吞吐: %.2f tokens/s\n\n",
		result.AvgTTFTMs, result.AvgLatencyMs, result.AvgThroughput)

	return result
}

func (r *Runner) executeLongContextRequest(contextLength int) LongContextTestResult {
	result := LongContextTestResult{
		ContextLength: contextLength,
		InputTokens:   int(float64(contextLength) * 0.7), // Rough estimate for Chinese text
	}

	start := time.Now()
	var firstTokenTime time.Time
	gotFirstToken := false

	// Generate long context
	longContext := r.generateLongContext(contextLength)

	// Create prompt with long context
	prompt := fmt.Sprintf(`以下是一段长文本，请阅读后用一句话总结其主题：

%s

请用一句话（不超过50字）总结上述内容的主题：`, longContext)

	// Log the request
	r.writeLog("")
	r.writeLog("════════════════════════════════════════════════════════════════")
	r.writeLog("[Long Context Test - %d chars] REQUEST", contextLength)
	r.writeLog("════════════════════════════════════════════════════════════════")
	r.writeLog("Time: %s", start.Format("2006-01-02 15:04:05.000"))
	r.writeLog("Context Length: %d chars (estimated %d tokens)", contextLength, result.InputTokens)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.cfg.TimeoutSec)*time.Second)
	defer cancel()

	// Create workload input
	input := workload.NewChatWorkload(
		fmt.Sprintf("long_context_%d", contextLength),
		[]workload.ChatMessage{
			{Role: "user", Content: prompt},
		},
		256, // Limited output tokens for summary
	)

	// Use the provider's StreamChat
	events, err := r.p.StreamChat(ctx, r.cfg, input)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.LatencyMs = float64(time.Since(start).Milliseconds())
		r.writeLog("Error: %s", err.Error())
		return result
	}

	// Process stream events
	var outputTokens int
	for event := range events {
		if (event.Type == provider.EventContent || event.Type == provider.EventReasoning) && !gotFirstToken {
			firstTokenTime = time.Now()
			gotFirstToken = true
			r.writeLog("First token at: %.2f ms", float64(firstTokenTime.Sub(start).Milliseconds()))
		}
		if event.Type == provider.EventUsage && event.Usage != nil {
			outputTokens = event.Usage.CompletionTokens
		}
		if event.Type == provider.EventError {
			result.Success = false
			result.Error = event.Err.Error()
			result.LatencyMs = float64(time.Since(start).Milliseconds())
			r.writeLog("Error: %s", event.Err.Error())
			return result
		}
	}

	endTime := time.Now()
	result.LatencyMs = float64(endTime.Sub(start).Milliseconds())
	result.OutputTokens = outputTokens

	if gotFirstToken {
		result.TTFTMs = float64(firstTokenTime.Sub(start).Milliseconds())
	} else {
		result.TTFTMs = result.LatencyMs
	}

	// Calculate throughput based on generation phase only (excluding TTFT)
	// Throughput = output tokens / generation time
	// Generation time = total latency - TTFT
	generationTimeMs := result.LatencyMs - result.TTFTMs
	if generationTimeMs > 0 && outputTokens > 0 {
		result.Throughput = float64(outputTokens) / (generationTimeMs / 1000.0)
	} else if result.LatencyMs > 0 && outputTokens > 0 {
		// Fallback: if generation time is 0 or negative, use total latency
		result.Throughput = float64(outputTokens) / (result.LatencyMs / 1000.0)
	}

	result.Success = true

	r.writeLog("Output Tokens: %d", outputTokens)
	r.writeLog("TTFT: %.2f ms", result.TTFTMs)
	r.writeLog("Latency: %.2f ms", result.LatencyMs)
	r.writeLog("Throughput: %.2f tokens/s", result.Throughput)
	r.writeLog("Status: SUCCESS")

	return result
}

func (r *Runner) printLongContextResult(result *LongContextResult) {
	if result == nil {
		fmt.Println("   ⚠️ 长上下文测试未完成")
		return
	}

	successCount := 0
	for _, res := range result.Results {
		if res.Success {
			successCount++
		}
	}

	fmt.Printf("   成功: %d/%d | 最大支持: %d 字符 | 平均TTFT: %.2f ms | 平均吞吐: %.2f tokens/s\n\n",
		successCount, len(result.Results), result.MaxSupported, result.AvgTTFTMs, result.AvgThroughput)
}

func (r *Runner) printLongContextConcurrentResult(result *LongContextConcurrentResult) {
	if result == nil {
		fmt.Println("   ⚠️ 长上下文并发测试未完成")
		return
	}

	fmt.Println("   Context(chars) | Concurrency | Success | Avg TTFT(ms) | Avg Latency(ms) | Throughput(tok/s) | P95 TTFT | RPS")
	fmt.Println("   " + strings.Repeat("-", 120))

	for _, level := range result.Levels {
		fmt.Printf("   %15d | %11d | %3d/%-3d | %12.2f | %15.2f | %17.2f | %8.2f | %8.2f\n",
			level.ContextLength, level.Concurrency, level.SuccessCount, level.TotalRequests,
			level.AvgTTFTMs, level.AvgLatencyMs, level.Throughput,
			level.P95TTFTMs, level.RPS)
	}
	fmt.Println()
}

// ========== Phase 3.5: Long Context Concurrent Test ==========

// generateVariedLongContext generates a long context with unique ordering for each request.
// This defeats vLLM prefix caching by ensuring each request has a different token sequence.
func (r *Runner) generateVariedLongContext(targetChars int, requestIdx int) string {
	// Different topic paragraphs - each ~200 chars
	paragraphs := []string{
		"人工智能技术的发展经历了多次浪潮。从早期的专家系统到现代的深度学习，每一次技术突破都带来了产业变革。自然语言处理、计算机视觉、语音识别等领域取得了显著进展。",
		"云计算基础设施为企业数字化转型提供了强大支撑。公有云、私有云和混合云架构各有优势，企业需要根据业务特点选择合适的部署方案。容器化和微服务架构成为主流趋势。",
		"区块链作为分布式账本技术，在金融、供应链、医疗等领域展现出巨大潜力。智能合约的引入使得去中心化应用成为可能，DeFi和NFT等新兴概念推动了Web3的发展。",
		"网络安全威胁日益复杂，零信任架构成为企业安全建设的新范式。从边界安全到身份驱动的安全模型转变，要求组织重新审视访问控制、数据保护和威胁检测策略。",
		"数据科学与大数据分析帮助企业从海量数据中提取价值。数据湖、数据仓库和实时流处理平台构成了现代数据基础设施。机器学习模型的训练和部署流程日益成熟。",
		"物联网设备的普及连接了物理世界与数字世界。传感器网络、边缘计算和5G通信技术的融合，推动了智能制造、智慧城市和自动驾驶等应用场景的落地实践。",
		"量子计算代表了计算能力的下一次飞跃。量子比特的叠加态和纠缠效应使其在密码学、材料模拟和优化问题上具有经典计算机无法比拟的优势。各国纷纷加大投入。",
		"DevOps实践缩短了软件交付周期，持续集成和持续部署流水线提升了开发效率。基础设施即代码、监控告警和混沌工程等实践确保了系统的可靠性和可观测性。",
		"边缘计算将数据处理能力从云端推向网络边缘，降低了延迟并节省了带宽。在工业互联网、视频分析和AR/VR等场景中发挥着重要作用，与云计算形成互补关系。",
		"数字孪生技术通过创建物理实体的虚拟副本，实现了实时监控和预测性维护。在制造业、建筑和医疗领域的应用正在快速扩展，结合AI可以实现智能优化决策。",
		"机器人流程自动化简化了企业重复性业务流程。结合自然语言处理和计算机视觉技术，智能RPA可以处理非结构化数据，扩展了自动化的应用边界和商业价值。",
		"开源软件运动深刻改变了软件行业格局。从Linux内核到Kubernetes容器编排，从TensorFlow到PyTorch，开源项目成为技术创新的重要推动力。",
	}

	// Different question suffixes to make tail different too
	questions := []string{
		"请概括上述内容涉及的主要技术领域，不超过100字。",
		"从上述内容中提取三个最重要的技术趋势，简要说明。",
		"根据以上文本，分析技术发展的核心驱动力是什么？",
		"请对上述内容按照技术成熟度进行分类，简要列出。",
		"综合以上信息，哪些技术将在未来五年内产生最大影响？",
		"请归纳上述各段落的共同主题，并用一句话总结。",
		"从以上内容中找出与数据处理最相关的技术，并简要解释。",
		"根据上述文本讨论的内容，企业数字化转型的核心挑战是什么？",
	}

	// Use requestIdx to create unique paragraph ordering
	// Shuffle: start from different paragraph, rotate order
	startIdx := requestIdx % len(paragraphs)
	step := (requestIdx/len(paragraphs))%3 + 1 // 1, 2, or 3

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("以下是第%d份技术分析文档，请仔细阅读后回答问题：\n\n", requestIdx+1))

	paraIdx := startIdx
	blockNum := 0
	for sb.Len() < targetChars {
		blockNum++
		sb.WriteString(fmt.Sprintf("[第%d节·文档%d]\n%s\n\n", blockNum, requestIdx+1, paragraphs[paraIdx%len(paragraphs)]))
		paraIdx += step
	}

	result := sb.String()
	if len(result) > targetChars {
		result = result[:targetChars]
	}

	// Append a unique question
	question := questions[requestIdx%len(questions)]
	result += "\n\n" + question

	return result
}

func (r *Runner) runLongContextConcurrentTest() *LongContextConcurrentResult {
	result := &LongContextConcurrentResult{
		Levels: make([]LongContextConcurrentLevelResult, 0),
	}

	// Test matrix: context lengths × concurrency levels
	contextLengths := []int{4000, 8000, 16000}
	concurrencyLevels := []int{2, 5, 10}
	requestsMultiplier := 2 // requests = concurrency × multiplier

	fmt.Printf("   上下文长度: %v | 并发级别: %v | 每级请求数: 并发×%d\n", contextLengths, concurrencyLevels, requestsMultiplier)
	fmt.Println("   ⚠️  每个请求使用不同的段落顺序和问题，避免 prefix cache 影响")
	fmt.Println()
	fmt.Println("   ┌──────────┬────────┬──────────┬──────────────┬──────────────┬──────────────┬──────────────┬────────┬──────────┐")
	fmt.Println("   │ 上下文   │ 并发   │ 成功/总数│ AvgTTFT(ms)  │ P95TTFT(ms)  │ AvgLatency   │ P95Latency   │ RPS    │ 耗时(ms) │")
	fmt.Println("   ├──────────┼────────┼──────────┼──────────────┼──────────────┼──────────────┼──────────────┼────────┼──────────┤")

	globalReqIdx := 0
	for _, ctxLen := range contextLengths {
		for _, conc := range concurrencyLevels {
			totalReqs := conc * requestsMultiplier
			if totalReqs < 6 {
				totalReqs = 6
			}
			levelResult := r.runSingleLongContextConcurrentLevel(ctxLen, conc, totalReqs, &globalReqIdx)
			result.Levels = append(result.Levels, levelResult)

			fmt.Printf("   │ %6d字 │ %5d  │ %4d/%-4d│ %10.0f   │ %10.0f   │ %10.0f   │ %10.0f   │ %5.2f  │ %8.0f │\n",
				levelResult.ContextLength, levelResult.Concurrency,
				levelResult.SuccessCount, levelResult.TotalRequests,
				levelResult.AvgTTFTMs, levelResult.P95TTFTMs,
				levelResult.AvgLatencyMs, levelResult.P95LatencyMs,
				levelResult.RPS, levelResult.WallTimeMs)
		}
	}

	fmt.Println("   └──────────┴────────┴──────────┴──────────────┴──────────────┴──────────────┴──────────────┴────────┴──────────┘")
	fmt.Println()

	return result
}

func (r *Runner) runSingleLongContextConcurrentLevel(contextLength, concurrency, totalRequests int, globalReqIdx *int) LongContextConcurrentLevelResult {
	levelResult := LongContextConcurrentLevelResult{
		ContextLength: contextLength,
		Concurrency:   concurrency,
		TotalRequests: totalRequests,
	}

	type singleResult struct {
		ttftMs    float64
		latencyMs float64
		tokens    int
		success   bool
	}

	results := make([]singleResult, totalRequests)
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	wallStart := time.Now()

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		reqIdx := *globalReqIdx
		*globalReqIdx++

		go func(idx, rIdx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Generate unique context for this request (defeats prefix caching)
			context_ := r.generateVariedLongContext(contextLength, rIdx)

			start := time.Now()
			var firstTokenTime time.Time
			gotFirstToken := false

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.cfg.TimeoutSec)*time.Second)
			defer cancel()

			input := workload.NewChatWorkload(
				fmt.Sprintf("lcc_%d_c%d_%d", contextLength, concurrency, idx),
				[]workload.ChatMessage{{Role: "user", Content: context_}},
				256,
			)

			events, err := r.p.StreamChat(ctx, r.cfg, input)
			if err != nil {
				results[idx] = singleResult{
					latencyMs: float64(time.Since(start).Milliseconds()),
					success:   false,
				}
				return
			}

			var tokens int
			for event := range events {
				switch event.Type {
				case provider.EventContent, provider.EventReasoning:
					if !gotFirstToken {
						firstTokenTime = time.Now()
						gotFirstToken = true
					}
				case provider.EventUsage:
					if event.Usage != nil {
						tokens = event.Usage.CompletionTokens
					}
				case provider.EventError:
					results[idx] = singleResult{
						latencyMs: float64(time.Since(start).Milliseconds()),
						success:   false,
					}
					return
				}
			}

			latency := float64(time.Since(start).Milliseconds())
			ttft := latency
			if gotFirstToken {
				ttft = float64(firstTokenTime.Sub(start).Milliseconds())
			}

			results[idx] = singleResult{
				ttftMs:    ttft,
				latencyMs: latency,
				tokens:    tokens,
				success:   gotFirstToken, // must have received at least one token
			}
		}(i, reqIdx)
	}

	wg.Wait()
	wallTime := float64(time.Since(wallStart).Milliseconds())
	levelResult.WallTimeMs = wallTime

	// Aggregate results
	var ttfts, latencies []float64
	var totalTokens int
	for _, res := range results {
		if res.success {
			levelResult.SuccessCount++
			ttfts = append(ttfts, res.ttftMs)
			latencies = append(latencies, res.latencyMs)
			totalTokens += res.tokens
		}
	}

	if len(ttfts) > 0 {
		// Average
		var sumTTFT, sumLatency float64
		for _, v := range ttfts {
			sumTTFT += v
		}
		for _, v := range latencies {
			sumLatency += v
		}
		levelResult.AvgTTFTMs = sumTTFT / float64(len(ttfts))
		levelResult.AvgLatencyMs = sumLatency / float64(len(latencies))

		// P95 - sort and pick
		sortFloat64s(ttfts)
		sortFloat64s(latencies)
		p95Idx := int(float64(len(ttfts)) * 0.95)
		if p95Idx >= len(ttfts) {
			p95Idx = len(ttfts) - 1
		}
		levelResult.P95TTFTMs = ttfts[p95Idx]
		p95LatIdx := int(float64(len(latencies)) * 0.95)
		if p95LatIdx >= len(latencies) {
			p95LatIdx = len(latencies) - 1
		}
		levelResult.P95LatencyMs = latencies[p95LatIdx]
	}

	if wallTime > 0 {
		levelResult.RPS = float64(levelResult.SuccessCount) / (wallTime / 1000.0)
		levelResult.Throughput = float64(totalTokens) / (wallTime / 1000.0)
	}

	return levelResult
}

// sortFloat64s sorts a slice of float64 in ascending order.
func sortFloat64s(a []float64) {
	sort.Float64s(a)
}

// ========== Phase 4: Summary Test ==========

func (r *Runner) runSummary(outputDir string) (string, *summarizer.SummaryMetrics, error) {
	if _, err := os.Stat(r.transcriptFile); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("transcript file not found: %s", r.transcriptFile)
	}

	fmt.Printf("   Transcript:   %s\n", r.transcriptFile)
	fmt.Printf("   Chunk Size:   8000 chars\n")
	fmt.Println()

	meetingTime := time.Now().Format("2006-01-02 15:04")
	sum := summarizer.NewSummarizer(r.cfg, 8000, meetingTime)

	content, metrics, err := sum.RunWithMetrics(r.transcriptFile, outputDir)
	if err != nil {
		return "", nil, err
	}

	return content, metrics, nil
}

// ========== Report Generation ==========

func (r *Runner) generateFinalReport(report *FullTestReport) error {
	var sb strings.Builder

	sb.WriteString("# LLM 完整测试报告\n\n")
	sb.WriteString(fmt.Sprintf("**生成时间**: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// Basic Info
	sb.WriteString("## 基本信息\n\n")
	sb.WriteString("| 项目 | 值 |\n")
	sb.WriteString("|------|----|\\n")
	sb.WriteString(fmt.Sprintf("| 模型名称 | %s |\n", report.ModelName))
	sb.WriteString(fmt.Sprintf("| API URL | %s |\n", report.APIURL))
	sb.WriteString(fmt.Sprintf("| 开始时间 | %s |\n", report.StartTime.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("| 结束时间 | %s |\n", report.EndTime.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("| 总耗时 | %.2f 秒 |\n", report.TotalDuration.Seconds()))
	sb.WriteString("\n")

	// Environment Info
	if report.Environment != nil {
		env := report.Environment
		sb.WriteString("## 环境信息\n\n")
		sb.WriteString("| 项目 | 值 |\n")
		sb.WriteString("|------|----|\n")
		sb.WriteString(fmt.Sprintf("| 主机名 | %s |\n", env.Hostname))
		if env.ExtraInfo["os_release"] != "" {
			sb.WriteString(fmt.Sprintf("| 操作系统 | %s |\n", env.ExtraInfo["os_release"]))
		}
		sb.WriteString(fmt.Sprintf("| OS/Arch | %s/%s |\n", env.OS, env.Arch))
		sb.WriteString(fmt.Sprintf("| 内核 | %s |\n", env.Kernel))
		sb.WriteString(fmt.Sprintf("| CPU | %s |\n", env.CPUModel))
		sb.WriteString(fmt.Sprintf("| CPU 核心/线程 | %d / %d |\n", env.CPUCores, env.CPUThreads))
		sb.WriteString(fmt.Sprintf("| 内存 | %s |\n", env.TotalMemory))
		if env.GPUInfo != "" {
			sb.WriteString(fmt.Sprintf("| GPU | %s |\n", env.GPUInfo))
		}
		sb.WriteString(fmt.Sprintf("| Go 版本 | %s |\n", env.GoVersion))
		sb.WriteString("\n")
	}

	// Phase 1: Performance Results
	sb.WriteString("## Phase 1: 性能测试结果\n\n")

	if report.FirstCallResults != nil {
		sb.WriteString("### 1.1 冷启动测试 (First Call)\n\n")
		r.writePhaseTable(&sb, report.FirstCallResults)
	}

	if report.ConcurrentResults != nil {
		sb.WriteString("### 1.2 并发测试 (Concurrent)\n\n")
		r.writePhaseTable(&sb, report.ConcurrentResults)
	}

	if report.MultiTurnResults != nil {
		sb.WriteString("### 1.3 多轮对话测试 (Multi-turn)\n\n")
		r.writePhaseTable(&sb, report.MultiTurnResults)
	}

	// Phase 1.5: Graduated Concurrency Results
	if report.GraduatedConcurrency != nil && len(report.GraduatedConcurrency.Levels) > 0 {
		sb.WriteString("### 1.5 逐级并发测试 (Graduated Concurrency)\n\n")
		sb.WriteString("| 并发数 | 成功/总数 | 平均延迟(ms) | 最小延迟(ms) | 最大延迟(ms) | 吞吐(tok/s) | RPS | 耗时(ms) |\n")
		sb.WriteString("|--------|-----------|-------------|-------------|-------------|-------------|------|--------|\n")
		for _, lv := range report.GraduatedConcurrency.Levels {
			sb.WriteString(fmt.Sprintf("| %d | %d/%d | %.0f | %.0f | %.0f | %.1f | %.2f | %.0f |\n",
				lv.Concurrency, lv.SuccessCount, lv.TotalRequests,
				lv.AvgLatencyMs, lv.MinLatencyMs, lv.MaxLatencyMs,
				lv.Throughput, lv.RPS, lv.WallTimeMs))
		}
		sb.WriteString("\n")
	}

	// Phase 2: Function Call Results
	sb.WriteString("## Phase 2: Function Call 测试\n\n")
	if report.FunctionCallResult != nil {
		fc := report.FunctionCallResult
		if fc.Supported {
			sb.WriteString("✅ **支持 Function Call**\n\n")
			sb.WriteString(fmt.Sprintf("- 函数名: `%s`\n", fc.FunctionName))
			sb.WriteString(fmt.Sprintf("- 参数: `%s`\n", fc.Arguments))
			sb.WriteString(fmt.Sprintf("- 响应延迟: %.2f ms\n", fc.LatencyMs))
		} else {
			sb.WriteString("❌ **不支持 Function Call**\n\n")
			if fc.Error != "" {
				sb.WriteString(fmt.Sprintf("错误信息: %s\n", fc.Error))
			}
		}
		sb.WriteString("\n")
	}

	// Phase 3: Long Context Results
	sb.WriteString("## Phase 3: 长上下文测试\n\n")
	if report.LongContextResult != nil {
		lc := report.LongContextResult
		sb.WriteString("| 上下文长度 | 估算Tokens | TTFT (ms) | Latency (ms) | 吞吐 (tok/s) | 状态 |\n")
		sb.WriteString("|------------|------------|-----------|--------------|--------------|------|\n")
		for _, res := range lc.Results {
			status := "✅"
			if !res.Success {
				status = "❌"
			}
			sb.WriteString(fmt.Sprintf("| %d 字符 | %d | %.2f | %.2f | %.2f | %s |\n",
				res.ContextLength, res.InputTokens, res.TTFTMs, res.LatencyMs, res.Throughput, status))
		}
		sb.WriteString(fmt.Sprintf("\n**最大支持上下文**: %d 字符 | **平均 TTFT**: %.2f ms | **平均吞吐**: %.2f tokens/s\n\n",
			lc.MaxSupported, lc.AvgTTFTMs, lc.AvgThroughput))
	} else {
		sb.WriteString("⚠️ 长上下文测试未完成\n\n")
	}

	// Phase 3.5: Long Context Concurrent Results
	sb.WriteString("## Phase 3.5: 长上下文并发测试\n\n")
	sb.WriteString("*使用不同内容的提示词，避免前缀缓存 (Prefix Caching) 干扰结果*\n\n")
	if report.LongContextConcurrentResult != nil && len(report.LongContextConcurrentResult.Levels) > 0 {
		sb.WriteString("| 上下文长度 | 并发数 | 成功率 | 平均TTFT (ms) | P95 TTFT | 平均延迟 (ms) | 吞吐 (tok/s) | RPS |\n")
		sb.WriteString("|------------|--------|--------|---------------|----------|---------------|--------------|------|\n")
		for _, level := range report.LongContextConcurrentResult.Levels {
			sb.WriteString(fmt.Sprintf("| %d 字符 | %d | %d/%d | %.2f | %.2f | %.2f | %.2f | %.2f |\n",
				level.ContextLength, level.Concurrency, level.SuccessCount, level.TotalRequests,
				level.AvgTTFTMs, level.P95TTFTMs, level.AvgLatencyMs, level.Throughput, level.RPS))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("⚠️ 长上下文并发测试未完成\n\n")
	}

	// Phase 4: Summary Results
	sb.WriteString("## Phase 4: 会议纪要测试\n\n")
	if report.SummaryOutputDir != "" {
		sb.WriteString(fmt.Sprintf("📁 会议纪要: [summary/meeting_summary.md](%s/meeting_summary.md)\n", report.SummaryOutputDir))
		sb.WriteString(fmt.Sprintf("📊 性能报告: [summary/performance_report.md](%s/performance_report.md)\n\n", report.SummaryOutputDir))
	} else {
		sb.WriteString("⚠️ 会议纪要测试未完成或跳过\n\n")
	}

	// Write MD report
	reportPath := filepath.Join(r.outputDir, "full_test_report.md")
	if err := os.WriteFile(reportPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}

	// Write HTML report
	htmlPath := filepath.Join(r.outputDir, "full_test_report.html")
	if err := r.generateHTMLReport(report, htmlPath); err != nil {
		fmt.Printf("Warning: failed to generate HTML report: %v\n", err)
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📋 Phase 5: Final Report Generated")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("📄 Markdown: %s\n", reportPath)
	fmt.Printf("📄 HTML:     %s\n", htmlPath)

	return nil
}

func (r *Runner) writePhaseTable(sb *strings.Builder, phase *PhaseResult) {
	sb.WriteString("| 测试项 | 状态 | 延迟 (ms) | Tokens |\n")
	sb.WriteString("|--------|------|-----------|--------|\n")

	for _, res := range phase.Results {
		status := "✅"
		if !res.Success {
			status = "❌"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %.2f | %d |\n", res.Name, status, res.LatencyMs, res.Tokens))
	}
	sb.WriteString(fmt.Sprintf("\n**平均延迟**: %.2f ms | **成功率**: %d/%d | **总 Tokens**: %d\n\n",
		phase.AvgLatencyMs, phase.Success, phase.Success+phase.Failure, phase.TotalTokens))
}

// SampleDataItem represents a sample data item for the template.
type SampleDataItem struct {
	Title   string
	Content string
}

// ChartData holds data for ECharts visualization.
type ChartData struct {
	TTFTDistribution    []float64 `json:"ttftDistribution"`
	LatencyDistribution []float64 `json:"latencyDistribution"`
	DecodeDistribution  []float64 `json:"decodeDistribution"`
	AllNames            []string  `json:"allNames"`
	FirstCallData       []float64 `json:"firstCallData"`
	ConcurrentData      []float64 `json:"concurrentData"`
	MultiTurnData       []float64 `json:"multiTurnData"`
	LongContext         *struct {
		Labels     []string  `json:"labels"`
		TTFT       []float64 `json:"ttft"`
		Latency    []float64 `json:"latency"`
		Throughput []float64 `json:"throughput"`
	} `json:"longContext,omitempty"`
	LongContextConcurrent *struct {
		Labels     []string  `json:"labels"`
		AvgTTFT    []float64 `json:"avgTTFT"`
		P95TTFT    []float64 `json:"p95TTFT"`
		AvgLatency []float64 `json:"avgLatency"`
		RPS        []float64 `json:"rps"`
	} `json:"longContextConcurrent,omitempty"`
	GraduatedConcurrency *struct {
		Labels     []string  `json:"labels"`
		AvgLatency []float64 `json:"avgLatency"`
		Throughput []float64 `json:"throughput"`
		RPS        []float64 `json:"rps"`
		AvgTTFT    []float64 `json:"avgTTFT"`
	} `json:"graduatedConcurrency,omitempty"`
}

func (r *Runner) generateHTMLReport(report *FullTestReport, outputPath string) error {
	// Parse template
	tmpl, err := template.New("fulltest_report").Parse(fullTestReportTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Calculate totals
	totalTests := 0
	totalSuccess := 0
	totalTokens := 0
	var latencySum float64
	latencyCount := 0
	var allLatencies []float64

	for _, phase := range []*PhaseResult{report.FirstCallResults, report.ConcurrentResults, report.MultiTurnResults} {
		if phase != nil {
			totalTests += len(phase.Results)
			totalSuccess += phase.Success
			totalTokens += phase.TotalTokens
			for _, res := range phase.Results {
				if res.Success {
					latencySum += res.LatencyMs
					latencyCount++
					allLatencies = append(allLatencies, res.LatencyMs)
				}
			}
		}
	}

	avgLatency := 0.0
	if latencyCount > 0 {
		avgLatency = latencySum / float64(latencyCount)
	}

	successRate := 0.0
	if totalTests > 0 {
		successRate = float64(totalSuccess) / float64(totalTests) * 100
	}

	// Get benchmark metrics
	var rps, throughput, avgTTFT float64
	var prefillSpeed, decodeSpeed, avgDecodeMs float64
	var p50Latency, p95Latency, p99Latency int64
	var p50TTFT, p95TTFT, p99TTFT int64
	var p50Decode, p95Decode, p99Decode int64
	var ttftDistribution, latencyDistribution, decodeDistribution []float64

	if report.BenchmarkReport != nil {
		rps = report.BenchmarkReport.RPS
		throughput = report.BenchmarkReport.TokenThroughput
		avgTTFT = report.BenchmarkReport.AvgTTFTMs
		p50Latency = report.BenchmarkReport.P50LatencyMs
		p95Latency = report.BenchmarkReport.P95LatencyMs
		p99Latency = report.BenchmarkReport.P99LatencyMs
		p50TTFT = report.BenchmarkReport.P50TTFTMs
		p95TTFT = report.BenchmarkReport.P95TTFTMs
		p99TTFT = report.BenchmarkReport.P99TTFTMs
		prefillSpeed = report.BenchmarkReport.PrefillSpeed
		decodeSpeed = report.BenchmarkReport.DecodeSpeed
		avgDecodeMs = report.BenchmarkReport.AvgDecodeMs
		p50Decode = report.BenchmarkReport.P50DecodeMs
		p95Decode = report.BenchmarkReport.P95DecodeMs
		p99Decode = report.BenchmarkReport.P99DecodeMs
		// Convert int64 slices to float64 for chart data
		for _, v := range report.BenchmarkReport.TTFTDistribution {
			ttftDistribution = append(ttftDistribution, float64(v))
		}
		for _, v := range report.BenchmarkReport.LatencyDistribution {
			latencyDistribution = append(latencyDistribution, float64(v))
		}
		for _, v := range report.BenchmarkReport.DecodeDistribution {
			decodeDistribution = append(decodeDistribution, float64(v))
		}
	}

	// Function call result
	fcSupported := false
	fcDetails := "未测试或不支持"
	if report.FunctionCallResult != nil {
		if report.FunctionCallResult.Supported {
			fcSupported = true
			fcDetails = fmt.Sprintf("函数: %s, 参数: %s, 延迟: %.2f ms",
				report.FunctionCallResult.FunctionName,
				report.FunctionCallResult.Arguments,
				report.FunctionCallResult.LatencyMs)
		} else if report.FunctionCallResult.Error != "" {
			fcDetails = report.FunctionCallResult.Error
		}
	}

	// Summary status
	summaryStatus := "⚠️ 跳过"
	summaryDetails := "未提供会议记录文件"
	if report.SummaryOutputDir != "" {
		summaryStatus = "✅ 完成"
		summaryDetails = fmt.Sprintf("详见 %s 目录", report.SummaryOutputDir)
	}

	// Sample data
	var sampleData []SampleDataItem
	if report.BenchmarkReport != nil {
		br := report.BenchmarkReport
		if br.FirstContentRaw != "" {
			escaped := strings.ReplaceAll(br.FirstContentRaw, "<", "&lt;")
			escaped = strings.ReplaceAll(escaped, ">", "&gt;")
			sampleData = append(sampleData, SampleDataItem{
				Title:   "首帧 (First Content)",
				Content: escaped,
			})
		}
		for i, frame := range br.MiddleFramesRaw {
			escaped := strings.ReplaceAll(frame, "<", "&lt;")
			escaped = strings.ReplaceAll(escaped, ">", "&gt;")
			sampleData = append(sampleData, SampleDataItem{
				Title:   fmt.Sprintf("过程帧 %d", i+1),
				Content: escaped,
			})
		}
		if br.FinalFrameRaw != "" {
			escaped := strings.ReplaceAll(br.FinalFrameRaw, "<", "&lt;")
			escaped = strings.ReplaceAll(escaped, ">", "&gt;")
			sampleData = append(sampleData, SampleDataItem{
				Title:   "尾帧 (Final Frame)",
				Content: escaped,
			})
		}
	}

	// Prepare chart data
	chartData := ChartData{
		TTFTDistribution:    ttftDistribution,
		LatencyDistribution: latencyDistribution,
		DecodeDistribution:  decodeDistribution,
	}

	// Prepare bar chart data with proper alignment
	maxLen := 0
	if report.FirstCallResults != nil && len(report.FirstCallResults.Results) > maxLen {
		maxLen = len(report.FirstCallResults.Results)
	}
	if report.ConcurrentResults != nil && len(report.ConcurrentResults.Results) > maxLen {
		maxLen = len(report.ConcurrentResults.Results)
	}
	if report.MultiTurnResults != nil && len(report.MultiTurnResults.Results) > maxLen {
		maxLen = len(report.MultiTurnResults.Results)
	}

	// Create individual series for each phase
	var allNames []string
	var firstCallData, concurrentData, multiTurnData []float64

	if report.FirstCallResults != nil {
		for _, res := range report.FirstCallResults.Results {
			allNames = append(allNames, res.Name)
			firstCallData = append(firstCallData, res.LatencyMs)
			concurrentData = append(concurrentData, 0)
			multiTurnData = append(multiTurnData, 0)
		}
	}
	if report.ConcurrentResults != nil {
		for _, res := range report.ConcurrentResults.Results {
			allNames = append(allNames, res.Name)
			firstCallData = append(firstCallData, 0)
			concurrentData = append(concurrentData, res.LatencyMs)
			multiTurnData = append(multiTurnData, 0)
		}
	}
	if report.MultiTurnResults != nil {
		for _, res := range report.MultiTurnResults.Results {
			allNames = append(allNames, res.Name)
			firstCallData = append(firstCallData, 0)
			concurrentData = append(concurrentData, 0)
			multiTurnData = append(multiTurnData, res.LatencyMs)
		}
	}

	chartData.AllNames = allNames
	chartData.FirstCallData = firstCallData
	chartData.ConcurrentData = concurrentData
	chartData.MultiTurnData = multiTurnData

	// Long context chart data
	if report.LongContextResult != nil && len(report.LongContextResult.Results) > 0 {
		chartData.LongContext = &struct {
			Labels     []string  `json:"labels"`
			TTFT       []float64 `json:"ttft"`
			Latency    []float64 `json:"latency"`
			Throughput []float64 `json:"throughput"`
		}{}
		for _, res := range report.LongContextResult.Results {
			chartData.LongContext.Labels = append(chartData.LongContext.Labels, fmt.Sprintf("%dK", res.ContextLength/1000))
			if res.Success {
				chartData.LongContext.TTFT = append(chartData.LongContext.TTFT, res.TTFTMs)
				chartData.LongContext.Latency = append(chartData.LongContext.Latency, res.LatencyMs)
				chartData.LongContext.Throughput = append(chartData.LongContext.Throughput, res.Throughput)
			} else {
				chartData.LongContext.TTFT = append(chartData.LongContext.TTFT, 0)
				chartData.LongContext.Latency = append(chartData.LongContext.Latency, 0)
				chartData.LongContext.Throughput = append(chartData.LongContext.Throughput, 0)
			}
		}
	}

	// Graduated concurrency chart data
	if report.GraduatedConcurrency != nil && len(report.GraduatedConcurrency.Levels) > 0 {
		chartData.GraduatedConcurrency = &struct {
			Labels     []string  `json:"labels"`
			AvgLatency []float64 `json:"avgLatency"`
			Throughput []float64 `json:"throughput"`
			RPS        []float64 `json:"rps"`
			AvgTTFT    []float64 `json:"avgTTFT"`
		}{}
		for _, lv := range report.GraduatedConcurrency.Levels {
			chartData.GraduatedConcurrency.Labels = append(chartData.GraduatedConcurrency.Labels, fmt.Sprintf("C=%d", lv.Concurrency))
			chartData.GraduatedConcurrency.AvgLatency = append(chartData.GraduatedConcurrency.AvgLatency, lv.AvgLatencyMs)
			chartData.GraduatedConcurrency.Throughput = append(chartData.GraduatedConcurrency.Throughput, lv.Throughput)
			chartData.GraduatedConcurrency.RPS = append(chartData.GraduatedConcurrency.RPS, lv.RPS)
			chartData.GraduatedConcurrency.AvgTTFT = append(chartData.GraduatedConcurrency.AvgTTFT, lv.AvgTTFTMs)
		}
	}

	// Long context concurrent chart data
	if report.LongContextConcurrentResult != nil && len(report.LongContextConcurrentResult.Levels) > 0 {
		chartData.LongContextConcurrent = &struct {
			Labels     []string  `json:"labels"`
			AvgTTFT    []float64 `json:"avgTTFT"`
			P95TTFT    []float64 `json:"p95TTFT"`
			AvgLatency []float64 `json:"avgLatency"`
			RPS        []float64 `json:"rps"`
		}{}
		for _, lv := range report.LongContextConcurrentResult.Levels {
			label := fmt.Sprintf("%dK×C%d", lv.ContextLength/1000, lv.Concurrency)
			chartData.LongContextConcurrent.Labels = append(chartData.LongContextConcurrent.Labels, label)
			chartData.LongContextConcurrent.AvgTTFT = append(chartData.LongContextConcurrent.AvgTTFT, lv.AvgTTFTMs)
			chartData.LongContextConcurrent.P95TTFT = append(chartData.LongContextConcurrent.P95TTFT, lv.P95TTFTMs)
			chartData.LongContextConcurrent.AvgLatency = append(chartData.LongContextConcurrent.AvgLatency, lv.AvgLatencyMs)
			chartData.LongContextConcurrent.RPS = append(chartData.LongContextConcurrent.RPS, lv.RPS)
		}
	}

	chartDataJSON, _ := json.Marshal(chartData)

	// Summary content HTML
	summaryContentHTML := ""
	if report.SummaryContent != "" {
		escaped := strings.ReplaceAll(report.SummaryContent, "&", "&amp;")
		escaped = strings.ReplaceAll(escaped, "<", "&lt;")
		escaped = strings.ReplaceAll(escaped, ">", "&gt;")
		escaped = strings.ReplaceAll(escaped, "\"", "&quot;")
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		summaryContentHTML = escaped
	}

	// Summary metrics processing time
	var summaryProcessingTime, summaryAvgChunkTime float64
	if report.SummaryMetrics != nil {
		summaryProcessingTime = report.SummaryMetrics.TotalProcessingTime.Seconds()
		summaryAvgChunkTime = report.SummaryMetrics.AverageTimePerChunk.Seconds()
	}

	// Calculate phase totals
	firstCallTotal := 0
	if report.FirstCallResults != nil {
		firstCallTotal = report.FirstCallResults.Success + report.FirstCallResults.Failure
	}
	concurrentTotal := 0
	if report.ConcurrentResults != nil {
		concurrentTotal = report.ConcurrentResults.Success + report.ConcurrentResults.Failure
	}
	multiTurnTotal := 0
	if report.MultiTurnResults != nil {
		multiTurnTotal = report.MultiTurnResults.Success + report.MultiTurnResults.Failure
	}

	// Encode fonts to base64
	jetBrainsMonoBase64 := base64.StdEncoding.EncodeToString(jetBrainsMonoFont)
	plusJakartaSansBase64 := base64.StdEncoding.EncodeToString(plusJakartaSansFont)

	// Prepare template data
	data := map[string]interface{}{
		"Report":                report,
		"EChartsJS":             template.JS(echartsJS),
		"JetBrainsMonoBase64":   jetBrainsMonoBase64,
		"PlusJakartaSansBase64": plusJakartaSansBase64,
		"DurationSeconds":       report.TotalDuration.Seconds(),
		"TotalTests":            totalTests,
		"TotalSuccess":          totalSuccess,
		"TotalTokens":           totalTokens,
		"SuccessRate":           successRate,
		"AvgLatency":            avgLatency,
		"AvgTTFT":               avgTTFT,
		"P50Latency":            p50Latency,
		"P95Latency":            p95Latency,
		"P99Latency":            p99Latency,
		"P50TTFT":               p50TTFT,
		"P95TTFT":               p95TTFT,
		"P99TTFT":               p99TTFT,
		"RPS":                   rps,
		"Throughput":            throughput,
		"PrefillSpeed":          prefillSpeed,
		"DecodeSpeed":           decodeSpeed,
		"AvgDecodeMs":           avgDecodeMs,
		"P50Decode":             p50Decode,
		"P95Decode":             p95Decode,
		"P99Decode":             p99Decode,
		"FCSupported":           fcSupported,
		"FCDetails":             fcDetails,
		"SummaryStatus":         summaryStatus,
		"SummaryDetails":        summaryDetails,
		"SampleData":            sampleData,
		"ChartDataJSON":         template.JS(chartDataJSON),
		"SummaryContentHTML":    template.HTML(summaryContentHTML),
		"SummaryProcessingTime": summaryProcessingTime,
		"SummaryAvgChunkTime":   summaryAvgChunkTime,
		"FirstCallTotal":        firstCallTotal,
		"ConcurrentTotal":       concurrentTotal,
		"MultiTurnTotal":        multiTurnTotal,
		"GeneratedAt":           time.Now().Format("2006-01-02 15:04:05"),
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return os.WriteFile(outputPath, buf.Bytes(), 0644)
}
