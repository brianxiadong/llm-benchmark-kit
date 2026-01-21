// Package main is the entry point for the LLM Benchmark Kit CLI.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/provider"
	_ "github.com/brianxiadong/llm-benchmark-kit/pkg/provider/openai" // Register OpenAI provider
	"github.com/brianxiadong/llm-benchmark-kit/pkg/runner"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cfg := config.DefaultConfig()

	// API Configuration
	flag.StringVar(&cfg.URL, "url", "", "API endpoint URL (required)")
	flag.StringVar(&cfg.ModelName, "model", "", "Model name to benchmark (required)")
	flag.StringVar(&cfg.Token, "token", "", "API authentication token")

	// Benchmark Parameters
	flag.IntVar(&cfg.Concurrency, "concurrency", cfg.Concurrency, "Number of concurrent workers")
	flag.IntVar(&cfg.TotalRequests, "total-requests", cfg.TotalRequests, "Total number of requests to make")
	flag.IntVar(&cfg.DurationSec, "duration", 0, "Duration in seconds (alternative to total-requests)")
	flag.Float64Var(&cfg.RPS, "rps", 0, "Requests per second limit (0 = unlimited)")
	flag.IntVar(&cfg.Warmup, "warmup", 0, "Number of warmup requests (excluded from stats)")
	flag.IntVar(&cfg.MaxTokens, "max-tokens", cfg.MaxTokens, "Max tokens for response")

	// Token Mode
	flag.StringVar(&cfg.TokenMode, "token-mode", cfg.TokenMode, "Token counting mode: usage|chars|disabled")

	// Network Configuration
	flag.IntVar(&cfg.TimeoutSec, "timeout", cfg.TimeoutSec, "Request timeout in seconds")
	flag.BoolVar(&cfg.InsecureTLS, "insecure", false, "Skip TLS verification")
	flag.StringVar(&cfg.CACertPath, "ca-cert", "", "Custom CA certificate path")

	// Input/Output
	flag.StringVar(&cfg.WorkloadFile, "workload-file", "", "Path to prompts file (each line a prompt or JSONL)")
	flag.StringVar(&cfg.OutputDir, "out", cfg.OutputDir, "Output directory for results")

	// Provider
	flag.StringVar(&cfg.ProviderType, "provider", cfg.ProviderType, "Provider type: openai, aliyun, custom")

	// Version flag
	showVersion := flag.Bool("version", false, "Show version information")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "LLM Benchmark Kit - High-Performance LLM Benchmarking Tool\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Basic benchmark with OpenAI-compatible API\n")
		fmt.Fprintf(os.Stderr, "  %s -url https://api.openai.com/v1/chat/completions -model gpt-4 -token $OPENAI_API_KEY\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Benchmark with custom concurrency and request count\n")
		fmt.Fprintf(os.Stderr, "  %s -url https://api.example.com/v1/chat/completions -model llama3 -concurrency 10 -total-requests 100\n\n", os.Args[0])
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("llm-benchmark-kit version %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// Validate required flags
	if cfg.URL == "" {
		log.Fatal("Error: -url is required")
	}
	if cfg.ModelName == "" {
		log.Fatal("Error: -model is required")
	}

	// Validate token mode
	switch cfg.TokenMode {
	case "usage", "chars", "disabled":
		// Valid
	default:
		log.Fatalf("Error: invalid token-mode '%s', must be one of: usage, chars, disabled", cfg.TokenMode)
	}

	// Get the provider
	p, err := provider.Get(cfg.ProviderType)
	if err != nil {
		log.Fatalf("Error: %v\nAvailable providers: %v", err, provider.List())
	}

	fmt.Printf("LLM Benchmark Kit\n")
	fmt.Printf("==================\n")
	fmt.Printf("Provider:     %s\n", p.Name())
	fmt.Printf("URL:          %s\n", cfg.URL)
	fmt.Printf("Model:        %s\n", cfg.ModelName)
	fmt.Printf("Concurrency:  %d\n", cfg.Concurrency)
	fmt.Printf("Requests:     %d\n", cfg.TotalRequests)
	fmt.Printf("Warmup:       %d\n", cfg.Warmup)
	fmt.Printf("Token Mode:   %s\n", cfg.TokenMode)
	fmt.Printf("Output:       %s\n", cfg.OutputDir)
	fmt.Println()

	// Run the benchmark
	r := runner.New(cfg, p)
	report, err := r.Run()
	if err != nil {
		log.Fatalf("Benchmark failed: %v", err)
	}

	fmt.Printf("\nBenchmark Complete!\n")
	fmt.Printf("==================\n")
	fmt.Printf("Success Rate: %.2f%% (%d/%d)\n", report.SuccessRate*100, report.Success, report.TotalRequests)
	fmt.Printf("Avg TTFT:     %.2f ms\n", report.AvgTTFTMs)
	fmt.Printf("Avg Latency:  %.2f ms\n", report.AvgLatencyMs)
	fmt.Printf("P50 TTFT:     %d ms\n", report.P50TTFTMs)
	fmt.Printf("P95 TTFT:     %d ms\n", report.P95TTFTMs)
	fmt.Printf("P99 TTFT:     %d ms\n", report.P99TTFTMs)
	fmt.Printf("P50 Latency:  %d ms\n", report.P50LatencyMs)
	fmt.Printf("P95 Latency:  %d ms\n", report.P95LatencyMs)
	fmt.Printf("P99 Latency:  %d ms\n", report.P99LatencyMs)
	fmt.Printf("RPS:          %.2f\n", report.RPS)
	if cfg.TokenMode != "disabled" {
		fmt.Printf("Throughput:   %.2f %s/s\n", report.TokenThroughput, cfg.TokenMode)
	}
	fmt.Printf("\nResults saved to: %s\n", cfg.OutputDir)
}
