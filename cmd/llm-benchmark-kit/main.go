// Package main is the entry point for the LLM Benchmark Kit CLI.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/embedded"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/fulltest"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/provider"
	_ "github.com/brianxiadong/llm-benchmark-kit/pkg/provider/openai" // Register OpenAI provider
	"github.com/brianxiadong/llm-benchmark-kit/pkg/runner"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/soaktest"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/summarizer"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/summarybench"
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

	// Meeting Summary Mode
	transcriptFile := flag.String("transcript-file", "", "Path to meeting transcript file (enables summary mode)")
	chunkSize := flag.Int("chunk-size", 8000, "Maximum characters per chunk for transcript processing")
	meetingTime := flag.String("meeting-time", "", "Meeting time for the summary header")

	// Debug Options
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging of LLM requests and responses")

	// Model Behavior
	flag.BoolVar(&cfg.DisableThinking, "no-thinking", false, "Disable thinking/reasoning mode (sends chat_template_kwargs.enable_thinking=false)")

	// Full Test Mode
	fullTest := flag.Bool("full-test", false, "Run complete test suite (benchmark + summary)")

	// Summary Benchmark Mode
	summaryBench := flag.Bool("summary-bench", false, "Run concurrent meeting summary benchmark")
	summaryBenchConcurrency := flag.Int("sb-concurrency", 5, "Concurrency for summary benchmark")
	summaryBenchRequests := flag.Int("sb-requests", 20, "Total requests for summary benchmark")

	// Soak Test Mode
	soakTest := flag.Bool("soak", false, "Run soak/endurance test (long-running stability test)")
	soakDuration := flag.Int("soak-duration", 300, "Soak test duration in seconds")
	soakConcurrency := flag.Int("soak-concurrency", 5, "Soak test concurrency")
	soakWindow := flag.Int("soak-window", 30, "Soak test snapshot window interval in seconds")
	soakMetricsInterval := flag.Int("soak-metrics-interval", 10, "System metrics collection interval in seconds")
	soakLongConcurrency := flag.Int("soak-long-concurrency", 0, "Number of workers for long requests (0 = all short)")
	soakLongMaxTokens := flag.Int("soak-long-max-tokens", 2048, "Max tokens for long request workers")

	// Soak Report Rebuild Mode
	soakReportDir := flag.String("soak-report", "", "Rebuild soak report from logs in the given directory (no server needed)")
	soakReportOutput := flag.String("soak-report-output", "", "Output directory for rebuilt report (default: same as input)")

	// Version flag
	showVersion := flag.Bool("version", false, "Show version information")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "LLM Benchmark Kit - High-Performance LLM Benchmarking Tool\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Modes:\n")
		fmt.Fprintf(os.Stderr, "  Benchmark Mode:      Run performance tests against LLM API\n")
		fmt.Fprintf(os.Stderr, "  Summary Mode:        Summarize meeting transcripts (use -transcript-file)\n")
		fmt.Fprintf(os.Stderr, "  Full Test Mode:      Run complete test suite (use -full-test)\n")
		fmt.Fprintf(os.Stderr, "  Summary Bench Mode:  Concurrent meeting summary benchmark (use -summary-bench)\n")
		fmt.Fprintf(os.Stderr, "  Soak Test Mode:      Long-running stability/endurance test (use -soak)\n")
		fmt.Fprintf(os.Stderr, "  Soak Report Mode:    Rebuild report from soak test logs (use -soak-report)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Benchmark mode\n")
		fmt.Fprintf(os.Stderr, "  %s -url https://api.openai.com/v1/chat/completions -model gpt-4 -token $OPENAI_API_KEY\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Summary mode\n")
		fmt.Fprintf(os.Stderr, "  %s -url http://localhost:8000/v1/chat/completions -model qwen -transcript-file meeting.txt\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Full test mode\n")
		fmt.Fprintf(os.Stderr, "  %s -full-test -url http://localhost:8000/v1/chat/completions -model qwen\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Summary benchmark mode (concurrent meeting summary stress test)\n")
		fmt.Fprintf(os.Stderr, "  %s -summary-bench -sb-concurrency 10 -sb-requests 50 -url http://localhost:8000/v1/chat/completions -model qwen\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Soak test mode (long-running stability test with system metrics)\n")
		fmt.Fprintf(os.Stderr, "  %s -soak -soak-duration 3600 -soak-concurrency 10 -soak-window 60 -url http://localhost:8000/v1/chat/completions -model qwen\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Rebuild soak report from logs (download logs from server, generate report locally)\n")
		fmt.Fprintf(os.Stderr, "  %s -soak-report ./output/soaktest_qwen_20260302_120000\n\n", os.Args[0])
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("llm-benchmark-kit version %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// Soak report rebuild mode does not require -url or -model
	if *soakReportDir != "" {
		runSoakReportRebuild(*soakReportDir, *soakReportOutput)
		return
	}

	// Validate required flags
	if cfg.URL == "" {
		log.Fatal("Error: -url is required")
	}
	if cfg.ModelName == "" {
		log.Fatal("Error: -model is required")
	}

	// Check if running in soak test mode
	if *soakTest {
		runSoakTest(cfg, *soakDuration, *soakConcurrency, *soakWindow, *soakMetricsInterval, *soakLongConcurrency, *soakLongMaxTokens)
		return
	}

	// Check if running in full-test mode
	if *fullTest {
		runFullTest(cfg)
		return
	}

	// Check if running in summary benchmark mode
	if *summaryBench {
		runSummaryBench(cfg, *transcriptFile, *chunkSize, *summaryBenchConcurrency, *summaryBenchRequests)
		return
	}

	// Check if running in summary mode
	if *transcriptFile != "" {
		runSummaryMode(cfg, *transcriptFile, *chunkSize, *meetingTime)
		return
	}

	// Benchmark mode
	runBenchmarkMode(cfg)
}

func runSummaryMode(cfg *config.GlobalConfig, transcriptFile string, chunkSize int, meetingTime string) {
	// Set default meeting time if not provided
	if meetingTime == "" {
		meetingTime = time.Now().Format("2006-01-02 15:04")
	}

	// Auto-generate output directory
	modelName := cfg.ModelName
	modelName = strings.ReplaceAll(modelName, "/", "_")
	modelName = strings.ReplaceAll(modelName, ":", "_")
	timestamp := time.Now().Format("20060102_150405")
	outputDir := filepath.Join("output", fmt.Sprintf("summary_%s_%s", modelName, timestamp))

	fmt.Printf("Meeting Summary Mode\n")
	fmt.Printf("====================\n")
	fmt.Printf("URL:          %s\n", cfg.URL)
	fmt.Printf("Model:        %s\n", cfg.ModelName)
	fmt.Printf("Transcript:   %s\n", transcriptFile)
	fmt.Printf("Chunk Size:   %d chars\n", chunkSize)
	fmt.Printf("Meeting Time: %s\n", meetingTime)
	fmt.Printf("Output:       %s\n", outputDir)
	fmt.Println()

	sum := summarizer.NewSummarizer(cfg, chunkSize, meetingTime)
	_, err := sum.Run(transcriptFile, outputDir)
	if err != nil {
		log.Fatalf("Summarization failed: %v", err)
	}

	fmt.Printf("\n✅ Meeting summary complete!\n")
	fmt.Printf("   Final summary:    %s/meeting_summary.md\n", outputDir)
	fmt.Printf("   Intermediate:     %s/intermediate/\n", outputDir)
}

func runBenchmarkMode(cfg *config.GlobalConfig) {
	// Validate token mode
	switch cfg.TokenMode {
	case "usage", "chars", "disabled":
		// Valid
	default:
		log.Fatalf("Error: invalid token-mode '%s', must be one of: usage, chars, disabled", cfg.TokenMode)
	}

	// Auto-generate output directory if using default
	if cfg.OutputDir == "./output" {
		modelName := cfg.ModelName
		modelName = strings.ReplaceAll(modelName, "/", "_")
		modelName = strings.ReplaceAll(modelName, ":", "_")
		modelName = strings.ReplaceAll(modelName, " ", "_")
		timestamp := time.Now().Format("20060102_150405")
		cfg.OutputDir = filepath.Join("output", fmt.Sprintf("%s_%s", modelName, timestamp))
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

func runFullTest(cfg *config.GlobalConfig) {
	// Use moderate benchmark settings
	moderateCfg := config.ModerateBenchmarkConfig()
	moderateCfg.URL = cfg.URL
	moderateCfg.ModelName = cfg.ModelName
	moderateCfg.Token = cfg.Token
	moderateCfg.InsecureTLS = cfg.InsecureTLS
	moderateCfg.CACertPath = cfg.CACertPath
	moderateCfg.Verbose = cfg.Verbose
	moderateCfg.DisableThinking = cfg.DisableThinking

	// Auto-generate output directory
	modelName := cfg.ModelName
	modelName = strings.ReplaceAll(modelName, "/", "_")
	modelName = strings.ReplaceAll(modelName, ":", "_")
	modelName = strings.ReplaceAll(modelName, " ", "_")
	timestamp := time.Now().Format("20060102_150405")
	outputDir := filepath.Join("output", fmt.Sprintf("fulltest_%s_%s", modelName, timestamp))

	// Find transcript file - try relative to working directory first
	transcriptFile := "example/text.txt"
	if _, err := os.Stat(transcriptFile); os.IsNotExist(err) {
		// Try relative to executable
		execPath, _ := os.Executable()
		execDir := filepath.Dir(execPath)
		transcriptFile = filepath.Join(execDir, "..", "example", "text.txt")
		if _, err := os.Stat(transcriptFile); os.IsNotExist(err) {
			// Use embedded transcript - write to temp file
			embeddedData := embedded.GetTranscriptSample()
			if len(embeddedData) > 0 {
				tmpFile := filepath.Join(os.TempDir(), "llm-benchmark-transcript.txt")
				if err := os.WriteFile(tmpFile, embeddedData, 0644); err == nil {
					transcriptFile = tmpFile
					fmt.Println("   Using embedded transcript sample")
				} else {
					log.Printf("Warning: failed to write embedded transcript: %v", err)
					transcriptFile = ""
				}
			} else {
				log.Printf("Warning: transcript file not found, summary test will be skipped")
				transcriptFile = ""
			}
		}
	}

	// Get the provider
	p, err := provider.Get(moderateCfg.ProviderType)
	if err != nil {
		log.Fatalf("Error: %v\nAvailable providers: %v", err, provider.List())
	}

	// Create and run full test
	r := fulltest.NewRunner(moderateCfg, p, transcriptFile, outputDir)
	report, err := r.Run()
	if err != nil {
		log.Fatalf("Full test failed: %v", err)
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Full Test Complete!                         ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📊 Total Duration: %.2f seconds\n", report.TotalDuration.Seconds())
	fmt.Printf("📁 Results saved to: %s\n", outputDir)
	fmt.Printf("📄 Full report: %s/full_test_report.md\n", outputDir)
}

func runSummaryBench(cfg *config.GlobalConfig, transcriptFile string, chunkSize, concurrency, requests int) {
	// Auto-generate output directory
	modelName := cfg.ModelName
	modelName = strings.ReplaceAll(modelName, "/", "_")
	modelName = strings.ReplaceAll(modelName, ":", "_")
	modelName = strings.ReplaceAll(modelName, " ", "_")
	timestamp := time.Now().Format("20060102_150405")
	outputDir := filepath.Join("output", fmt.Sprintf("summarybench_%s_%s", modelName, timestamp))

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║         LLM Benchmark Kit - Summary Benchmark Mode             ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📋 Model:       %s\n", cfg.ModelName)
	fmt.Printf("🔗 URL:         %s\n", cfg.URL)
	fmt.Printf("👥 Concurrency: %d\n", concurrency)
	fmt.Printf("📝 Requests:    %d\n", requests)
	fmt.Printf("📏 Chunk Size:  %d chars\n", chunkSize)
	fmt.Printf("📁 Output:      %s\n", outputDir)

	bench := summarybench.NewBenchmark(cfg, concurrency, requests, chunkSize)
	_, err := bench.Run(transcriptFile, outputDir)
	if err != nil {
		log.Fatalf("Summary benchmark failed: %v", err)
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              Summary Benchmark Complete!                        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Printf("📁 Results saved to: %s\n", outputDir)
}

func runSoakTest(cfg *config.GlobalConfig, duration, concurrency, window, metricsInterval, longConcurrency, longMaxTokens int) {
	// Auto-generate output directory
	modelName := cfg.ModelName
	modelName = strings.ReplaceAll(modelName, "/", "_")
	modelName = strings.ReplaceAll(modelName, ":", "_")
	modelName = strings.ReplaceAll(modelName, " ", "_")
	timestamp := time.Now().Format("20060102_150405")
	outputDir := filepath.Join("output", fmt.Sprintf("soaktest_%s_%s", modelName, timestamp))

	soakCfg := &soaktest.SoakConfig{
		DurationSec:     duration,
		Concurrency:     concurrency,
		WindowSec:       window,
		MetricsInterval: metricsInterval,
		LongConcurrency: longConcurrency,
		LongMaxTokens:   longMaxTokens,
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║         LLM Benchmark Kit - Soak Test Mode                     ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📋 Model:            %s\n", cfg.ModelName)
	fmt.Printf("🔗 URL:              %s\n", cfg.URL)
	fmt.Printf("👥 Concurrency:      %d\n", concurrency)
	if longConcurrency > 0 {
		fmt.Printf("   ├─ Short workers: %d (max_tokens=%d)\n", concurrency-longConcurrency, cfg.MaxTokens)
		fmt.Printf("   └─ Long workers:  %d (max_tokens=%d)\n", longConcurrency, longMaxTokens)
	}
	fmt.Printf("⏱️  Duration:         %ds\n", duration)
	fmt.Printf("📊 Window Interval:  %ds\n", window)
	fmt.Printf("💻 Metrics Interval: %ds\n", metricsInterval)
	fmt.Printf("📁 Output:           %s\n", outputDir)
	fmt.Println()

	// Get the provider
	p, err := provider.Get(cfg.ProviderType)
	if err != nil {
		log.Fatalf("Error: %v\nAvailable providers: %v", err, provider.List())
	}

	r := soaktest.NewRunner(cfg, soakCfg, p, outputDir)
	report, err := r.Run()
	if err != nil {
		log.Fatalf("Soak test failed: %v", err)
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  Soak Test Complete!                            ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📊 Duration:     %ds\n", report.DurationSec)
	fmt.Printf("📝 Total Reqs:   %d\n", report.TotalRequests)
	fmt.Printf("✅ Success:      %d (%.1f%%)\n", report.TotalSuccess, report.SuccessRate*100)
	fmt.Printf("❌ Failure:      %d\n", report.TotalFailure)
	fmt.Printf("🚀 Overall RPS:  %.2f\n", report.OverallRPS)
	fmt.Printf("⚡ Avg TTFT:     %.0fms\n", report.AvgTTFTMs)
	fmt.Printf("⏱️  Avg Latency:  %.0fms\n", report.AvgLatencyMs)
	fmt.Printf("📊 Windows:      %d\n", len(report.Snapshots))
	fmt.Printf("📁 Results:      %s\n", outputDir)
	fmt.Printf("📄 HTML Report:  %s/soak_report.html\n", outputDir)
	fmt.Printf("📄 JSON Report:  %s/soak_report.json\n", outputDir)
	fmt.Printf("📄 Request Log:  %s/soak_log.jsonl\n", outputDir)
}

func runSoakReportRebuild(inputDir, outputDir string) {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              Soak Report Rebuild Mode                           ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📂 Input dir:  %s\n", inputDir)
	fmt.Printf("📁 Output dir: %s\n", outputDir)
	fmt.Println()

	report, err := soaktest.RebuildReportFromDir(inputDir, outputDir)
	if err != nil {
		log.Fatalf("Failed to rebuild report: %v", err)
	}

	fmt.Println("✅ Report rebuilt successfully!")
	fmt.Println()
	fmt.Printf("📊 Duration:     %ds\n", report.DurationSec)
	fmt.Printf("📝 Total Reqs:   %d\n", report.TotalRequests)
	fmt.Printf("✅ Success:      %d (%.1f%%)\n", report.TotalSuccess, report.SuccessRate*100)
	fmt.Printf("❌ Failure:      %d\n", report.TotalFailure)
	fmt.Printf("🚀 Overall RPS:  %.2f\n", report.OverallRPS)
	fmt.Printf("⚡ Avg TTFT:     %.0fms\n", report.AvgTTFTMs)
	fmt.Printf("⏱️  Avg Latency:  %.0fms\n", report.AvgLatencyMs)
	fmt.Printf("📊 Windows:      %d\n", len(report.Snapshots))
	fmt.Printf("📁 Results:      %s\n", outputDir)
	fmt.Printf("📄 HTML Report:  %s/soak_report.html\n", outputDir)
	fmt.Printf("📄 JSON Report:  %s/soak_report.json\n", outputDir)
}
