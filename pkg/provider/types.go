// Package provider defines the Provider interface and streaming types.
package provider

import (
	"context"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/workload"
)

// StreamEventType represents the type of streaming event.
type StreamEventType int

const (
	// EventMeta represents metadata events (non-content).
	EventMeta StreamEventType = iota
	// EventContent represents visible content (used for TTFT determination).
	EventContent
	// EventUsage represents token usage information.
	EventUsage
	// EventEnd represents explicit end signal ([DONE] / finish_reason).
	EventEnd
	// EventError represents an error event.
	EventError
)

// String returns the string representation of the event type.
func (t StreamEventType) String() string {
	switch t {
	case EventMeta:
		return "meta"
	case EventContent:
		return "content"
	case EventUsage:
		return "usage"
	case EventEnd:
		return "end"
	case EventError:
		return "error"
	default:
		return "unknown"
	}
}

// TokenUsage holds token count information.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// StreamEvent represents a single event from the SSE stream.
type StreamEvent struct {
	Type  StreamEventType
	Raw   string      // Original raw data (for sampling/debugging)
	Text  string      // Content text (if EventContent)
	Usage *TokenUsage // Token usage (if EventUsage)
	Err   error       // Error (if EventError)
}

// Provider defines the interface for LLM API providers.
type Provider interface {
	// Name returns the provider name.
	Name() string

	// StreamChat executes a streaming chat request and returns events via channel.
	// The channel will be closed when the stream ends (success or error).
	StreamChat(ctx context.Context, cfg *config.GlobalConfig, input workload.WorkloadInput) (<-chan StreamEvent, error)
}
