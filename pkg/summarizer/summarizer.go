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
	// Read the transcript file
	content, err := os.ReadFile(transcriptFile)
	if err != nil {
		return "", fmt.Errorf("failed to read transcript file: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create intermediate results directory
	intermediateDir := filepath.Join(outputDir, "intermediate")
	if err := os.MkdirAll(intermediateDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create intermediate directory: %w", err)
	}

	// Split into chunks
	chunks := s.chunker.Split(string(content))
	fmt.Printf("Transcript split into %d chunks\n", len(chunks))

	// Process each chunk iteratively
	var currentSummary string
	for i, chunk := range chunks {
		fmt.Printf("Processing chunk %d/%d...\n", i+1, len(chunks))

		// Build the prompt
		sysPrompt, userPrompt := BuildPrompt(currentSummary, chunk, s.meetingTime)

		// Call the LLM
		response, err := s.chat(sysPrompt, userPrompt)
		if err != nil {
			return "", fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}

		currentSummary = s.cleanResponse(response)

		// Save intermediate result
		intermediatePath := filepath.Join(intermediateDir, fmt.Sprintf("chunk_%02d.md", i+1))
		if err := os.WriteFile(intermediatePath, []byte(currentSummary), 0644); err != nil {
			fmt.Printf("  Warning: failed to save intermediate result: %v\n", err)
		}

		fmt.Printf("  ✓ Chunk %d/%d processed, saved to %s\n", i+1, len(chunks), intermediatePath)
	}

	// Save final summary
	finalPath := filepath.Join(outputDir, "meeting_summary.md")
	if err := os.WriteFile(finalPath, []byte(currentSummary), 0644); err != nil {
		return "", fmt.Errorf("failed to save final summary: %w", err)
	}
	fmt.Printf("\n✅ Final summary saved to: %s\n", finalPath)

	return currentSummary, nil
}

// chat sends a non-streaming chat request to the LLM.
func (s *Summarizer) chat(sysPrompt, userPrompt string) (string, error) {
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
		return "", fmt.Errorf("failed to marshal request: %w", err)
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
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}

	client := s.createClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response choices")
	}

	content := chatResp.Choices[0].Message.Content

	// Verbose logging: response
	if s.cfg.Verbose {
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("[VERBOSE] LLM RESPONSE")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("Status: %d\n", resp.StatusCode)
		fmt.Printf("Tokens: prompt=%d, completion=%d, total=%d\n",
			chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, chatResp.Usage.TotalTokens)
		fmt.Println("\n[Content]:")
		fmt.Println(content)
		fmt.Println(strings.Repeat("=", 80))
	}

	return content, nil
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
