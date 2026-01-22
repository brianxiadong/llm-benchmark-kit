// Package openai provides an OpenAI-compatible API provider.
package openai

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
	"strings"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/provider"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/sse"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/workload"
)

func init() {
	provider.Register("openai", func() provider.Provider {
		return &Provider{}
	})
}

// Provider implements the OpenAI-compatible API provider.
type Provider struct{}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "openai"
}

// ChatRequest represents the OpenAI chat completion request.
type ChatRequest struct {
	Model         string                 `json:"model"`
	Messages      []workload.ChatMessage `json:"messages"`
	MaxTokens     int                    `json:"max_tokens,omitempty"`
	Stream        bool                   `json:"stream"`
	StreamOptions *StreamOptions         `json:"stream_options,omitempty"`
}

// StreamOptions configures stream behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// StreamChoice represents a choice in the streaming response.
type StreamChoice struct {
	Index        int          `json:"index"`
	Delta        DeltaContent `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
}

// DeltaContent represents the delta content in streaming.
type DeltaContent struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// StreamResponse represents a single streaming response chunk.
type StreamResponse struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []StreamChoice       `json:"choices"`
	Usage   *provider.TokenUsage `json:"usage,omitempty"`
}

// StreamChat executes a streaming chat request.
func (p *Provider) StreamChat(ctx context.Context, cfg *config.GlobalConfig, input workload.WorkloadInput) (<-chan provider.StreamEvent, error) {
	// Build request body
	messages := input.ToMessages()
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	maxTokens := input.MaxTokens
	if maxTokens == 0 {
		maxTokens = cfg.MaxTokens
	}

	reqBody := ChatRequest{
		Model:     cfg.ModelName,
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    true,
		StreamOptions: &StreamOptions{
			IncludeUsage: true, // Request usage info in stream (for vLLM compatibility)
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Verbose logging: request
	if cfg.Verbose {
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("[VERBOSE] LLM STREAM REQUEST")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("URL: %s\n", cfg.URL)
		fmt.Printf("Model: %s\n", cfg.ModelName)
		fmt.Printf("MaxTokens: %d\n", maxTokens)
		fmt.Println("\n[Messages]:")
		for i, msg := range messages {
			fmt.Printf("  [%d] %s: %s\n", i, msg.Role, truncateString(msg.Content, 200))
		}
		fmt.Println(strings.Repeat("=", 80))
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.URL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	// Create HTTP client
	client := p.createClient(cfg)

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Create event channel
	events := make(chan provider.StreamEvent, 100)

	// Start goroutine to parse SSE
	go p.parseStream(resp.Body, events, cfg.Verbose)

	return events, nil
}

// truncateString truncates a string to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (p *Provider) createClient(cfg *config.GlobalConfig) *http.Client {
	transport := &http.Transport{}

	if cfg.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	} else if cfg.CACertPath != "" {
		caCert, err := os.ReadFile(cfg.CACertPath)
		if err == nil {
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			transport.TLSClientConfig = &tls.Config{RootCAs: caCertPool}
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
	}
}

func (p *Provider) parseStream(body io.ReadCloser, events chan<- provider.StreamEvent, verbose bool) {
	defer close(events)
	defer body.Close()

	parser := sse.NewParser(body)
	var lastUsage *provider.TokenUsage
	var fullContent strings.Builder // Accumulate content for verbose logging

	for {
		event, err := parser.Next()
		if err == io.EOF {
			// Verbose logging: response
			if verbose && fullContent.Len() > 0 {
				fmt.Println("\n" + strings.Repeat("=", 80))
				fmt.Println("[VERBOSE] LLM STREAM RESPONSE")
				fmt.Println(strings.Repeat("-", 80))
				fmt.Printf("[Content] (%d chars):\n", fullContent.Len())
				fmt.Println(truncateString(fullContent.String(), 500))
				fmt.Println(strings.Repeat("=", 80))
			}
			// Send end event if we haven't received one
			events <- provider.StreamEvent{Type: provider.EventEnd}
			return
		}
		if err != nil {
			events <- provider.StreamEvent{
				Type: provider.EventError,
				Err:  fmt.Errorf("SSE parse error: %w", err),
			}
			return
		}

		// Check for [DONE] signal
		if event.Data == "[DONE]" {
			// Verbose logging: response
			if verbose && fullContent.Len() > 0 {
				fmt.Println("\n" + strings.Repeat("=", 80))
				fmt.Println("[VERBOSE] LLM STREAM RESPONSE")
				fmt.Println(strings.Repeat("-", 80))
				fmt.Printf("[Content] (%d chars):\n", fullContent.Len())
				fmt.Println(truncateString(fullContent.String(), 500))
				fmt.Println(strings.Repeat("=", 80))
			}
			// Send any remaining usage if not already sent
			if lastUsage != nil {
				events <- provider.StreamEvent{
					Type:  provider.EventUsage,
					Usage: lastUsage,
				}
			}
			events <- provider.StreamEvent{
				Type: provider.EventEnd,
				Raw:  event.Data,
			}
			return
		}

		// Parse JSON response
		var resp StreamResponse
		if err := json.Unmarshal([]byte(event.Data), &resp); err != nil {
			// Skip invalid JSON (might be partial or metadata)
			if verbose {
				fmt.Printf("[DEBUG] Failed to parse JSON: %v\nData: %s\n", err, truncateString(event.Data, 200))
			}
			continue
		}

		// Store usage for later (usually comes with final chunk or [DONE])
		if resp.Usage != nil {
			lastUsage = resp.Usage
			// For vLLM, send usage event immediately when received
			// (vLLM sends usage in a separate chunk with empty choices)
			events <- provider.StreamEvent{
				Type:  provider.EventUsage,
				Usage: lastUsage,
			}
		}

		// Check for content
		for _, choice := range resp.Choices {
			if choice.Delta.Content != "" {
				// Accumulate for verbose logging
				if verbose {
					fullContent.WriteString(choice.Delta.Content)
				}
				events <- provider.StreamEvent{
					Type: provider.EventContent,
					Raw:  event.Data,
					Text: choice.Delta.Content,
				}
			}

			// Note: We no longer return on finish_reason because vLLM sends usage
			// in a separate chunk AFTER finish_reason. We wait for [DONE] instead.
		}
	}
}
