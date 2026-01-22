// Package fulltest provides a complete test runner that executes
// performance benchmark, function call test, and meeting summary tests.
package fulltest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	MaxSupported  int                     `json:"max_supported"`   // Maximum supported context length
	AvgTTFTMs     float64                 `json:"avg_ttft_ms"`
	AvgLatencyMs  float64                 `json:"avg_latency_ms"`
	AvgThroughput float64                 `json:"avg_throughput"`
}

// FullTestReport contains the combined results from all test phases.
type FullTestReport struct {
	ModelName     string        `json:"model_name"`
	APIURL        string        `json:"api_url"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	TotalDuration time.Duration `json:"total_duration"`

	// Phase 1: Performance
	FirstCallResults  *PhaseResult            `json:"first_call_results"`
	ConcurrentResults *PhaseResult            `json:"concurrent_results"`
	MultiTurnResults  *PhaseResult            `json:"multi_turn_results"`
	BenchmarkReport   *result.BenchmarkReport `json:"benchmark_report,omitempty"`

	// Phase 2: Function Call
	FunctionCallResult *FunctionCallResult `json:"function_call_result,omitempty"`

	// Phase 3: Long Context Test
	LongContextResult *LongContextResult `json:"long_context_result,omitempty"`

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
	originalMaxTokens := r.cfg.MaxTokens
	if r.cfg.MaxTokens > 512 || r.cfg.MaxTokens == 0 {
		r.cfg.MaxTokens = 512
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
		if event.Type == provider.EventContent {
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
		if event.Type == provider.EventContent && !gotFirstToken {
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

	// Calculate throughput
	if result.LatencyMs > 0 {
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

func (r *Runner) generateHTMLReport(report *FullTestReport, outputPath string) error {
	// Prepare data for charts
	var firstCallLatencies, concurrentLatencies, multiTurnLatencies []float64
	var firstCallNames, concurrentNames, multiTurnNames []string

	if report.FirstCallResults != nil {
		for _, res := range report.FirstCallResults.Results {
			firstCallNames = append(firstCallNames, res.Name)
			firstCallLatencies = append(firstCallLatencies, res.LatencyMs)
		}
	}
	if report.ConcurrentResults != nil {
		for _, res := range report.ConcurrentResults.Results {
			concurrentNames = append(concurrentNames, res.Name)
			concurrentLatencies = append(concurrentLatencies, res.LatencyMs)
		}
	}
	if report.MultiTurnResults != nil {
		for _, res := range report.MultiTurnResults.Results {
			multiTurnNames = append(multiTurnNames, res.Name)
			multiTurnLatencies = append(multiTurnLatencies, res.LatencyMs)
		}
	}

	// JSON encode arrays for JavaScript
	firstCallNamesJSON, _ := json.Marshal(firstCallNames)
	firstCallLatenciesJSON, _ := json.Marshal(firstCallLatencies)
	concurrentNamesJSON, _ := json.Marshal(concurrentNames)
	concurrentLatenciesJSON, _ := json.Marshal(concurrentLatencies)
	multiTurnNamesJSON, _ := json.Marshal(multiTurnNames)
	multiTurnLatenciesJSON, _ := json.Marshal(multiTurnLatencies)

	// Function call result
	fcSupported := "❌ 不支持"
	fcDetails := ""
	if report.FunctionCallResult != nil {
		if report.FunctionCallResult.Supported {
			fcSupported = "✅ 支持"
			fcDetails = fmt.Sprintf("函数: %s, 参数: %s, 延迟: %.2f ms",
				report.FunctionCallResult.FunctionName,
				report.FunctionCallResult.Arguments,
				report.FunctionCallResult.LatencyMs)
		} else if report.FunctionCallResult.Error != "" {
			fcDetails = report.FunctionCallResult.Error
		}
	}

	// Calculate totals
	totalTests := 0
	totalSuccess := 0
	totalTokens := 0
	var avgLatency float64
	latencySum := 0.0
	latencyCount := 0

	for _, phase := range []*PhaseResult{report.FirstCallResults, report.ConcurrentResults, report.MultiTurnResults} {
		if phase != nil {
			totalTests += len(phase.Results)
			totalSuccess += phase.Success
			totalTokens += phase.TotalTokens
			for _, res := range phase.Results {
				if res.Success {
					latencySum += res.LatencyMs
					latencyCount++
				}
			}
		}
	}
	if latencyCount > 0 {
		avgLatency = latencySum / float64(latencyCount)
	}

	// Calculate success rate
	successRate := 0.0
	if totalTests > 0 {
		successRate = float64(totalSuccess) / float64(totalTests) * 100
	}

	// Get benchmark report metrics if available
	var rps, throughput, avgTTFT float64
	var p50Latency, p95Latency, p99Latency int64
	if report.BenchmarkReport != nil {
		rps = report.BenchmarkReport.RPS
		throughput = report.BenchmarkReport.TokenThroughput
		avgTTFT = report.BenchmarkReport.AvgTTFTMs
		p50Latency = report.BenchmarkReport.P50LatencyMs
		p95Latency = report.BenchmarkReport.P95LatencyMs
		p99Latency = report.BenchmarkReport.P99LatencyMs
	}

	// Summary status and details
	summaryStatus := "⚠️ 跳过"
	summaryDetails := "未提供会议记录文件"
	if report.SummaryOutputDir != "" {
		summaryStatus = "✅ 完成"
		summaryDetails = fmt.Sprintf("详见 %s 目录", report.SummaryOutputDir)
	}

	// Prepare summary metrics HTML
	summaryMetricsHTML := ""
	if report.SummaryMetrics != nil {
		m := report.SummaryMetrics
		summaryMetricsHTML = fmt.Sprintf(`
			<div class="phase-card">
				<h3>性能指标</h3>
				<table>
					<thead><tr><th>指标</th><th>值</th></tr></thead>
					<tbody>
						<tr><td>总分片数</td><td>%d</td></tr>
						<tr><td>总 Prompt Tokens</td><td>%d</td></tr>
						<tr><td>总 Completion Tokens</td><td>%d</td></tr>
						<tr><td>总 Tokens</td><td>%d</td></tr>
						<tr><td>总处理时间</td><td>%.2f 秒</td></tr>
						<tr><td>平均每分片耗时</td><td>%.2f 秒</td></tr>
						<tr><td>Token 生成速度</td><td>%.2f tokens/秒</td></tr>
					</tbody>
				</table>
			</div>`,
			m.TotalChunks,
			m.TotalPromptTokens,
			m.TotalCompletionTokens,
			m.TotalTokens,
			m.TotalProcessingTime.Seconds(),
			m.AverageTimePerChunk.Seconds(),
			m.TokensPerSecond)
	}

	// Prepare summary content preview (escape HTML)
	summaryContentPreview := ""
	if report.SummaryContent != "" {
		escapedContent := strings.ReplaceAll(report.SummaryContent, "&", "&amp;")
		escapedContent = strings.ReplaceAll(escapedContent, "<", "&lt;")
		escapedContent = strings.ReplaceAll(escapedContent, ">", "&gt;")
		escapedContent = strings.ReplaceAll(escapedContent, "\"", "&quot;")
		escapedContent = strings.ReplaceAll(escapedContent, "\n", "<br>")
		summaryContentPreview = fmt.Sprintf(`
			<div class="phase-card">
				<h3>会议纪要预览</h3>
				<details>
					<summary style="cursor: pointer; color: #00d2ff; margin-bottom: 10px;">点击展开/收起</summary>
					<div class="summary-content">%s</div>
				</details>
			</div>`, escapedContent)
	}

	// Prepare sample data HTML from BenchmarkReport
	sampleDataHTML := ""
	if report.BenchmarkReport != nil {
		br := report.BenchmarkReport
		var sampleBuilder strings.Builder
		sampleBuilder.WriteString(`<div class="phase-card"><h3>采样数据</h3>`)
		if br.FirstContentRaw != "" {
			escaped := strings.ReplaceAll(br.FirstContentRaw, "<", "&lt;")
			escaped = strings.ReplaceAll(escaped, ">", "&gt;")
			sampleBuilder.WriteString(fmt.Sprintf(`<div class="sample-item"><strong>首帧 (First Content):</strong><div class="sample-content">%s</div></div>`, escaped))
		}
		if len(br.MiddleFramesRaw) > 0 {
			for i, frame := range br.MiddleFramesRaw {
				escaped := strings.ReplaceAll(frame, "<", "&lt;")
				escaped = strings.ReplaceAll(escaped, ">", "&gt;")
				sampleBuilder.WriteString(fmt.Sprintf(`<div class="sample-item"><strong>过程帧 %d:</strong><div class="sample-content">%s</div></div>`, i+1, escaped))
			}
		}
		if br.FinalFrameRaw != "" {
			escaped := strings.ReplaceAll(br.FinalFrameRaw, "<", "&lt;")
			escaped = strings.ReplaceAll(escaped, ">", "&gt;")
			sampleBuilder.WriteString(fmt.Sprintf(`<div class="sample-item"><strong>尾帧 (Final Frame):</strong><div class="sample-content">%s</div></div>`, escaped))
		}
		sampleBuilder.WriteString(`</div>`)
		sampleDataHTML = sampleBuilder.String()
	}

	// Prepare long context test HTML
	longContextHTML := ""
	longContextChartData := ""
	if report.LongContextResult != nil && len(report.LongContextResult.Results) > 0 {
		lc := report.LongContextResult
		var lcBuilder strings.Builder
		lcBuilder.WriteString(`<div class="phase-card">
			<h3>测试结果详情</h3>
			<table>
				<thead><tr><th>上下文长度</th><th>估算Tokens</th><th>TTFT (ms)</th><th>Latency (ms)</th><th>吞吐 (tok/s)</th><th>状态</th></tr></thead>
				<tbody>`)
		for _, res := range lc.Results {
			status := "✅"
			statusClass := "success"
			if !res.Success {
				status = "❌"
				statusClass = "error"
			}
			lcBuilder.WriteString(fmt.Sprintf(`<tr>
				<td>%d 字符</td>
				<td>%d</td>
				<td>%.2f</td>
				<td>%.2f</td>
				<td>%.2f</td>
				<td class="%s">%s</td>
			</tr>`, res.ContextLength, res.InputTokens, res.TTFTMs, res.LatencyMs, res.Throughput, statusClass, status))
		}
		lcBuilder.WriteString(fmt.Sprintf(`</tbody></table>
			<div class="phase-summary">
				<span>最大支持: <strong>%d 字符</strong></span>
				<span>平均 TTFT: <strong>%.2f ms</strong></span>
				<span>平均吞吐: <strong>%.2f tok/s</strong></span>
			</div>
		</div>`, lc.MaxSupported, lc.AvgTTFTMs, lc.AvgThroughput))
		longContextHTML = lcBuilder.String()

		// Prepare chart data
		var contextLengths, ttftValues, latencyValues, throughputValues []string
		for _, res := range lc.Results {
			contextLengths = append(contextLengths, fmt.Sprintf("%dK", res.ContextLength/1000))
			if res.Success {
				ttftValues = append(ttftValues, fmt.Sprintf("%.2f", res.TTFTMs))
				latencyValues = append(latencyValues, fmt.Sprintf("%.2f", res.LatencyMs))
				throughputValues = append(throughputValues, fmt.Sprintf("%.2f", res.Throughput))
			} else {
				ttftValues = append(ttftValues, "null")
				latencyValues = append(latencyValues, "null")
				throughputValues = append(throughputValues, "null")
			}
		}
		longContextChartData = fmt.Sprintf(`
		<div class="chart-container" id="longContextChart"></div>
		<script>
			var lcTrace1 = {
				x: [%s],
				y: [%s],
				name: 'TTFT (ms)',
				type: 'scatter',
				mode: 'lines+markers',
				marker: { color: '#3498db', size: 10 },
				yaxis: 'y'
			};
			var lcTrace2 = {
				x: [%s],
				y: [%s],
				name: 'Latency (ms)',
				type: 'scatter',
				mode: 'lines+markers',
				marker: { color: '#e74c3c', size: 10 },
				yaxis: 'y'
			};
			var lcLayout = {
				title: '上下文长度 vs 延迟',
				xaxis: { title: '上下文长度' },
				yaxis: { title: '时间 (ms)' },
				paper_bgcolor: 'rgba(0,0,0,0)',
				plot_bgcolor: 'rgba(0,0,0,0)',
				height: 350,
				legend: { x: 0.02, y: 0.98 }
			};
			Plotly.newPlot('longContextChart', [lcTrace1, lcTrace2], lcLayout);
		</script>`,
			"'"+strings.Join(contextLengths, "','")+"'",
			strings.Join(ttftValues, ","),
			"'"+strings.Join(contextLengths, "','")+"'",
			strings.Join(latencyValues, ","))
	}

	// Generate phase result tables
	generatePhaseHTML := func(phase *PhaseResult, title string) string {
		if phase == nil {
			return ""
		}
		var html strings.Builder
		html.WriteString(fmt.Sprintf(`<div class="phase-card">
			<h3>%s</h3>
			<table>
				<thead><tr><th>测试项</th><th>状态</th><th>延迟 (ms)</th><th>Tokens</th></tr></thead>
				<tbody>`, title))
		for _, res := range phase.Results {
			status := "✅"
			statusClass := "success"
			if !res.Success {
				status = "❌"
				statusClass = "error"
			}
			html.WriteString(fmt.Sprintf(`<tr><td>%s</td><td class="%s">%s</td><td>%.2f</td><td>%d</td></tr>`,
				res.Name, statusClass, status, res.LatencyMs, res.Tokens))
		}
		html.WriteString(fmt.Sprintf(`</tbody></table>
			<div class="phase-summary">
				<span>平均延迟: <strong>%.2f ms</strong></span>
				<span>成功率: <strong>%d/%d</strong></span>
				<span>总 Tokens: <strong>%d</strong></span>
			</div>
		</div>`, phase.AvgLatencyMs, phase.Success, phase.Success+phase.Failure, phase.TotalTokens))
		return html.String()
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM Full Test Report - %s</title>
    <script src="https://cdn.plot.ly/plotly-2.27.0.min.js"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #1a1a2e 0%%, #16213e 100%%);
            min-height: 100vh;
            color: #e0e0e0;
            padding: 20px;
        }
        .container { max-width: 1400px; margin: 0 auto; }
        header {
            text-align: center;
            padding: 40px 20px;
            background: rgba(255,255,255,0.05);
            border-radius: 16px;
            margin-bottom: 30px;
            backdrop-filter: blur(10px);
        }
        h1 {
            font-size: 2.5em;
            background: linear-gradient(90deg, #00d2ff, #3a7bd5);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 10px;
        }
        .subtitle { color: #888; font-size: 1.1em; }
        .model-info { margin-top: 15px; color: #aaa; }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .stat-card {
            background: rgba(255,255,255,0.05);
            border-radius: 12px;
            padding: 20px;
            text-align: center;
            backdrop-filter: blur(10px);
        }
        .stat-value {
            font-size: 2.5em;
            font-weight: bold;
            background: linear-gradient(90deg, #00d2ff, #3a7bd5);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
        .stat-label { color: #888; margin-top: 5px; }
        .section {
            background: rgba(255,255,255,0.05);
            border-radius: 16px;
            padding: 25px;
            margin-bottom: 25px;
            backdrop-filter: blur(10px);
        }
        .section h2 {
            color: #3a7bd5;
            margin-bottom: 20px;
            padding-bottom: 10px;
            border-bottom: 1px solid rgba(255,255,255,0.1);
        }
        .phase-card {
            background: rgba(0,0,0,0.2);
            border-radius: 12px;
            padding: 20px;
            margin-bottom: 20px;
        }
        .phase-card h3 { color: #00d2ff; margin-bottom: 15px; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { padding: 10px 15px; text-align: left; border-bottom: 1px solid rgba(255,255,255,0.1); }
        th { background: rgba(58, 123, 213, 0.2); color: #fff; }
        tr:hover { background: rgba(255,255,255,0.05); }
        .success { color: #2ecc71; }
        .error { color: #e74c3c; }
        .phase-summary {
            margin-top: 15px;
            padding-top: 15px;
            border-top: 1px solid rgba(255,255,255,0.1);
            display: flex;
            gap: 30px;
            color: #aaa;
        }
        .phase-summary strong { color: #00d2ff; }
        .chart-container { background: #fff; border-radius: 12px; padding: 15px; height: 400px; }
        .fc-result {
            display: flex;
            align-items: center;
            gap: 20px;
            padding: 20px;
            background: rgba(0,0,0,0.2);
            border-radius: 12px;
        }
        .fc-status { font-size: 1.5em; }
        .fc-details { color: #aaa; }
        .footer { text-align: center; padding: 20px; color: #666; font-size: 0.9em; }
        .summary-content {
            background: rgba(0,0,0,0.3);
            padding: 15px;
            border-radius: 8px;
            max-height: 500px;
            overflow-y: auto;
            font-size: 0.9em;
            line-height: 1.6;
        }
        .sample-item {
            margin-bottom: 15px;
        }
        .sample-item strong {
            color: #00d2ff;
            display: block;
            margin-bottom: 5px;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>🚀 LLM Full Test Report</h1>
            <p class="subtitle">完整测试报告</p>
            <div class="model-info">
                <strong>模型:</strong> %s | 
                <strong>API:</strong> %s |
                <strong>耗时:</strong> %.2f 秒
            </div>
        </header>

        <div class="stats-grid">
            <div class="stat-card">
                <div class="stat-value">%d/%d</div>
                <div class="stat-label">成功/总测试</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%.1f%%</div>
                <div class="stat-label">成功率</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%.0f</div>
                <div class="stat-label">平均延迟 (ms)</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%.0f</div>
                <div class="stat-label">平均 TTFT (ms)</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%d</div>
                <div class="stat-label">P50 延迟 (ms)</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%d</div>
                <div class="stat-label">P95 延迟 (ms)</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%d</div>
                <div class="stat-label">P99 延迟 (ms)</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%d</div>
                <div class="stat-label">总 Tokens</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%.2f</div>
                <div class="stat-label">RPS</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%.1f</div>
                <div class="stat-label">Token/s</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%s</div>
                <div class="stat-label">Function Call</div>
            </div>
        </div>

        <div class="section">
            <h2>📊 Phase 1: 性能测试</h2>
            %s
            %s
            %s
            
            <h3 style="color: #00d2ff; margin: 20px 0;">延迟分布图</h3>
            <div class="chart-container" id="latencyChart"></div>
            %s
        </div>

        <div class="section">
            <h2>🔧 Phase 2: Function Call 测试</h2>
            <div class="fc-result">
                <div class="fc-status">%s</div>
                <div class="fc-details">%s</div>
            </div>
        </div>

        <div class="section">
            <h2>� Phase 3: 长上下文测试</h2>
            %s
            %s
        </div>

        <div class="section">
            <h2>📝 Phase 4: 会议纪要测试</h2>
            <div class="fc-result">
                <div class="fc-status">%s</div>
                <div class="fc-details">%s</div>
            </div>
            %s
            %s
        </div>

        <div class="footer">
            <p>Generated at %s by LLM Benchmark Kit</p>
        </div>
    </div>

    <script>
        // Latency comparison chart
        var trace1 = {
            x: %s,
            y: %s,
            name: 'First Call',
            type: 'bar',
            marker: { color: '#3498db' }
        };
        var trace2 = {
            x: %s,
            y: %s,
            name: 'Concurrent',
            type: 'bar',
            marker: { color: '#2ecc71' }
        };
        var trace3 = {
            x: %s,
            y: %s,
            name: 'Multi-turn',
            type: 'bar',
            marker: { color: '#9b59b6' }
        };
        
        var layout = {
            barmode: 'group',
            title: '各阶段延迟对比',
            xaxis: { title: '测试项' },
            yaxis: { title: '延迟 (ms)' },
            paper_bgcolor: 'rgba(0,0,0,0)',
            plot_bgcolor: 'rgba(0,0,0,0)',
            height: 380,
        };
        
        Plotly.newPlot('latencyChart', [trace1, trace2, trace3], layout);
    </script>
</body>
</html>`,
		report.ModelName,
		report.ModelName, report.APIURL, report.TotalDuration.Seconds(),
		totalSuccess, totalTests, successRate, avgLatency, avgTTFT,
		p50Latency, p95Latency, p99Latency,
		totalTokens, rps, throughput, fcSupported,
		generatePhaseHTML(report.FirstCallResults, "1.1 冷启动测试 (First Call)"),
		generatePhaseHTML(report.ConcurrentResults, "1.2 并发测试 (Concurrent)"),
		generatePhaseHTML(report.MultiTurnResults, "1.3 多轮对话测试 (Multi-turn)"),
		sampleDataHTML,
		fcSupported, fcDetails,
		longContextHTML, longContextChartData,
		summaryStatus, summaryDetails, summaryMetricsHTML, summaryContentPreview,
		time.Now().Format("2006-01-02 15:04:05"),
		string(firstCallNamesJSON), string(firstCallLatenciesJSON),
		string(concurrentNamesJSON), string(concurrentLatenciesJSON),
		string(multiTurnNamesJSON), string(multiTurnLatenciesJSON))

	return os.WriteFile(outputPath, []byte(html), 0644)
}
