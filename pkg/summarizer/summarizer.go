// Package summarizer provides meeting transcript summarization functionality.
package summarizer

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/workload"
)

// ChunkMetrics holds performance metrics for a single chunk processing.
type ChunkMetrics struct {
	ChunkIndex       int           `json:"chunk_index"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	TotalTokens      int           `json:"total_tokens"`
	ProcessingTime   time.Duration `json:"processing_time"`
	StartTime        time.Time     `json:"start_time"`
	EndTime          time.Time     `json:"end_time"`
	Overflowed       bool          `json:"overflowed"`               // Whether this chunk caused overflow
	OverflowError    string        `json:"overflow_error,omitempty"` // Error message if overflowed
}

// SummaryMetrics holds overall performance metrics for the summarization.
type SummaryMetrics struct {
	ModelName             string         `json:"model_name"`
	TotalChunks           int            `json:"total_chunks"`
	TotalPromptTokens     int            `json:"total_prompt_tokens"`
	TotalCompletionTokens int            `json:"total_completion_tokens"`
	TotalTokens           int            `json:"total_tokens"`
	TotalProcessingTime   time.Duration  `json:"total_processing_time"`
	AverageTimePerChunk   time.Duration  `json:"average_time_per_chunk"`
	TokensPerSecond       float64        `json:"tokens_per_second"`
	ChunkMetrics          []ChunkMetrics `json:"chunk_metrics"`
	StartTime             time.Time      `json:"start_time"`
	EndTime               time.Time      `json:"end_time"`
	OverflowDetected      bool           `json:"overflow_detected"`            // Whether overflow was detected
	OverflowAtChunk       int            `json:"overflow_at_chunk,omitempty"`  // Chunk number where overflow occurred
	OverflowAtTokens      int            `json:"overflow_at_tokens,omitempty"` // Total tokens when overflow occurred
}

// Summarizer handles meeting transcript summarization.
type Summarizer struct {
	cfg         *config.GlobalConfig
	chunker     *Chunker
	meetingTime string
}

// NewSummarizer creates a new Summarizer.
func NewSummarizer(cfg *config.GlobalConfig, chunkSize int, meetingTime string) *Summarizer {
	return &Summarizer{
		cfg:         cfg,
		chunker:     NewChunker(chunkSize),
		meetingTime: meetingTime,
	}
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
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
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

// Run processes the transcript file and generates a meeting summary.
func (s *Summarizer) Run(transcriptFile, outputDir string) (string, error) {
	content, _, err := s.RunWithMetrics(transcriptFile, outputDir)
	return content, err
}

// RunWithMetrics processes the transcript file and returns the summary along with performance metrics.
func (s *Summarizer) RunWithMetrics(transcriptFile, outputDir string) (string, *SummaryMetrics, error) {
	// Initialize metrics
	metrics := &SummaryMetrics{
		ModelName:    s.cfg.ModelName,
		StartTime:    time.Now(),
		ChunkMetrics: make([]ChunkMetrics, 0),
	}

	// Read the transcript file
	content, err := os.ReadFile(transcriptFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read transcript file: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create intermediate results directory
	intermediateDir := filepath.Join(outputDir, "intermediate")
	if err := os.MkdirAll(intermediateDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create intermediate directory: %w", err)
	}

	// Split into chunks
	chunks := s.chunker.Split(string(content))
	fmt.Printf("Transcript split into %d chunks\n", len(chunks))
	metrics.TotalChunks = len(chunks)

	// Process each chunk iteratively
	var currentSummary string
	for i, chunk := range chunks {
		fmt.Printf("Processing chunk %d/%d...\n", i+1, len(chunks))

		// Build the prompt
		sysPrompt, userPrompt := BuildPrompt(currentSummary, chunk, s.meetingTime)

		// Call the LLM and collect metrics
		response, chunkMetrics, err := s.chat(sysPrompt, userPrompt, i+1)
		if err != nil {
			// Check if it's an overflow error
			if strings.Contains(strings.ToLower(err.Error()), "maximum context length") ||
				strings.Contains(strings.ToLower(err.Error()), "context_length_exceeded") ||
				strings.Contains(strings.ToLower(err.Error()), "token limit") ||
				strings.Contains(strings.ToLower(err.Error()), "too many tokens") {
				// Mark overflow in metrics
				chunkMetrics.Overflowed = true
				chunkMetrics.OverflowError = err.Error()
				metrics.ChunkMetrics = append(metrics.ChunkMetrics, chunkMetrics)
				metrics.OverflowDetected = true
				metrics.OverflowAtChunk = i + 1
				metrics.OverflowAtTokens = metrics.TotalTokens

				fmt.Printf("  ⚠️  Token overflow detected at chunk %d/%d (total tokens so far: %d)\n",
					i+1, len(chunks), metrics.TotalTokens)
				fmt.Printf("  Error: %s\n", err.Error())

				// Use the current summary as the final result
				if currentSummary == "" {
					return "", metrics, fmt.Errorf("overflow on first chunk, cannot continue: %w", err)
				}

				fmt.Printf("  Using last successful summary as final result\n")
				break
			}
			// Other errors - fail immediately
			return "", metrics, fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}

		// Update metrics
		metrics.ChunkMetrics = append(metrics.ChunkMetrics, chunkMetrics)
		metrics.TotalPromptTokens += chunkMetrics.PromptTokens
		metrics.TotalCompletionTokens += chunkMetrics.CompletionTokens
		metrics.TotalTokens += chunkMetrics.TotalTokens
		metrics.TotalProcessingTime += chunkMetrics.ProcessingTime

		currentSummary = s.cleanResponse(response)

		// Save intermediate result
		intermediatePath := filepath.Join(intermediateDir, fmt.Sprintf("chunk_%02d.md", i+1))
		if err := os.WriteFile(intermediatePath, []byte(currentSummary), 0644); err != nil {
			fmt.Printf("  Warning: failed to save intermediate result: %v\n", err)
		}

		fmt.Printf("  ✓ Chunk %d/%d processed (tokens: %d, time: %.2fs), saved to %s\n",
			i+1, len(chunks), chunkMetrics.TotalTokens, chunkMetrics.ProcessingTime.Seconds(), intermediatePath)
	}

	// Finalize metrics
	metrics.EndTime = time.Now()
	if len(chunks) > 0 {
		metrics.AverageTimePerChunk = metrics.TotalProcessingTime / time.Duration(len(chunks))
	}
	if metrics.TotalProcessingTime.Seconds() > 0 {
		metrics.TokensPerSecond = float64(metrics.TotalCompletionTokens) / metrics.TotalProcessingTime.Seconds()
	}

	// Save final summary
	finalPath := filepath.Join(outputDir, "meeting_summary.md")
	if err := os.WriteFile(finalPath, []byte(currentSummary), 0644); err != nil {
		return "", metrics, fmt.Errorf("failed to save final summary: %w", err)
	}
	fmt.Printf("\n✅ Final summary saved to: %s\n", finalPath)

	// Generate and save performance report
	if err := s.savePerformanceReport(metrics, outputDir); err != nil {
		fmt.Printf("  Warning: failed to save performance report: %v\n", err)
	}

	return currentSummary, metrics, nil
}

// chat sends a non-streaming chat request to the LLM and returns content with metrics.
func (s *Summarizer) chat(sysPrompt, userPrompt string, chunkIndex int) (string, ChunkMetrics, error) {
	startTime := time.Now()
	metrics := ChunkMetrics{
		ChunkIndex: chunkIndex,
		StartTime:  startTime,
	}

	messages := []workload.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userPrompt},
	}

	reqBody := ChatRequest{
		Model:     s.cfg.ModelName,
		Messages:  messages,
		MaxTokens: 4096, // Allow longer responses for summaries
		Stream:    false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", metrics, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Verbose logging: request
	if s.cfg.Verbose {
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("[VERBOSE] LLM REQUEST")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("URL: %s\n", s.cfg.URL)
		fmt.Printf("Model: %s\n", s.cfg.ModelName)
		fmt.Println("\n[System Prompt]:")
		fmt.Println(sysPrompt)
		fmt.Println("\n[User Prompt]:")
		fmt.Println(userPrompt)
		fmt.Println(strings.Repeat("=", 80))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.TimeoutSec)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", s.cfg.URL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", metrics, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}

	client := s.createClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", metrics, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", metrics, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", metrics, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", metrics, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", metrics, fmt.Errorf("no response choices")
	}

	// Update metrics with timing and token usage
	metrics.EndTime = time.Now()
	metrics.ProcessingTime = metrics.EndTime.Sub(startTime)
	metrics.PromptTokens = chatResp.Usage.PromptTokens
	metrics.CompletionTokens = chatResp.Usage.CompletionTokens
	metrics.TotalTokens = chatResp.Usage.TotalTokens

	content := chatResp.Choices[0].Message.Content

	// Verbose logging: response
	if s.cfg.Verbose {
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("[VERBOSE] LLM RESPONSE")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("Status: %d\n", resp.StatusCode)
		fmt.Printf("Tokens: prompt=%d, completion=%d, total=%d\n",
			chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, chatResp.Usage.TotalTokens)
		fmt.Printf("Processing time: %.2fs\n", metrics.ProcessingTime.Seconds())
		fmt.Println("\n[Content]:")
		fmt.Println(content)
		fmt.Println(strings.Repeat("=", 80))
	}

	return content, metrics, nil
}

// savePerformanceReport generates and saves a performance report to the output directory.
func (s *Summarizer) savePerformanceReport(metrics *SummaryMetrics, outputDir string) error {
	// Generate markdown report
	var sb strings.Builder
	sb.WriteString("# 会议总结性能报告\n\n")
	sb.WriteString(fmt.Sprintf("**生成时间**: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// Add overflow warning if detected
	if metrics.OverflowDetected {
		sb.WriteString("## ⚠️ Token 溢出警告\n\n")
		sb.WriteString(fmt.Sprintf("在处理第 **%d** 个分片时检测到 token 溢出。\n", metrics.OverflowAtChunk))
		sb.WriteString(fmt.Sprintf("溢出时累计 token 数量: **%d**\n\n", metrics.OverflowAtTokens))
		sb.WriteString("总结已基于最后一次成功的结果生成。\n\n")
		sb.WriteString("---\n\n")
	}

	sb.WriteString("## 总体指标\n\n")
	sb.WriteString("| 指标 | 值 |\n")
	sb.WriteString("|------|-----|\n")
	sb.WriteString(fmt.Sprintf("| 模型名称 | %s |\n", metrics.ModelName))
	sb.WriteString(fmt.Sprintf("| 总分片数 | %d |\n", metrics.TotalChunks))
	if metrics.OverflowDetected {
		sb.WriteString(fmt.Sprintf("| 成功处理分片数 | %d |\n", len(metrics.ChunkMetrics)))
	}
	sb.WriteString(fmt.Sprintf("| 总 Prompt Tokens | %d |\n", metrics.TotalPromptTokens))
	sb.WriteString(fmt.Sprintf("| 总 Completion Tokens | %d |\n", metrics.TotalCompletionTokens))
	sb.WriteString(fmt.Sprintf("| 总 Tokens | %d |\n", metrics.TotalTokens))
	sb.WriteString(fmt.Sprintf("| 总处理时间 | %.2f 秒 |\n", metrics.TotalProcessingTime.Seconds()))
	sb.WriteString(fmt.Sprintf("| 平均每分片耗时 | %.2f 秒 |\n", metrics.AverageTimePerChunk.Seconds()))
	sb.WriteString(fmt.Sprintf("| Token 生成速度 | %.2f tokens/秒 |\n", metrics.TokensPerSecond))
	sb.WriteString(fmt.Sprintf("| 开始时间 | %s |\n", metrics.StartTime.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("| 结束时间 | %s |\n", metrics.EndTime.Format("2006-01-02 15:04:05")))

	sb.WriteString("\n## 分片详情\n\n")
	sb.WriteString("| 分片 | Prompt Tokens | Completion Tokens | Total Tokens | 耗时(秒) | 状态 |\n")
	sb.WriteString("|------|---------------|-------------------|--------------|----------|------|\n")
	for _, chunk := range metrics.ChunkMetrics {
		status := "✓"
		if chunk.Overflowed {
			status = "⚠️ 溢出"
		}
		sb.WriteString(fmt.Sprintf("| %d | %d | %d | %d | %.2f | %s |\n",
			chunk.ChunkIndex,
			chunk.PromptTokens,
			chunk.CompletionTokens,
			chunk.TotalTokens,
			chunk.ProcessingTime.Seconds(),
			status))
	}

	// Add overflow details if any
	if metrics.OverflowDetected {
		sb.WriteString("\n## 溢出详情\n\n")
		for _, chunk := range metrics.ChunkMetrics {
			if chunk.Overflowed {
				sb.WriteString(fmt.Sprintf("**分片 %d**: %s\n\n", chunk.ChunkIndex, chunk.OverflowError))
			}
		}
	}

	// Save markdown report
	reportPath := filepath.Join(outputDir, "performance_report.md")
	if err := os.WriteFile(reportPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to save performance report: %w", err)
	}
	fmt.Printf("📊 Performance report saved to: %s\n", reportPath)

	// Also save as JSON for programmatic access
	jsonPath := filepath.Join(outputDir, "performance_metrics.json")
	jsonData, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metrics to JSON: %w", err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to save metrics JSON: %w", err)
	}
	fmt.Printf("📊 Performance metrics (JSON) saved to: %s\n", jsonPath)

	return nil
}

func (s *Summarizer) createClient() *http.Client {
	transport := &http.Transport{}

	if s.cfg.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	} else if s.cfg.CACertPath != "" {
		caCert, err := os.ReadFile(s.cfg.CACertPath)
		if err == nil {
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			transport.TLSClientConfig = &tls.Config{RootCAs: caCertPool}
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(s.cfg.TimeoutSec) * time.Second,
	}
}

// cleanResponse removes <think> tags and other unwanted artifacts from the response.
func (s *Summarizer) cleanResponse(response string) string {
	// Remove <think>...</think> blocks including the tags
	for {
		start := strings.Index(response, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(response, "</think>")
		if end == -1 {
			// If no closing tag, remove everything from start
			response = response[:start]
			break
		}
		// Remove the block
		response = response[:start] + response[end+8:]
	}

	// Remove ```markdown and ```
	response = strings.ReplaceAll(response, "```markdown", "")
	response = strings.ReplaceAll(response, "```", "")

	// Trim whitespace
	return strings.TrimSpace(response)
}
