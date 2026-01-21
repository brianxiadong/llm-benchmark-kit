package workload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSimpleWorkload(t *testing.T) {
	w := NewSimpleWorkload("test-1", "Hello world", 100)

	if w.ID != "test-1" {
		t.Errorf("expected ID 'test-1', got '%s'", w.ID)
	}
	if w.Prompt != "Hello world" {
		t.Errorf("expected Prompt 'Hello world', got '%s'", w.Prompt)
	}
	if w.MaxTokens != 100 {
		t.Errorf("expected MaxTokens 100, got %d", w.MaxTokens)
	}
}

func TestNewChatWorkload(t *testing.T) {
	messages := []ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hi!"},
	}
	w := NewChatWorkload("test-2", messages, 200)

	if w.ID != "test-2" {
		t.Errorf("expected ID 'test-2', got '%s'", w.ID)
	}
	if len(w.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(w.Messages))
	}
	if w.MaxTokens != 200 {
		t.Errorf("expected MaxTokens 200, got %d", w.MaxTokens)
	}
}

func TestWorkloadInput_ToMessages(t *testing.T) {
	t.Run("from messages", func(t *testing.T) {
		messages := []ChatMessage{
			{Role: "user", Content: "Hello"},
		}
		w := WorkloadInput{Messages: messages}
		result := w.ToMessages()
		if len(result) != 1 {
			t.Errorf("expected 1 message, got %d", len(result))
		}
	})

	t.Run("from prompt", func(t *testing.T) {
		w := WorkloadInput{Prompt: "Hello"}
		result := w.ToMessages()
		if len(result) != 1 {
			t.Errorf("expected 1 message, got %d", len(result))
		}
		if result[0].Role != "user" {
			t.Errorf("expected role 'user', got '%s'", result[0].Role)
		}
		if result[0].Content != "Hello" {
			t.Errorf("expected content 'Hello', got '%s'", result[0].Content)
		}
	})

	t.Run("empty", func(t *testing.T) {
		w := WorkloadInput{}
		result := w.ToMessages()
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})
}

func TestLoader_LoadFromFile_PlainText(t *testing.T) {
	// Create temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "prompts.txt")
	content := "What is AI?\nExplain machine learning\nHello world"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	loader := NewLoader()
	workloads, err := loader.LoadFromFile(path, 256)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if len(workloads) != 3 {
		t.Fatalf("expected 3 workloads, got %d", len(workloads))
	}

	if workloads[0].Prompt != "What is AI?" {
		t.Errorf("workload 0: expected 'What is AI?', got '%s'", workloads[0].Prompt)
	}
}

func TestLoader_LoadFromFile_JSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompts.jsonl")
	content := `{"prompt": "Hello", "max_tokens": 100}
{"id": "custom-id", "prompt": "World"}
{"messages": [{"role": "user", "content": "Hi"}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	loader := NewLoader()
	workloads, err := loader.LoadFromFile(path, 256)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if len(workloads) != 3 {
		t.Fatalf("expected 3 workloads, got %d", len(workloads))
	}

	if workloads[0].Prompt != "Hello" {
		t.Errorf("workload 0: expected 'Hello', got '%s'", workloads[0].Prompt)
	}
	if workloads[0].MaxTokens != 100 {
		t.Errorf("workload 0: expected MaxTokens 100, got %d", workloads[0].MaxTokens)
	}

	if workloads[1].ID != "custom-id" {
		t.Errorf("workload 1: expected ID 'custom-id', got '%s'", workloads[1].ID)
	}

	if len(workloads[2].Messages) != 1 {
		t.Errorf("workload 2: expected 1 message, got %d", len(workloads[2].Messages))
	}
}

func TestLoader_GenerateDefault(t *testing.T) {
	loader := NewLoader()
	workloads := loader.GenerateDefault(10, 256)

	if len(workloads) != 10 {
		t.Errorf("expected 10 workloads, got %d", len(workloads))
	}

	// Check that IDs are unique
	idSet := make(map[string]bool)
	for _, w := range workloads {
		if idSet[w.ID] {
			t.Errorf("duplicate ID: %s", w.ID)
		}
		idSet[w.ID] = true

		if w.MaxTokens != 256 {
			t.Errorf("expected MaxTokens 256, got %d", w.MaxTokens)
		}
	}
}
