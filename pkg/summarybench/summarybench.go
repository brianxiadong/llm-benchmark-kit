// Package summarybench provides concurrent meeting summary benchmarking functionality.
package summarybench

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/embedded"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/workload"
)

// RequestResult holds the result of a single summary request.
type RequestResult struct {
	ID               int       `json:"id"`
	Success          bool      `json:"success"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	TTFTMs           float64   `json:"ttft_ms"`
	LatencyMs        float64   `json:"latency_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	TokensPerSecond  float64   `json:"tokens_per_second"`
	Error            string    `json:"error,omitempty"`
}

// BenchmarkStats holds aggregated statistics.
type BenchmarkStats struct {
	TotalRequests    int     `json:"total_requests"`
	SuccessCount     int     `json:"success_count"`
	FailureCount     int     `json:"failure_count"`
	SuccessRate      float64 `json:"success_rate"`
	TotalDurationSec float64 `json:"total_duration_sec"`
	RPS              float64 `json:"requests_per_second"`

	LatencyAvg float64 `json:"latency_avg_ms"`
	LatencyP50 float64 `json:"latency_p50_ms"`
	LatencyP95 float64 `json:"latency_p95_ms"`
	LatencyP99 float64 `json:"latency_p99_ms"`
	LatencyMin float64 `json:"latency_min_ms"`
	LatencyMax float64 `json:"latency_max_ms"`

	ThroughputAvg float64 `json:"throughput_avg"`
	ThroughputP50 float64 `json:"throughput_p50"`
	ThroughputP95 float64 `json:"throughput_p95"`
	ThroughputP99 float64 `json:"throughput_p99"`
	ThroughputMin float64 `json:"throughput_min"`
	ThroughputMax float64 `json:"throughput_max"`

	TotalPromptTokens     int     `json:"total_prompt_tokens"`
	TotalCompletionTokens int     `json:"total_completion_tokens"`
	AvgPromptTokens       float64 `json:"avg_prompt_tokens"`
	AvgCompletionTokens   float64 `json:"avg_completion_tokens"`

	OverallTokensPerSecond float64 `json:"overall_tokens_per_second"`
}

// BenchmarkReport holds the complete benchmark report.
type BenchmarkReport struct {
	ModelName     string          `json:"model_name"`
	APIURL        string          `json:"api_url"`
	Concurrency   int             `json:"concurrency"`
	TotalRequests int             `json:"total_requests"`
	ChunkSize     int             `json:"chunk_size"`
	StartTime     time.Time       `json:"start_time"`
	EndTime       time.Time       `json:"end_time"`
	Stats         BenchmarkStats  `json:"stats"`
	Results       []RequestResult `json:"results"`
}

// ChatRequest represents the OpenAI chat completion request.
type ChatRequest struct {
	Model     string                 `json:"model"`
	Messages  []workload.ChatMessage `json:"messages"`
	MaxTokens int                    `json:"max_tokens,omitempty"`
	Stream    bool                   `json:"stream"`
}

// ChatResponse represents the OpenAI chat completion response.
type ChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Benchmark handles concurrent summary benchmarking.
type Benchmark struct {
	cfg         *config.GlobalConfig
	concurrency int
	requests    int
	chunkSize   int
	transcript  string
}

// NewBenchmark creates a new summary benchmark runner.
func NewBenchmark(cfg *config.GlobalConfig, concurrency, requests, chunkSize int) *Benchmark {
	return &Benchmark{
		cfg:         cfg,
		concurrency: concurrency,
		requests:    requests,
		chunkSize:   chunkSize,
	}
}

// Run executes the concurrent summary benchmark.
func (b *Benchmark) Run(transcriptFile, outputDir string) (*BenchmarkReport, error) {
	var content []byte
	var err error

	if transcriptFile == "" {
		content = embedded.GetTranscriptSample()
		if len(content) == 0 {
			return nil, fmt.Errorf("no embedded transcript available")
		}
		fmt.Println("   Using embedded transcript sample")
	} else {
		content, err = os.ReadFile(transcriptFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read transcript: %w", err)
		}
	}
	b.transcript = string(content)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	report := &BenchmarkReport{
		ModelName:     b.cfg.ModelName,
		APIURL:        b.cfg.URL,
		Concurrency:   b.concurrency,
		TotalRequests: b.requests,
		ChunkSize:     b.chunkSize,
		StartTime:     time.Now(),
		Results:       make([]RequestResult, 0, b.requests),
	}

	workCh := make(chan int, b.requests)
	resultCh := make(chan RequestResult, b.requests)

	for i := 0; i < b.requests; i++ {
		workCh <- i
	}
	close(workCh)

	var completed int64
	var wg sync.WaitGroup

	fmt.Printf("\n")
	fmt.Printf("   ┌─────────────────────────────────────────────────────────────────────────┐\n")
	fmt.Printf("   │  会议纪要并发压测                                                        │\n")
	fmt.Printf("   ├─────────────────────────────────────────────────────────────────────────┤\n")
	fmt.Printf("   │  并发数: %-5d  总请求数: %-5d  分块大小: %-6d                        │\n", b.concurrency, b.requests, b.chunkSize)
	fmt.Printf("   └─────────────────────────────────────────────────────────────────────────┘\n")
	fmt.Printf("\n")

	for i := 0; i < b.concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client := b.createClient()

			for reqID := range workCh {
				result := b.executeRequest(client, reqID)
				resultCh <- result

				current := atomic.AddInt64(&completed, 1)
				status := "✅"
				if !result.Success {
					status = "❌"
				}
				fmt.Printf("   %s [%3d/%3d] Worker-%02d | Latency: %8.0fms | Tokens: %5d | %.1f tok/s\n",
					status, current, b.requests, workerID,
					result.LatencyMs, result.CompletionTokens, result.TokensPerSecond)
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for result := range resultCh {
		report.Results = append(report.Results, result)
	}

	report.EndTime = time.Now()

	sort.Slice(report.Results, func(i, j int) bool {
		return report.Results[i].ID < report.Results[j].ID
	})

	report.Stats = b.calculateStats(report.Results, report.EndTime.Sub(report.StartTime))

	if err := b.saveReport(report, outputDir); err != nil {
		return nil, fmt.Errorf("failed to save report: %w", err)
	}

	b.printSummary(report)

	return report, nil
}

func (b *Benchmark) executeRequest(client *http.Client, reqID int) RequestResult {
	result := RequestResult{
		ID:        reqID,
		StartTime: time.Now(),
	}

	chunk := b.getChunk(reqID)

	sysPrompt := `你是一个专业的会议记录分析助手。请对以下会议内容进行总结，包括：
1. 主要议题
2. 关键决定
3. 行动项
4. 重要发言人观点`

	userPrompt := fmt.Sprintf("请总结以下会议内容：\n\n%s", chunk)

	messages := []workload.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userPrompt},
	}

	reqBody := ChatRequest{
		Model:     b.cfg.ModelName,
		Messages:  messages,
		MaxTokens: 2048,
		Stream:    false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		result.Error = fmt.Sprintf("marshal error: %v", err)
		result.EndTime = time.Now()
		result.LatencyMs = float64(result.EndTime.Sub(result.StartTime).Milliseconds())
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(b.cfg.TimeoutSec)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", b.cfg.URL, bytes.NewReader(jsonBody))
	if err != nil {
		result.Error = fmt.Sprintf("request error: %v", err)
		result.EndTime = time.Now()
		result.LatencyMs = float64(result.EndTime.Sub(result.StartTime).Milliseconds())
		return result
	}

	req.Header.Set("Content-Type", "application/json")
	if b.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+b.cfg.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("request failed: %v", err)
		result.EndTime = time.Now()
		result.LatencyMs = float64(result.EndTime.Sub(result.StartTime).Milliseconds())
		return result
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("read error: %v", err)
		result.EndTime = time.Now()
		result.LatencyMs = float64(result.EndTime.Sub(result.StartTime).Milliseconds())
		return result
	}

	result.EndTime = time.Now()
	result.LatencyMs = float64(result.EndTime.Sub(result.StartTime).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		return result
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		result.Error = fmt.Sprintf("parse error: %v", err)
		return result
	}

	if len(chatResp.Choices) == 0 {
		result.Error = "no response choices"
		return result
	}

	result.Success = true
	result.PromptTokens = chatResp.Usage.PromptTokens
	result.CompletionTokens = chatResp.Usage.CompletionTokens
	result.TotalTokens = chatResp.Usage.TotalTokens

	if result.LatencyMs > 0 {
		result.TokensPerSecond = float64(result.CompletionTokens) / (result.LatencyMs / 1000.0)
	}

	return result
}

// getChunk returns a chunk with randomization to avoid cache hits.
// It uses random offset and adds a unique request ID prefix.
func (b *Benchmark) getChunk(reqID int) string {
	// Add unique prefix to prevent cache hits
	uniquePrefix := fmt.Sprintf("[请求ID: %d, 时间戳: %d]\n\n", reqID, time.Now().UnixNano())
	
	if len(b.transcript) <= b.chunkSize {
		return uniquePrefix + b.transcript
	}
	
	// Use random offset to get different parts of the transcript
	maxOffset := len(b.transcript) - b.chunkSize
	if maxOffset <= 0 {
		return uniquePrefix + b.transcript
	}
	
	offset := rand.Intn(maxOffset)
	return uniquePrefix + b.transcript[offset:offset+b.chunkSize]
}

func (b *Benchmark) calculateStats(results []RequestResult, totalDuration time.Duration) BenchmarkStats {
	stats := BenchmarkStats{
		TotalRequests:    len(results),
		TotalDurationSec: totalDuration.Seconds(),
	}

	var latencies []float64
	var throughputs []float64

	for _, r := range results {
		if r.Success {
			stats.SuccessCount++
			latencies = append(latencies, r.LatencyMs)
			throughputs = append(throughputs, r.TokensPerSecond)
			stats.TotalPromptTokens += r.PromptTokens
			stats.TotalCompletionTokens += r.CompletionTokens
		} else {
			stats.FailureCount++
		}
	}

	if stats.TotalRequests > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalRequests) * 100
		stats.RPS = float64(stats.TotalRequests) / totalDuration.Seconds()
	}

	if len(latencies) > 0 {
		sort.Float64s(latencies)
		stats.LatencyMin = latencies[0]
		stats.LatencyMax = latencies[len(latencies)-1]
		stats.LatencyAvg = avg(latencies)
		stats.LatencyP50 = percentile(latencies, 50)
		stats.LatencyP95 = percentile(latencies, 95)
		stats.LatencyP99 = percentile(latencies, 99)
	}

	if len(throughputs) > 0 {
		sort.Float64s(throughputs)
		stats.ThroughputMin = throughputs[0]
		stats.ThroughputMax = throughputs[len(throughputs)-1]
		stats.ThroughputAvg = avg(throughputs)
		stats.ThroughputP50 = percentile(throughputs, 50)
		stats.ThroughputP95 = percentile(throughputs, 95)
		stats.ThroughputP99 = percentile(throughputs, 99)
	}

	if stats.SuccessCount > 0 {
		stats.AvgPromptTokens = float64(stats.TotalPromptTokens) / float64(stats.SuccessCount)
		stats.AvgCompletionTokens = float64(stats.TotalCompletionTokens) / float64(stats.SuccessCount)
	}

	if totalDuration.Seconds() > 0 {
		stats.OverallTokensPerSecond = float64(stats.TotalCompletionTokens) / totalDuration.Seconds()
	}

	return stats
}

func avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))
	if lower == upper {
		return sorted[lower]
	}
	return sorted[lower] + (sorted[upper]-sorted[lower])*(index-float64(lower))
}

func (b *Benchmark) createClient() *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	if b.cfg.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(b.cfg.TimeoutSec) * time.Second,
	}
}

func (b *Benchmark) saveReport(report *BenchmarkReport, outputDir string) error {
	jsonPath := filepath.Join(outputDir, "summary_bench_report.json")
	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return err
	}

	mdPath := filepath.Join(outputDir, "summary_bench_report.md")
	md := b.generateMarkdown(report)
	if err := os.WriteFile(mdPath, []byte(md), 0644); err != nil {
		return err
	}

	fmt.Printf("\n   📄 Reports saved:\n")
	fmt.Printf("      - %s\n", jsonPath)
	fmt.Printf("      - %s\n", mdPath)

	return nil
}

func (b *Benchmark) generateMarkdown(report *BenchmarkReport) string {
	s := report.Stats
	md := fmt.Sprintf(`# 会议纪要并发压测报告

## 测试配置

| 配置项 | 值 |
|--------|-----|
| 模型 | %s |
| API URL | %s |
| 并发数 | %d |
| 总请求数 | %d |
| 分块大小 | %d 字符 |
| 测试时间 | %s |
| 总耗时 | %.2f 秒 |

## 性能指标

### 请求统计

| 指标 | 值 |
|------|-----|
| 成功请求 | %d |
| 失败请求 | %d |
| 成功率 | %.1f%% |
| RPS | %.2f |

### 延迟统计 (ms)

| 指标 | 值 |
|------|-----|
| 平均 | %.0f |
| P50 | %.0f |
| P95 | %.0f |
| P99 | %.0f |
| 最小 | %.0f |
| 最大 | %.0f |

### 吞吐量统计 (tokens/s)

| 指标 | 值 |
|------|-----|
| 平均 | %.1f |
| P50 | %.1f |
| P95 | %.1f |
| P99 | %.1f |
| 最小 | %.1f |
| 最大 | %.1f |

### Token 统计

| 指标 | 值 |
|------|-----|
| 总输入 Tokens | %d |
| 总输出 Tokens | %d |
| 平均输入 Tokens | %.0f |
| 平均输出 Tokens | %.0f |
| **整体吞吐 (tokens/s)** | **%.1f** |

## 详细结果

| ID | 状态 | 延迟(ms) | 输入Tokens | 输出Tokens | 吞吐(tok/s) |
|----|------|----------|------------|------------|-------------|
`,
		report.ModelName,
		report.APIURL,
		report.Concurrency,
		report.TotalRequests,
		report.ChunkSize,
		report.StartTime.Format("2006-01-02 15:04:05"),
		s.TotalDurationSec,
		s.SuccessCount,
		s.FailureCount,
		s.SuccessRate,
		s.RPS,
		s.LatencyAvg,
		s.LatencyP50,
		s.LatencyP95,
		s.LatencyP99,
		s.LatencyMin,
		s.LatencyMax,
		s.ThroughputAvg,
		s.ThroughputP50,
		s.ThroughputP95,
		s.ThroughputP99,
		s.ThroughputMin,
		s.ThroughputMax,
		s.TotalPromptTokens,
		s.TotalCompletionTokens,
		s.AvgPromptTokens,
		s.AvgCompletionTokens,
		s.OverallTokensPerSecond,
	)

	for _, r := range report.Results {
		status := "✅"
		if !r.Success {
			status = "❌"
		}
		md += fmt.Sprintf("| %d | %s | %.0f | %d | %d | %.1f |\n",
			r.ID, status, r.LatencyMs, r.PromptTokens, r.CompletionTokens, r.TokensPerSecond)
	}

	return md
}

func (b *Benchmark) printSummary(report *BenchmarkReport) {
	s := report.Stats

	fmt.Printf("\n")
	fmt.Printf("   ┌─────────────────────────────────────────────────────────────────────────┐\n")
	fmt.Printf("   │  📊 压测结果汇总                                                         │\n")
	fmt.Printf("   ├─────────────────────────────────────────────────────────────────────────┤\n")
	fmt.Printf("   │  %-20s │ %-48s │\n", "成功率", fmt.Sprintf("%.1f%% (%d/%d)", s.SuccessRate, s.SuccessCount, s.TotalRequests))
	fmt.Printf("   │  %-20s │ %-48s │\n", "总耗时", fmt.Sprintf("%.2f 秒", s.TotalDurationSec))
	fmt.Printf("   │  %-20s │ %-48s │\n", "RPS", fmt.Sprintf("%.2f req/s", s.RPS))
	fmt.Printf("   ├─────────────────────────────────────────────────────────────────────────┤\n")
	fmt.Printf("   │  延迟 (ms)           │ Avg: %-8.0f P50: %-8.0f P95: %-8.0f P99: %-6.0f │\n",
		s.LatencyAvg, s.LatencyP50, s.LatencyP95, s.LatencyP99)
	fmt.Printf("   │  吞吐 (tok/s)        │ Avg: %-8.1f P50: %-8.1f P95: %-8.1f P99: %-6.1f │\n",
		s.ThroughputAvg, s.ThroughputP50, s.ThroughputP95, s.ThroughputP99)
	fmt.Printf("   ├─────────────────────────────────────────────────────────────────────────┤\n")
	fmt.Printf("   │  总输出 Tokens: %-10d      整体吞吐: %-10.1f tokens/s           │\n",
		s.TotalCompletionTokens, s.OverallTokensPerSecond)
	fmt.Printf("   └─────────────────────────────────────────────────────────────────────────┘\n")
}
