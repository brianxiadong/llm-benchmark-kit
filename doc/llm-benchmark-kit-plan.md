# Go High-Performance LLM Benchmark Kit — Project Plan (Optimized)

> 目标：构建一个可交付到客户环境的 **单文件 / 高性能 / 流式友好** 的 LLM Benchmark 工具，精准统计 **TTFT / Latency / Throughput / P50/P95/P99**，并生成 **离线可打开** 的自包含 HTML 报告。  
> 交付形态：**静态二进制（amd64/arm64, Linux/macOS）+ 多架构 Docker**。

---

## 1. Project Overview

### 1.1 Goals
- **单文件交付**：CGO=0 静态二进制，客户环境直接运行（Docker/Linux/Mac）。
- **精准流式指标**：支持 SSE（Server-Sent Events）流式处理，可靠计算 TTFT/Latency。
- **多架构支持**：amd64 / arm64 原生支持；Docker multi-arch。
- **高扩展性适配**：Provider 接口 + 注册表机制，快速适配阿里云/百度云/私有化网关/签名鉴权。
- **离线报告**：生成 **单文件 HTML**（内嵌 ECharts/JS/数据），不依赖外网 CDN。
- **可追溯结果**：输出 JSONL/CSV 原始明细 + summary 汇总 + report.html 报告。

---

## 2. Tech Stack

- Language: **Go 1.21+**
- Concurrency: Goroutines + Channels + sync.WaitGroup
- HTTP: `net/http` 标准库
- Streaming SSE: `bufio.Reader`（**禁止使用 bufio.Scanner 默认 64K 限制**）
- Template & Assets: `html/template` + `//go:embed`
- External deps: **0（默认）**  
  - 可选增强（未来）：引入轻量 histogram/percentile lib（v2 可讨论）

---

## 3. Metrics Definition（务必统一口径）

> 指标口径必须稳定，否则“微调前后/多模型/多客户”不可比。

### 3.1 Core Metrics

| Metric | Definition | Notes |
|---|---|---|
| TTFT | 从请求发出到 **首个“可见内容帧”** 到达的时间 | “可见内容帧”由 Provider 定义（如 OpenAI `delta.content` 首次非空） |
| Latency | 从请求发出到 **明确结束信号** 的时间；若无结束信号则 EOF | 优先 `[DONE]` / `finish_reason`，否则连接关闭 |
| Decode Time (Optional) | `end - first_content_time` | 用于区分“首包慢 vs 生成慢” |
| Token Throughput | `total_output_tokens / wall_time` | wall_time = 整个压测任务墙钟时间 |
| Per-Request TPS (Optional) | `out_tokens / decode_time` | 有助于看分布（P50/P95） |
| RPS | `success_requests / wall_time` | 客户经常会问 |
| Percentiles | P50/P95/P99（TTFT & Latency 分开） | 小样本需标注“统计意义有限” |
| Success Rate | 成功请求 / 总请求 | HTTP 非 200、解析失败、超时等均计失败 |

### 3.2 Token Counting Strategy（关键：默认不要“按单词估算 token”）
提供三档模式（CLI 控制）：
- `--token-mode=usage`（默认推荐）：优先使用响应中的 `usage.prompt_tokens / completion_tokens`（若 Provider 支持）。
- `--token-mode=chars`：无法获取 usage 时，使用 `chars/s` 输出（并在报告中明确标注非 token 指标）。
- `--token-mode=disabled`：不统计 token（只看 TTFT/Latency/成功率）。

> v1 不建议默认“估算 tokens”，因为不同语言/分词差异会严重污染结果；如需可做 v2 可选插件。

---

## 4. Architecture

### 4.1 Data Flow

1. CLI 解析配置（并发、总请求、RPS、超时、provider、workload）
2. Workload 生成单次请求输入（prompt/messages）
3. Worker Pool 按并发/RPS 发请求
4. Provider 执行请求并解析流式事件
5. Runner 记录 timestamps（start/first_content/end）并累计 tokens/bytes
6. Aggregator 汇总统计（均值、分位数、吞吐、错误分类）
7. 生成输出：`results.jsonl` + `summary.json` + `report.html`

### 4.2 Key Interfaces

#### Provider Interface（适配层边界）
```go
type Provider interface {
    Name() string

    // 执行一次请求，并以事件流返回（可用于 SSE / 非流式统一）
    StreamChat(ctx context.Context, cfg *GlobalConfig, input WorkloadInput) (<-chan StreamEvent, error)
}
```

#### StreamEvent（Runner 只关心“何时有内容/何时结束/是否有 usage”）
```go
type StreamEventType int
const (
    EventMeta StreamEventType = iota
    EventContent      // 可见内容（用于 TTFT 判定）
    EventUsage        // prompt/completion token 数
    EventEnd          // 明确结束信号（如 [DONE] / finish_reason）
    EventError
)

type StreamEvent struct {
    Type StreamEventType
    Raw  string // 原始片段（用于采样/排错）
    Text string // content 文本（若有）
    Usage *TokenUsage
    Err  error
}

type TokenUsage struct {
    PromptTokens     int
    CompletionTokens int
}
```

### 4.3 Workload Input（保证可扩展）
```go
type WorkloadInput struct {
    ID       string
    Prompt   string
    Messages []ChatMessage // 可选
    MaxTokens int
}

type ChatMessage struct {
    Role    string
    Content string
}
```

---

## 5. SSE Streaming Implementation Notes（必须按事件块解析）

### 5.1 Why NOT bufio.Scanner
- 默认 64K token 限制，长响应/长 JSON 容易直接失败
- SSE event 是以 **空行 `\n\n`** 分隔，逐行读会拼错多行 data

### 5.2 Recommended Parsing Approach（标准 SSE）
- 使用 `bufio.Reader` 读取，按 `\n\n` 取一个 event block
- event block 内可包含多行 `data:`，需拼接为完整 payload
- 处理：
  - `data: [DONE]` => `EventEnd`
  - `:` 注释/keep-alive 行 => 忽略
  - 非 200 状态码 => EventError（并记录 body）

---

## 6. Reporting（离线自包含单文件 HTML）

### 6.1 Output Artifacts
- `results.jsonl`：每请求一行，包含：
  - request_id, start_ts, first_content_ts, end_ts, ttft_ms, latency_ms, status, err, in_bytes, out_bytes, out_tokens(可选), provider
- `summary.json`：聚合统计（均值、分位数、吞吐、错误分布）
- `report.html`：单文件，内嵌 JS（ECharts）+ 数据（summary + 分布数组）+ 采样

### 6.2 Offline Requirement
- 禁止 CDN：将 `echarts.min.js` 放入仓库并 `//go:embed` 内嵌
- HTML 内用 `<script>` 内嵌 ECharts 和数据

### 6.3 What to Visualize (v1)
- Latency 分布直方图（P50/P95/P99 标线）
- TTFT 分布直方图（P50/P95/P99 标线）
- Success/Failure 饼图 + 错误 TopN（按错误类型/HTTP 码/超时分类）
- 可选：并发/时间序列折线（若支持 duration 模式）

### 6.4 Sampling Strategy（更稳）
采样两条（而不是只取第一帧）：
- `FirstContentRaw`：首个内容帧 raw（用于展示“结构样例”）
- `FinalFrameRaw`：结束帧 raw（可能包含 usage/finish_reason）

并增加大小上限（如 64KB）避免报告膨胀。

---

## 7. Benchmark Scheduling（并发 + 可选 RPS + Warmup）

### 7.1 CLI Parameters（建议）
- `--provider`：openai / aliyun / custom
- `--url`、`--model`、`--token`、`--timeout`
- `--concurrency`：并发 worker 数
- `--total-requests`：总请求数（与 duration 二选一）
- `--duration`：运行时长（可选）
- `--rps`：到达率控制（可选；为 0 表示尽快打完）
- `--warmup`：预热请求数（不计入统计）
- `--token-mode`：usage / chars / disabled
- `--workload-file`：prompts 文件（每行一个 prompt 或 JSONL）
- `--insecure` / `--ca-cert`：TLS 控制
- `--out`：输出目录

### 7.2 Worker Pool Model
- jobs channel 产生 WorkloadInput
- results channel 收集 RequestResult
- 支持两种驱动：
  - **尽快打完**：一次性塞满 jobs
  - **RPS 控制**：ticker 以固定速率投喂 jobs

---

## 8. Data Structures（v1）

```go
type GlobalConfig struct {
    URL           string
    ModelName     string
    Token         string
    Concurrency   int
    TotalRequests int
    DurationSec   int
    RPS           float64
    Warmup        int
    MaxTokens     int
    TokenMode     string // usage|chars|disabled

    TimeoutSec    int
    InsecureTLS   bool
    CACertPath    string

    WorkloadFile  string
    OutputDir     string
    ProviderType  string
}

type RequestResult struct {
    ID        string
    Status    string // ok|http_error|timeout|parse_error
    TTFT      time.Duration
    Latency   time.Duration
    Decode    time.Duration // optional
    OutTokens int
    OutChars  int
    Err       string
}

type BenchmarkReport struct {
    Provider     string
    StartedAt    string
    WallTimeMs   int64

    Success      int
    Failure      int
    SuccessRate  float64

    AvgTTFTMs    float64
    AvgLatencyMs float64

    P50TTFTMs    int64
    P95TTFTMs    int64
    P99TTFTMs    int64

    P50LatencyMs int64
    P95LatencyMs int64
    P99LatencyMs int64

    TokenMode        string
    TokenThroughput  float64 // tokens/s 或 chars/s
    RPS              float64

    FirstContentRaw  string
    FinalFrameRaw    string

    ErrorsTopN []ErrorStat
}

type ErrorStat struct {
    Key   string
    Count int
}
```

---

## 9. Implementation Phases（带可直接给 AI 的 Prompt）

> 建议每个 Phase 结束都 `go test ./...` + `go run` 做一次 e2e。

### Phase 1 — Foundation (CLI + Config + Interfaces)
**Goal**：定义配置、数据结构、Provider 接口、基础 CLI（flag 或 cobra，v1 用 flag 即可）。

### Phase 2 — SSE Engine (OpenAIProvider + Robust SSE Parsing)
**Goal**：实现 OpenAI SSE provider：可靠解析 event block、判定首个 content、识别 end、采样 raw。

### Phase 3 — Runner & Worker Pool (Concurrency + Optional RPS + Warmup)
**Goal**：并发调度、计时、超时、取消、warmup 剔除。

### Phase 4 — Statistics (Percentiles + Error Breakdown)
**Goal**：汇总统计、分位数（TTFT/Latency）、吞吐（token/char）。

### Phase 5 — Offline HTML Report (Single-file)
**Goal**：生成 report.html（内嵌 echarts.min.js + 数据 + template）。

### Phase 6 — Build & Ship (Cross Compile + Multi-arch Docker)
**Goal**：输出 linux/amd64 + linux/arm64 静态二进制；Docker multi-arch。

---

## 10. Customization Guide（客户适配）

### 10.1 How to Add a Provider
1. 新建 `provider_xxx.go` 实现 Provider 接口
2. 在 `provider_registry.go` 注册：
```go
var registry = map[string]func() Provider{
    "openai": func() Provider { return &OpenAIProvider{} },
    "aliyun": func() Provider { return &AliyunProvider{} },
}
```
3. Provider 内处理：
- 鉴权 header（token / HMAC / 签名）
- payload 格式映射
- 结束信号/可见内容帧判定

### 10.2 Provider Should Own These Differences
- “什么算首个内容帧”（TTFT 判定）
- “什么算结束”（Latency 终止）
- “usage 如何提取”（token 统计）

---

## 11. Testing Strategy（强烈建议 v1 就做）

### 11.1 Mock SSE Server（本地可重复测）
实现一个本地 HTTP SSE mock：
- 延迟 200ms 才发首个 content
- 每 50ms 发一帧，发 20 帧
- 末尾发 `[DONE]`

用于验证：
- TTFT ≈ 200ms（允许误差范围）
- Latency ≈ 200ms + 50ms*(20-1) + 网络开销
- runner/percentile/report 均正确

### 11.2 Regression Checks
- 长 event > 64KB（确保不会被 Scanner 限制）
- 多行 data: 拼接
- keep-alive 行（`: ping`）
- 非 200 / 超时 / 断流

---

## 12. Deliverables
- ✅ `llm-benchmark-kit` 二进制（linux/amd64, linux/arm64, darwin 可选）
- ✅ `results.jsonl` 明细
- ✅ `summary.json` 汇总
- ✅ `report.html` 自包含离线报告
- ✅ Docker multi-arch 镜像（可选）

---

## 13. Open Questions（开工前确认）
- 客户接口是否 OpenAI-compatible（SSE 格式与结束信号）？
- token 统计是否必须精确（usage 是否可得）？如果不可得，是否接受 chars/s？
- 主要压测方式：固定并发？还是需要 rps/duration 模式（建议保留）？
