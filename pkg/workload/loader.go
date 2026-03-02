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

// GenerateLong generates long workloads that simulate document analysis tasks.
// These prompts require longer responses (2048+ tokens), exercising deeper KV cache usage.
func (l *Loader) GenerateLong(count, maxTokens int) []WorkloadInput {
	prompts := []string{
		"Write a detailed technical analysis of transformer architecture in deep learning. Cover the attention mechanism, positional encoding, multi-head attention, feed-forward networks, layer normalization, and how they work together. Include mathematical formulations and practical considerations for deployment.",
		"Create a comprehensive guide to building a production-ready microservices architecture. Cover service discovery, load balancing, circuit breakers, API gateways, distributed tracing, logging, monitoring, containerization, orchestration, and CI/CD pipelines. Include specific technology recommendations and trade-offs.",
		"Write an in-depth analysis of the current state of large language models. Discuss architecture innovations, training methodologies, scaling laws, RLHF, Constitutional AI, mixture of experts, inference optimization techniques, quantization, speculative decoding, and future research directions.",
		"Create a detailed tutorial on implementing a distributed database from scratch. Cover data partitioning strategies, replication protocols, consensus algorithms (Raft/Paxos), conflict resolution, transaction management, query optimization, and failure recovery mechanisms.",
		"Write a thorough comparison of major cloud computing platforms (AWS, Azure, GCP). Cover compute services, storage options, networking, AI/ML services, serverless offerings, pricing models, security features, compliance certifications, and migration strategies. Include real-world use case recommendations.",
		"Provide a comprehensive overview of modern cybersecurity practices. Cover threat modeling, zero trust architecture, identity and access management, encryption standards, vulnerability management, incident response, SOC operations, penetration testing methodologies, and compliance frameworks like SOC2, ISO 27001, and GDPR.",
		"Write a detailed guide to performance optimization for web applications. Cover frontend optimization (Critical Rendering Path, code splitting, lazy loading, caching strategies), backend optimization (database query optimization, connection pooling, async processing), infrastructure optimization (CDN, edge computing, auto-scaling), and monitoring/profiling tools.",
		"Create an extensive analysis of GPU computing architectures and their evolution. Cover CUDA programming model, tensor cores, memory hierarchy, warp scheduling, kernel optimization techniques, multi-GPU communication (NVLink, NVSwitch), and how modern AI accelerators differ from traditional GPUs.",
	}

	workloads := make([]WorkloadInput, count)
	for i := 0; i < count; i++ {
		prompt := prompts[i%len(prompts)]
		workloads[i] = NewSimpleWorkload(fmt.Sprintf("long-req-%d", i+1), prompt, maxTokens)
	}
	return workloads
}
