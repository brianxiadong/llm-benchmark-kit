// Package fulltest provides a complete test runner that executes both
// performance benchmark and meeting summary tests in sequence.
package fulltest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/config"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/provider"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/result"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/runner"
	"github.com/brianxiadong/llm-benchmark-kit/pkg/summarizer"
)

// FullTestReport contains the combined results from all test phases.
type FullTestReport struct {
	ModelName          string                     `json:"model_name"`
	APIURL             string                     `json:"api_url"`
	StartTime          time.Time                  `json:"start_time"`
	EndTime            time.Time                  `json:"end_time"`
	TotalDuration      time.Duration              `json:"total_duration"`
	BenchmarkReport    *result.BenchmarkReport    `json:"benchmark_report"`
	SummaryMetrics     *summarizer.SummaryMetrics `json:"summary_metrics,omitempty"`
	BenchmarkOutputDir string                     `json:"benchmark_output_dir"`
	SummaryOutputDir   string                     `json:"summary_output_dir"`
}

// Runner executes the full test suite.
type Runner struct {
	cfg            *config.GlobalConfig
	transcriptFile string
	outputDir      string
	p              provider.Provider
}

// NewRunner creates a new full test runner.
func NewRunner(cfg *config.GlobalConfig, p provider.Provider, transcriptFile, outputDir string) *Runner {
	return &Runner{
		cfg:            cfg,
		p:              p,
		transcriptFile: transcriptFile,
		outputDir:      outputDir,
	}
}

// Run executes the full test suite and returns the combined report.
func (r *Runner) Run() (*FullTestReport, error) {
	report := &FullTestReport{
		ModelName: r.cfg.ModelName,
		APIURL:    r.cfg.URL,
		StartTime: time.Now(),
	}

	// Create output directory
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              LLM Benchmark Kit - Full Test Mode                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📋 Model:     %s\n", r.cfg.ModelName)
	fmt.Printf("🔗 URL:       %s\n", r.cfg.URL)
	fmt.Printf("📁 Output:    %s\n", r.outputDir)
	fmt.Println()

	// Phase 1: Performance Benchmark
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 Phase 1: Performance Benchmark")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	benchmarkDir := filepath.Join(r.outputDir, "benchmark")
	benchmarkReport, err := r.runBenchmark(benchmarkDir)
	if err != nil {
		return nil, fmt.Errorf("benchmark failed: %w", err)
	}
	report.BenchmarkReport = benchmarkReport
	report.BenchmarkOutputDir = benchmarkDir
	fmt.Println("✅ Phase 1 Complete!")
	fmt.Println()

	// Phase 2: Meeting Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📝 Phase 2: Meeting Summary Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	summaryDir := filepath.Join(r.outputDir, "summary")
	summaryMetrics, err := r.runSummary(summaryDir)
	if err != nil {
		fmt.Printf("⚠️  Summary test failed: %v\n", err)
		// Continue even if summary fails
	} else {
		report.SummaryMetrics = summaryMetrics
		report.SummaryOutputDir = summaryDir
		fmt.Println("✅ Phase 2 Complete!")
	}
	fmt.Println()

	// Finalize report
	report.EndTime = time.Now()
	report.TotalDuration = report.EndTime.Sub(report.StartTime)

	// Generate final report
	if err := r.generateFinalReport(report); err != nil {
		return nil, fmt.Errorf("failed to generate final report: %w", err)
	}

	return report, nil
}

// runBenchmark runs the performance benchmark phase.
func (r *Runner) runBenchmark(outputDir string) (*result.BenchmarkReport, error) {
	// Create a copy of config for benchmark
	benchCfg := *r.cfg
	benchCfg.OutputDir = outputDir

	fmt.Printf("   Concurrency:  %d\n", benchCfg.Concurrency)
	fmt.Printf("   Requests:     %d\n", benchCfg.TotalRequests)
	fmt.Printf("   Warmup:       %d\n", benchCfg.Warmup)
	fmt.Printf("   Max Tokens:   %d\n", benchCfg.MaxTokens)
	fmt.Println()

	benchRunner := runner.New(&benchCfg, r.p)
	return benchRunner.Run()
}

// runSummary runs the meeting summary phase.
func (r *Runner) runSummary(outputDir string) (*summarizer.SummaryMetrics, error) {
	// Check if transcript file exists
	if _, err := os.Stat(r.transcriptFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("transcript file not found: %s", r.transcriptFile)
	}

	fmt.Printf("   Transcript:   %s\n", r.transcriptFile)
	fmt.Printf("   Chunk Size:   8000 chars\n")
	fmt.Println()

	meetingTime := time.Now().Format("2006-01-02 15:04")
	sum := summarizer.NewSummarizer(r.cfg, 8000, meetingTime)

	_, err := sum.Run(r.transcriptFile, outputDir)
	if err != nil {
		return nil, err
	}

	// Load the metrics from the generated JSON file
	// For now, we return nil as the summarizer already saves the metrics
	return nil, nil
}

// generateFinalReport creates the combined markdown report.
func (r *Runner) generateFinalReport(report *FullTestReport) error {
	var sb strings.Builder

	sb.WriteString("# LLM 完整测试报告\n\n")
	sb.WriteString(fmt.Sprintf("**生成时间**: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// Basic Info
	sb.WriteString("## 基本信息\n\n")
	sb.WriteString("| 项目 | 值 |\n")
	sb.WriteString("|------|----|\n")
	sb.WriteString(fmt.Sprintf("| 模型名称 | %s |\n", report.ModelName))
	sb.WriteString(fmt.Sprintf("| API URL | %s |\n", report.APIURL))
	sb.WriteString(fmt.Sprintf("| 开始时间 | %s |\n", report.StartTime.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("| 结束时间 | %s |\n", report.EndTime.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("| 总耗时 | %.2f 秒 |\n", report.TotalDuration.Seconds()))
	sb.WriteString("\n")

	// Benchmark Results
	sb.WriteString("## 性能测试结果\n\n")
	if report.BenchmarkReport != nil {
		br := report.BenchmarkReport
		sb.WriteString("### 总体指标\n\n")
		sb.WriteString("| 指标 | 值 |\n")
		sb.WriteString("|------|----|\\n")
		sb.WriteString(fmt.Sprintf("| 成功率 | %.2f%% (%d/%d) |\n", br.SuccessRate*100, br.Success, br.TotalRequests))
		sb.WriteString(fmt.Sprintf("| 失败数 | %d |\n", br.Failure))
		sb.WriteString(fmt.Sprintf("| RPS | %.2f |\n", br.RPS))
		sb.WriteString(fmt.Sprintf("| Token 吞吐量 | %.2f tokens/s |\n", br.TokenThroughput))
		sb.WriteString("\n")

		// Show error statistics if there are failures
		if br.Failure > 0 && len(br.ErrorsTopN) > 0 {
			sb.WriteString("### ⚠️ 错误统计\n\n")
			sb.WriteString("> [!WARNING]\n")
			sb.WriteString(fmt.Sprintf("> 测试中发生了 %d 次错误\n\n", br.Failure))
			sb.WriteString("| 错误类型 | 次数 |\n")
			sb.WriteString("|----------|------|\n")
			for _, errStat := range br.ErrorsTopN {
				// Truncate long error messages
				errKey := errStat.Key
				if len(errKey) > 100 {
					errKey = errKey[:100] + "..."
				}
				// Escape pipe characters for markdown table
				errKey = strings.ReplaceAll(errKey, "|", "\\|")
				sb.WriteString(fmt.Sprintf("| %s | %d |\n", errKey, errStat.Count))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("### 延迟指标 (ms)\n\n")
		sb.WriteString("| 指标 | TTFT | 总延迟 |\n")
		sb.WriteString("|------|------|--------|\n")
		sb.WriteString(fmt.Sprintf("| 平均值 | %.2f | %.2f |\n", br.AvgTTFTMs, br.AvgLatencyMs))
		sb.WriteString(fmt.Sprintf("| P50 | %d | %d |\n", br.P50TTFTMs, br.P50LatencyMs))
		sb.WriteString(fmt.Sprintf("| P95 | %d | %d |\n", br.P95TTFTMs, br.P95LatencyMs))
		sb.WriteString(fmt.Sprintf("| P99 | %d | %d |\n", br.P99TTFTMs, br.P99LatencyMs))
		sb.WriteString("\n")

		sb.WriteString(fmt.Sprintf("📁 详细报告: [benchmark/report.md](%s/report.md)\n\n", report.BenchmarkOutputDir))
	} else {
		sb.WriteString("⚠️ 性能测试未完成\n\n")
	}

	// Summary Results
	sb.WriteString("## 会议纪要测试结果\n\n")
	if report.SummaryOutputDir != "" {
		sb.WriteString(fmt.Sprintf("📁 会议纪要: [summary/meeting_summary.md](%s/meeting_summary.md)\n", report.SummaryOutputDir))
		sb.WriteString(fmt.Sprintf("📊 性能报告: [summary/performance_report.md](%s/performance_report.md)\n\n", report.SummaryOutputDir))
	} else {
		sb.WriteString("⚠️ 会议纪要测试未完成或出错\n\n")
	}

	// Output Files
	sb.WriteString("## 输出文件清单\n\n")
	sb.WriteString("```\n")
	sb.WriteString(fmt.Sprintf("%s/\n", r.outputDir))
	sb.WriteString("├── full_test_report.md    # 本汇总报告\n")
	sb.WriteString("├── benchmark/             # 性能测试结果\n")
	sb.WriteString("│   ├── report.json\n")
	sb.WriteString("│   ├── report.md\n")
	sb.WriteString("│   └── requests.csv\n")
	sb.WriteString("└── summary/               # 会议纪要结果\n")
	sb.WriteString("    ├── meeting_summary.md\n")
	sb.WriteString("    ├── performance_report.md\n")
	sb.WriteString("    ├── performance_metrics.json\n")
	sb.WriteString("    └── intermediate/\n")
	sb.WriteString("```\n")

	// Write report
	reportPath := filepath.Join(r.outputDir, "full_test_report.md")
	if err := os.WriteFile(reportPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📋 Phase 3: Final Report Generated")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("📄 Report: %s\n", reportPath)

	return nil
}
