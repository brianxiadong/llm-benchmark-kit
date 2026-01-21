// Package workload provides workload loading utilities.
package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Loader loads workload inputs from various sources.
type Loader struct{}

// NewLoader creates a new workload loader.
func NewLoader() *Loader {
	return &Loader{}
}

// LoadFromFile loads workloads from a file.
// Supports:
// - Plain text (one prompt per line)
// - JSONL (one JSON object per line with prompt/messages fields)
func (l *Loader) LoadFromFile(path string, maxTokens int) ([]WorkloadInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open workload file: %w", err)
	}
	defer file.Close()

	var workloads []WorkloadInput
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	id := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		id++
		workload, err := l.parseLine(line, id, maxTokens)
		if err != nil {
			return nil, fmt.Errorf("failed to parse line %d: %w", id, err)
		}
		workloads = append(workloads, workload)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read workload file: %w", err)
	}

	return workloads, nil
}

func (l *Loader) parseLine(line string, id int, maxTokens int) (WorkloadInput, error) {
	// Try to parse as JSON first
	if strings.HasPrefix(line, "{") {
		var input WorkloadInput
		if err := json.Unmarshal([]byte(line), &input); err == nil {
			if input.ID == "" {
				input.ID = fmt.Sprintf("req-%d", id)
			}
			if input.MaxTokens == 0 {
				input.MaxTokens = maxTokens
			}
			return input, nil
		}
	}

	// Treat as plain text prompt
	return NewSimpleWorkload(fmt.Sprintf("req-%d", id), line, maxTokens), nil
}

// GenerateDefault generates a default workload for testing.
func (l *Loader) GenerateDefault(count, maxTokens int) []WorkloadInput {
	prompts := []string{
		"Hello, how are you?",
		"What is the capital of France?",
		"Explain quantum computing in simple terms.",
		"Write a short poem about the ocean.",
		"What are the benefits of exercise?",
	}

	workloads := make([]WorkloadInput, count)
	for i := 0; i < count; i++ {
		prompt := prompts[i%len(prompts)]
		workloads[i] = NewSimpleWorkload(fmt.Sprintf("req-%d", i+1), prompt, maxTokens)
	}
	return workloads
}
