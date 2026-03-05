<div align="center">

# LLM Benchmark Kit

**Production-grade benchmarking toolkit for Large Language Model APIs**

Precisely measure **TTFT · Latency · Throughput · P50/P95/P99** and generate self-contained HTML reports with interactive ECharts visualizations.

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-blue)]()
[![Architecture](https://img.shields.io/badge/Arch-amd64%20%7C%20arm64-orange)]()

[English](#features) · [中文](#中文说明)

</div>

---

## Features

<img width="1490" height="864" alt="Full Test Report" src="https://github.com/user-attachments/assets/6f7c599b-23f6-44c3-8082-e1ce0817ab76" />
<img width="1068" height="631" alt="Soak Test Report" src="https://github.com/user-attachments/assets/54058f0f-daf2-4a8d-9768-7667214cf6c1" />

### 6 Test Modes

| Mode | Flag | Description |
|------|------|-------------|
| **Full Test** | `-full-test` | One-click complete test suite: performance + function call + long context + meeting summary |
| **Benchmark** | *(default)* | Standard performance benchmark with configurable concurrency and workload |
| **Soak Test** | `-soak` | Long-running stability test (12h+) with system resource monitoring |
| **Summary Bench** | `-summary-bench` | Concurrent meeting summary stress test |
| **Summary** | `-transcript-file` | Single transcript summarization with chunked processing |
| **Soak Report** | `-soak-report` | Offline report rebuild from downloaded logs |

### Core Capabilities

- **Single Binary Delivery** — `CGO_ENABLED=0` static binary, zero dependencies, runs anywhere
- **Streaming Metrics** — SSE-based TTFT/Latency measurement with precise first-token detection
- **Thinking Model Support** — Auto-detects `reasoning_content` from Qwen3/DeepSeek reasoning models
- **Mixed Workload** — Short requests (256 tokens) + long requests (2048+ tokens) in soak tests
- **Offline HTML Reports** — Self-contained single-file HTML with embedded ECharts, JS, and fonts — no CDN needed
- **Multi-Model Comparison** — `./compare.sh` generates interactive comparison reports across models
- **System Monitoring** — CPU, Memory, GPU utilization/temperature/power tracking during soak tests
- **Error Classification** — Automatic categorization: timeout, rate_limit, service_unavailable, etc.
- **Cross-Platform** — Linux / macOS / Windows × amd64 / arm64, Docker multi-arch
- **Extensible Providers** — Interface + registry pattern for quick LLM API integration

---

## Quick Start

### Installation

```bash
# Build from source
git clone https://github.com/brianxiadong/llm-benchmark-kit.git
cd llm-benchmark-kit
make build

# Or install directly
go install github.com/brianxiadong/llm-benchmark-kit/cmd/llm-benchmark-kit@latest

# Or use Docker
docker pull brianxiadong/llm-benchmark-kit:latest
```

### Usage Examples

#### 1. Full Test (Recommended)

Run the complete test suite in one command — performance benchmark, function call verification, long context test (1K~32K), and meeting summary generation:

```bash
./bin/llm-benchmark-kit -full-test \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model-name \
  -token $API_KEY \
  -insecure  # optional: skip TLS verification
```

Output:
- Performance report (TTFT / Latency / RPS with percentiles)
- Function Call test (tool use capability verification)
- Long Context test (1K~32K character context performance)
- Meeting Summary test (built-in transcript processing)
- Unified reports: `full_test_report.html` + `full_test_report.md`

#### 2. Benchmark

Standard performance test with custom concurrency and workload:

```bash
# Basic benchmark
./bin/llm-benchmark-kit \
  -url https://api.openai.com/v1/chat/completions \
  -model gpt-4o \
  -token $OPENAI_API_KEY \
  -total-requests 100 \
  -concurrency 10

# Custom workload file with warmup
./bin/llm-benchmark-kit \
  -url https://your-api/v1/chat/completions \
  -model your-model \
  -token $API_KEY \
  -workload-file prompts.txt \
  -total-requests 1000 \
  -concurrency 50 \
  -warmup 10 \
  -out ./benchmark-results
```

#### 3. Soak Test (Stability / Endurance)

Long-running stability test to detect memory leaks, performance degradation, and service instability:

```bash
# 50 concurrent workers for 12 hours
./bin/llm-benchmark-kit -soak \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model \
  -token $API_KEY \
  -soak-duration 43200 \
  -soak-concurrency 50 \
  -soak-window 60 \
  -soak-metrics-interval 30
```

**Mixed workload** (recommended) — 40 short-request workers + 10 long-request workers to simulate production traffic:

```bash
./bin/llm-benchmark-kit -soak \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model \
  -token $API_KEY \
  -soak-duration 43200 \
  -soak-concurrency 50 \
  -soak-long-concurrency 10 \
  -soak-long-max-tokens 2048 \
  -soak-window 60 \
  -soak-metrics-interval 30
```

Startup output:
```
👥 Concurrency:      50
   ├─ Short workers: 40 (max_tokens=256)
   └─ Long workers:  10 (max_tokens=2048)
```

Real-time monitoring:
```
[Soak] Window #5 | Requests: 305 | Success: 100.0% | AvgLatency: 8891ms | AvgTTFT: 124ms | RPS: 5.1
[Soak] System: CPU: 12.3% | Mem: 28012/1028470MB (2.7%)
```

#### 4. Summary Bench (Meeting Summary Stress Test)

Concurrent stress test for meeting summary generation:

```bash
./bin/llm-benchmark-kit -summary-bench \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model-name \
  -token $API_KEY \
  -sb-concurrency 10 \
  -sb-requests 50 \
  -chunk-size 8000
```

#### 5. Single Transcript Summary

Process a meeting transcript with chunk-based summarization:

```bash
./bin/llm-benchmark-kit \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model \
  -transcript-file ./meeting_transcript.txt \
  -chunk-size 8000 \
  -meeting-time "2026-01-22 10:00"
```

#### 6. Soak Report Rebuild

Rebuild HTML reports offline from downloaded server logs — no LLM API connection needed:

```bash
# Download logs from remote server
scp -r root@server:/path/to/output/soaktest_xxx ./local/

# Rebuild report
./bin/llm-benchmark-kit -soak-report ./local/soaktest_xxx

# Or specify custom output directory
./bin/llm-benchmark-kit -soak-report ./local/soaktest_xxx -soak-report-output ./reports/
```

#### 7. Multi-Model Comparison

Generate comparison reports after running Full Tests across multiple models:

```bash
# Run Full Test for each model
./bin/llm-benchmark-kit -full-test -url http://api1/v1 -model model-a -token $KEY1
./bin/llm-benchmark-kit -full-test -url http://api2/v1 -model model-b -token $KEY2

# Generate comparison report
./compare.sh

# Filter by pattern
./compare.sh deepseek    # compare only results matching 'deepseek'
```

Comparison report includes: TTFT/Latency/Throughput bar charts, radar chart, long context TTFT curves, latency distribution box plots, function call capability matrix, and summary performance comparison.

---

## CLI Reference

### Common Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | *(required)* | API endpoint URL |
| `-model` | *(required)* | Model name |
| `-token` | | Bearer token for authentication |
| `-timeout` | 60 | Request timeout in seconds |
| `-insecure` | false | Skip TLS certificate verification |
| `-ca-cert` | | Custom CA certificate file path |
| `-provider` | openai | Provider type (openai, aliyun, custom) |
| `-verbose` | false | Show detailed request/response logs |

### Mode Selection

| Flag | Description |
|------|-------------|
| `-full-test` | Complete test suite (performance + function call + long context + summary) |
| `-summary-bench` | Meeting summary concurrent stress test |
| `-soak` | Soak endurance test (long-running stability) |
| `-soak-report <dir>` | Rebuild soak report from logs (offline) |
| `-transcript-file <file>` | Single transcript summary mode |
| *(default)* | Benchmark mode |

### Benchmark Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-concurrency` | 1 | Number of concurrent workers |
| `-total-requests` | 10 | Total requests to send |
| `-duration` | 0 | Duration-based testing in seconds (alternative to total-requests) |
| `-rps` | 0 | Requests per second limit (0 = unlimited) |
| `-warmup` | 0 | Warmup requests excluded from statistics |
| `-max-tokens` | 256 | Maximum response tokens |
| `-token-mode` | usage | Token counting: `usage` / `chars` / `disabled` |
| `-workload-file` | | Path to prompts file (plain text or JSONL) |
| `-out` | ./output | Output directory |

### Soak Test Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-soak` | false | Enable soak test mode |
| `-soak-duration` | 300 | Test duration in seconds (12h = 43200) |
| `-soak-concurrency` | 5 | Total concurrent workers |
| `-soak-window` | 30 | Snapshot window interval in seconds |
| `-soak-metrics-interval` | 10 | System metrics collection interval in seconds |
| `-soak-long-concurrency` | 0 | Long-request worker count (0 = all short) |
| `-soak-long-max-tokens` | 2048 | Max tokens for long requests |
| `-soak-report-output` | *(input dir)* | Output directory for rebuilt report |

### Summary Bench Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-sb-concurrency` | 5 | Concurrent workers |
| `-sb-requests` | 20 | Total requests |
| `-chunk-size` | 8000 | Transcript chunk size in characters |

### Summary Parameters

| Flag | Default | Description |
|------|---------|-------------|
| `-transcript-file` | | Meeting transcript file path |
| `-chunk-size` | 8000 | Max characters per chunk |
| `-meeting-time` | *(now)* | Meeting time for report header |

---

## Output Files

### Full Test

```
output/fulltest_{model}_{timestamp}/
├── full_test_report.md          # Markdown summary
├── full_test_report.html        # Interactive HTML report (dark theme, ECharts)
├── request_response.log         # Full request/response log
├── benchmark/                   # Phase 1: Performance
│   ├── results.jsonl
│   ├── summary.json
│   └── report.html
├── function_call/               # Phase 2: Function Call
├── long_context/                # Phase 3: Long Context
└── summary/                     # Phase 4: Meeting Summary
    ├── meeting_summary.md
    ├── performance_report.md
    ├── performance_metrics.json
    └── intermediate/
```

### Soak Test

```
output/soaktest_{model}_{timestamp}/
├── soak_report.html             # ECharts interactive report (dark theme)
├── soak_report.md               # Text summary
├── soak_report.json             # Full JSON data
├── soak_log.jsonl               # Per-request log (with workload_type)
└── snapshots.jsonl              # Time-window snapshots
```

Charts in HTML report: Latency trends (Avg/P50/P95/P99), TTFT trends, RPS throughput, success rate, CPU/Memory usage, GPU utilization/VRAM/temperature/power (if nvidia-smi available).

### Benchmark

```
output/{model}_{timestamp}/
├── results.jsonl                # Per-request details
├── summary.json                 # Aggregated statistics
└── report.html                  # Interactive HTML report
```

### Summary Bench

```
output/summarybench_{model}_{timestamp}/
├── summary_bench_report.json
└── summary_bench_report.md
```

### Comparison

```
local/comparison_{timestamp}.html    # Multi-model comparison report
```

---

## Metrics Guide

### Core Metrics

| Metric | Full Name | Description |
|--------|-----------|-------------|
| **TTFT** | Time To First Token | Time from request to first content token. Key user-experience metric. |
| **Latency** | End-to-End Latency | Total time from request to complete response (TTFT + generation time). |
| **Throughput** | Generation Speed | Tokens per second (tokens/s) or characters per second (chars/s). |
| **RPS** | Requests Per Second | Successfully completed requests per second. Service capacity metric. |
| **Success Rate** | — | Ratio of successful requests to total requests. |

### Percentile Metrics

| Metric | Description |
|--------|-------------|
| **P50** (Median) | 50% of requests complete within this time. Typical experience. |
| **P95** | 95% of requests complete within this time. Most-user experience. |
| **P99** | 99% of requests complete within this time. Tail latency indicator. |

### Example Output

```
Avg TTFT:     84.67 ms   → First token in ~85ms, feels responsive
Avg Latency:  1890.70 ms → Full response in ~1.9s
P50 TTFT:     77 ms      → Half of requests start output within 77ms
P95 TTFT:     122 ms     → 95% start within 122ms
P99 TTFT:     137 ms     → 99% start within 137ms (no severe tail latency)
RPS:          4.77       → ~4.8 requests handled per second
Throughput:   501.28/s   → ~500 tokens generated per second
```

### Token Counting Modes

| Mode | Description |
|------|-------------|
| `usage` | Uses API `usage` field (most accurate) |
| `chars` | Character-based counting when API doesn't report token usage |
| `disabled` | Skip token counting, measure latency only |

---

## Build

```bash
make build               # Current platform
make build-all           # All platforms (Linux/macOS/Windows × amd64/arm64)
make build-linux-amd64   # Specific target
make test                # Run tests
make test-coverage       # Tests with coverage report
make lint                # Run golangci-lint
make docker              # Build Docker image
make docker-multi        # Multi-arch Docker image
make clean               # Clean build artifacts
```

## Docker

```bash
# Build
docker build -t llm-benchmark-kit .

# Run
docker run --rm llm-benchmark-kit -full-test \
  -url http://your-api/v1/chat/completions \
  -model your-model \
  -token $API_KEY

# Mount output directory
docker run --rm -v $(pwd)/output:/app/output llm-benchmark-kit \
  -url http://your-api/v1/chat/completions \
  -model your-model \
  -total-requests 100 -concurrency 10
```

---

## Project Structure

```
.
├── cmd/llm-benchmark-kit/       # CLI entrypoint
├── pkg/
│   ├── config/                  # Configuration definitions
│   ├── provider/                # Provider interface + registry
│   │   └── openai/              # OpenAI-compatible provider (supports reasoning_content)
│   ├── runner/                  # Benchmark engine (worker pool)
│   │   └── templates/           # Benchmark HTML templates
│   ├── fulltest/                # Full Test orchestrator
│   │   ├── templates/           # Full Test HTML templates
│   │   └── assets/              # Embedded JS / fonts
│   ├── soaktest/                # Soak Test engine
│   │   ├── soaktest.go          # Core runner (mixed workload scheduling)
│   │   ├── snapshot.go          # Time-window aggregation & error classification
│   │   ├── sysmetrics.go        # System resource collector (CPU/Mem/GPU)
│   │   ├── report.go            # Report generation & offline rebuild
│   │   └── templates/           # ECharts HTML templates
│   ├── summarizer/              # Meeting summary generator
│   ├── summarybench/            # Summary concurrent benchmark
│   ├── workload/                # Workload definitions (short/long prompt generation)
│   ├── sse/                     # Server-Sent Events parser
│   ├── stats/                   # Statistical utilities
│   ├── result/                  # Result types
│   ├── embedded/                # Embedded resources (sample transcript)
│   ├── assets/                  # Asset management
│   └── progress/                # Progress tracking
├── tools/
│   └── compare/                 # Multi-model comparison report (Python + Plotly)
│       ├── compare_report.py
│       └── requirements.txt
├── compare.sh                   # One-click comparison script
├── Dockerfile                   # Multi-stage Docker build
└── Makefile                     # Build system
```

---

## 中文说明

LLM Benchmark Kit 是一款专业的大语言模型 API 基准测试工具，支持 6 种测试模式：

- **一键完整测试** (`-full-test`)：自动运行性能 + Function Call + 长上下文 + 会议纪要测试
- **标准压测** (默认模式)：可配置并发、QPS 限制、预热请求
- **Soak 耐久测试** (`-soak`)：支持 12h+ 长时间稳定性测试，混合负载，实时系统资源监控
- **会议纪要压测** (`-summary-bench`)：并发会议纪要生成压力测试
- **会议纪要生成** (`-transcript-file`)：长文本分片处理，增量式总结
- **报告离线重建** (`-soak-report`)：从服务器下载日志后本地重建 HTML 报告

核心特性：CGO=0 静态二进制零依赖交付 · 精准 SSE 流式 TTFT 测量 · 自动识别 Qwen3/DeepSeek Thinking 模型 · 离线单文件 HTML 报告（内嵌 ECharts） · 多模型对比分析 · 多架构 Docker 支持。

---

## License

[MIT License](LICENSE)
