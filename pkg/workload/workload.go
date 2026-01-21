// Package workload defines workload input types.
package workload

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// WorkloadInput represents a single benchmark request input.
type WorkloadInput struct {
	ID        string        `json:"id"`
	Prompt    string        `json:"prompt,omitempty"`
	Messages  []ChatMessage `json:"messages,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

// NewSimpleWorkload creates a WorkloadInput with a simple prompt.
func NewSimpleWorkload(id, prompt string, maxTokens int) WorkloadInput {
	return WorkloadInput{
		ID:        id,
		Prompt:    prompt,
		MaxTokens: maxTokens,
	}
}

// NewChatWorkload creates a WorkloadInput with chat messages.
func NewChatWorkload(id string, messages []ChatMessage, maxTokens int) WorkloadInput {
	return WorkloadInput{
		ID:        id,
		Messages:  messages,
		MaxTokens: maxTokens,
	}
}

// ToMessages converts the workload to chat messages format.
// If Messages is set, returns it directly.
// If Prompt is set, converts it to a single user message.
func (w *WorkloadInput) ToMessages() []ChatMessage {
	if len(w.Messages) > 0 {
		return w.Messages
	}
	if w.Prompt != "" {
		return []ChatMessage{
			{Role: "user", Content: w.Prompt},
		}
	}
	return nil
}
