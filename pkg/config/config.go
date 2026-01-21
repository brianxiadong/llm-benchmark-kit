// Package config defines the global configuration for the LLM Benchmark Kit.
package config

// GlobalConfig holds all configuration options for the benchmark.
type GlobalConfig struct {
	// API Configuration
	URL       string // API endpoint URL
	ModelName string // Model name to benchmark
	Token     string // API authentication token

	// Benchmark Parameters
	Concurrency   int     // Number of concurrent workers
	TotalRequests int     // Total number of requests to make
	DurationSec   int     // Duration in seconds (alternative to TotalRequests)
	RPS           float64 // Requests per second limit (0 = unlimited)
	Warmup        int     // Number of warmup requests (excluded from stats)
	MaxTokens     int     // Max tokens for response

	// Token Counting Mode
	TokenMode string // usage|chars|disabled

	// Network Configuration
	TimeoutSec  int    // Request timeout in seconds
	InsecureTLS bool   // Skip TLS verification
	CACertPath  string // Custom CA certificate path

	// Input/Output
	WorkloadFile string // Path to prompts file (each line a prompt or JSONL)
	OutputDir    string // Output directory for results

	// Provider Selection
	ProviderType string // Provider type: openai, aliyun, custom
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *GlobalConfig {
	return &GlobalConfig{
		Concurrency:   1,
		TotalRequests: 10,
		MaxTokens:     256,
		TokenMode:     "usage",
		TimeoutSec:    60,
		OutputDir:     "./output",
		ProviderType:  "openai",
	}
}
