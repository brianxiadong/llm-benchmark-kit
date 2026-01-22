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

// ========== Function Call Types ==========

// Tool represents a tool definition for function calling.
type Tool struct {
	Type     string   `json:"type"` // "function"
	Function Function `json:"function"`
}

// Function represents a function definition.
type Function struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  Parameters `json:"parameters"`
}

// Parameters represents function parameters schema.
type Parameters struct {
	Type       string              `json:"type"` // "object"
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

// Property represents a single parameter property.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ToolChoice represents tool selection.
type ToolChoice struct {
	Type     string             `json:"type"` // "function"
	Function ToolChoiceFunction `json:"function,omitempty"`
}

// ToolChoiceFunction specifies which function to call.
type ToolChoiceFunction struct {
	Name string `json:"name"`
}

// ToolCall represents a tool call in the response.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction represents the function call details.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}
