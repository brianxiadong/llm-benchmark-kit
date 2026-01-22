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

	// Phase 3: Summary
	SummaryMetrics *summarizer.SummaryMetrics `json:"summary_metrics,omitempty"`

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

	// ===== Phase 3: Meeting Summary Test =====
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📝 Phase 3: Meeting Summary Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	if r.transcriptFile != "" {
		summaryDir := filepath.Join(r.outputDir, "summary")
		_, err := r.runSummary(summaryDir)
		if err != nil {
			fmt.Printf("⚠️  Summary test failed: %v\n", err)
		} else {
			report.SummaryOutputDir = summaryDir
			fmt.Println("✅ Phase 3 Complete!")
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.cfg.TimeoutSec)*time.Second)
	defer cancel()

	// Create workload input using the proper type
	input := workload.NewChatWorkload(name, []workload.ChatMessage{
		{Role: "user", Content: prompt},
	}, r.cfg.MaxTokens)

	// Use the provider's StreamChat
	events, err := r.p.StreamChat(ctx, r.cfg, input)

	if err != nil {
		return TestResult{
			Name:      name,
			Success:   false,
			LatencyMs: float64(time.Since(start).Milliseconds()),
			Error:     err.Error(),
		}
	}

	var tokens int
	for event := range events {
		if event.Type == provider.EventUsage && event.Usage != nil {
			tokens = event.Usage.CompletionTokens
		}
		if event.Type == provider.EventError {
			return TestResult{
				Name:      name,
				Success:   false,
				LatencyMs: float64(time.Since(start).Milliseconds()),
				Error:     event.Err.Error(),
			}
		}
	}

	return TestResult{
		Name:      name,
		Success:   true,
		LatencyMs: float64(time.Since(start).Milliseconds()),
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

	req, err := http.NewRequest("POST", r.cfg.URL, bytes.NewBuffer(jsonBody))
	if err != nil {
		result.Error = err.Error()
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
		return result
	}
	defer resp.Body.Close()

	result.LatencyMs = float64(time.Since(start).Milliseconds())

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
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

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &respData); err != nil {
		result.Error = fmt.Sprintf("Failed to parse response: %v", err)
		return result
	}

	// Check if function call is supported
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
	} else {
		result.Supported = false
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

// ========== Phase 3: Summary Test ==========

func (r *Runner) runSummary(outputDir string) (*summarizer.SummaryMetrics, error) {
	if _, err := os.Stat(r.transcriptFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("transcript file not found: %s", r.transcriptFile)
	}

	fmt.Printf("   Transcript:   %s\n", r.transcriptFile)
	fmt.Printf("   Chunk Size:   8000 chars\n")
	fmt.Println()

	meetingTime := time.Now().Format("2006-01-02 15:04")
	sum := summarizer.NewSummarizer(r.cfg, 8000, meetingTime)

	_, err := sum.Run(r.transcriptFile, outputDir)
	if err != nil {
		return nil, err
	}

	return nil, nil
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

	// Phase 3: Summary Results
	sb.WriteString("## Phase 3: 会议纪要测试\n\n")
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
	fmt.Println("📋 Phase 4: Final Report Generated")
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

	// Summary status
	summaryStatus := "⚠️ 跳过"
	if report.SummaryOutputDir != "" {
		summaryStatus = "✅ 完成"
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
        .chart-container { background: #fff; border-radius: 12px; padding: 15px; }
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
                <div class="stat-value">%.0f</div>
                <div class="stat-label">平均延迟 (ms)</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">%d</div>
                <div class="stat-label">总 Tokens</div>
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
        </div>

        <div class="section">
            <h2>🔧 Phase 2: Function Call 测试</h2>
            <div class="fc-result">
                <div class="fc-status">%s</div>
                <div class="fc-details">%s</div>
            </div>
        </div>

        <div class="section">
            <h2>📝 Phase 3: 会议纪要测试</h2>
            <div class="fc-result">
                <div class="fc-status">%s</div>
                <div class="fc-details">详见 summary/ 目录</div>
            </div>
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
        };
        
        Plotly.newPlot('latencyChart', [trace1, trace2, trace3], layout);
    </script>
</body>
</html>`,
		report.ModelName,
		report.ModelName, report.APIURL, report.TotalDuration.Seconds(),
		totalSuccess, totalTests, avgLatency, totalTokens, fcSupported,
		generatePhaseHTML(report.FirstCallResults, "1.1 冷启动测试 (First Call)"),
		generatePhaseHTML(report.ConcurrentResults, "1.2 并发测试 (Concurrent)"),
		generatePhaseHTML(report.MultiTurnResults, "1.3 多轮对话测试 (Multi-turn)"),
		fcSupported, fcDetails, summaryStatus,
		time.Now().Format("2006-01-02 15:04:05"),
		string(firstCallNamesJSON), string(firstCallLatenciesJSON),
		string(concurrentNamesJSON), string(concurrentLatenciesJSON),
		string(multiTurnNamesJSON), string(multiTurnLatenciesJSON))

	return os.WriteFile(outputPath, []byte(html), 0644)
}
