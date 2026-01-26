# LLM Benchmark Kit

高性能 LLM 基准测试工具，用于精准统计 **TTFT / Latency / Throughput / P50/P95/P99**，并生成离线可打开的自包含 HTML 报告。支持一键运行完整测试套件。

## 特性
<img width="1490" height="864" alt="image" src="https://github.com/user-attachments/assets/6f7c599b-23f6-44c3-8082-e1ce0817ab76" />
<img width="1068" height="631" alt="image" src="https://github.com/user-attachments/assets/54058f0f-daf2-4a8d-9768-7667214cf6c1" />

- **一键完整测试**：`-full-test` 模式自动运行性能测试 + Function Call 测试 + 长上下文测试 + 会议纪要生成测试
- **会议纪要并发压测**：`-summary-bench` 模式支持自定义并发数的会议纪要压力测试
- **多模型对比报告**：`./compare.sh` 一键生成多模型对比分析 HTML 报告
- **单文件交付**：CGO=0 静态二进制，客户环境直接运行
- **精准流式指标**：支持 SSE 流式处理，可靠计算 TTFT/Latency
- **多架构支持**：amd64/arm64 原生支持，Docker multi-arch
- **高扩展性**：Provider 接口 + 注册表机制，快速适配各类 LLM API
- **离线报告**：单文件 HTML（内嵌 ECharts/JS/数据），不依赖外网 CDN
- **会议纪要生成**：支持长文本分片处理，增量式会议纪要总结

## 快速开始

### 安装

```bash
# 从源码构建
git clone https://github.com/brianxiadong/llm-benchmark-kit.git
cd llm-benchmark-kit
make build

# 或使用 go install
go install github.com/brianxiadong/llm-benchmark-kit/cmd/llm-benchmark-kit@latest
```

### 使用示例

#### 1. Full Test 模式（推荐）

一键运行完整测试套件，包括性能测试和会议纪要生成测试：

```bash
./bin/llm-benchmark-kit -full-test \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model-name \
  -token $API_KEY \
  -insecure  # 如需跳过 TLS 验证
```

输出包含：
- 性能测试报告（TTFT/Latency/RPS 等指标）
- Function Call 测试（函数调用能力验证）
- 长上下文测试（1K~32K 字符上下文性能）
- 会议纪要生成测试（使用内置测试文本）
- 汇总报告 `full_test_report.md` 和 `full_test_report.html`

#### 2. Summary Benchmark 模式（会议纪要并发压测）

对会议纪要生成进行并发压力测试，测试大模型在高并发场景下的吞吐能力：

```bash
./bin/llm-benchmark-kit -summary-bench \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model-name \
  -token $API_KEY \
  -sb-concurrency 10 \
  -sb-requests 50 \
  -chunk-size 8000
```

输出包含：
- 每请求 tokens/s 吞吐量统计（Avg/P50/P95/P99）
- 延迟分布统计
- 成功率、RPS
- 详细报告 `summary_bench_report.md` 和 `summary_bench_report.json`

#### 3. Benchmark 模式

单独运行性能基准测试：

```bash
# 基础测试
./bin/llm-benchmark-kit \
  -url https://api.openai.com/v1/chat/completions \
  -model gpt-3.5-turbo \
  -token $OPENAI_API_KEY \
  -total-requests 100 \
  -concurrency 10

# 自定义工作负载
./bin/llm-benchmark-kit \
  -url https://your-llm-api.com/v1/chat/completions \
  -model your-model \
  -token $API_KEY \
  -workload-file prompts.txt \
  -total-requests 1000 \
  -concurrency 50 \
  -warmup 10 \
  -out ./benchmark-results
```

#### 4. Summary 模式

会议纪要生成测试：

```bash
./bin/llm-benchmark-kit \
  -url http://your-llm-api/v1/chat/completions \
  -model your-model \
  -transcript-file ./meeting_transcript.txt \
  -chunk-size 8000 \
  -meeting-time "2026-01-22 10:00"
```

#### 5. 多模型对比报告

运行多次 Full Test 后，一键生成多模型对比分析报告：

```bash
# 先运行多个模型的 Full Test
./bin/llm-benchmark-kit -full-test -url http://api1/v1 -model model-a -token $KEY1
./bin/llm-benchmark-kit -full-test -url http://api2/v1 -model model-b -token $KEY2

# 生成对比报告
./compare.sh

# 或指定过滤模式
./compare.sh deepseek    # 只对比包含 'deepseek' 的结果
./compare.sh --help      # 查看帮助
```

生成的对比报告包含：
- TTFT/Latency/Throughput 对比柱状图
- 综合能力雷达图
- 长上下文 TTFT 曲线对比
- 延迟分布箱线图
- Function Call 支持对比
- 会议纪要性能对比

## 命令行参数

### 通用参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-url` | (必填) | API 端点 URL |
| `-model` | (必填) | 模型名称 |
| `-token` | | API 认证 Token |
| `-timeout` | 60 | 请求超时（秒） |
| `-insecure` | false | 跳过 TLS 验证 |
| `-ca-cert` | | 自定义 CA 证书路径 |
| `-provider` | openai | Provider 类型 |
| `-verbose` | false | 显示详细请求/响应日志 |

### 模式选择

| 参数 | 说明 |
|------|------|
| `-full-test` | 运行完整测试套件（性能 + Function Call + 长上下文 + 会议纪要） |
| `-summary-bench` | 会议纪要并发压测模式 |
| `-transcript-file` | 指定会议记录文件，启用 Summary 模式 |
| (默认) | Benchmark 模式 |

### Summary Benchmark 模式参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-sb-concurrency` | 5 | 并发 worker 数 |
| `-sb-requests` | 20 | 总请求数 |
| `-chunk-size` | 8000 | 会议纪要分块大小（字符） |

### Benchmark 模式参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-concurrency` | 1 | 并发 worker 数 |
| `-total-requests` | 10 | 总请求数 |
| `-duration` | 0 | 运行时长（秒） |
| `-rps` | 0 | 每秒请求数限制（0=不限制） |
| `-warmup` | 0 | 预热请求数（不计入统计） |
| `-max-tokens` | 256 | 响应最大 token 数 |
| `-token-mode` | usage | Token 统计模式：usage/chars/disabled |
| `-workload-file` | | Prompts 文件路径 |
| `-out` | ./output | 输出目录 |

### Summary 模式参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-transcript-file` | | 会议记录文件路径 |
| `-chunk-size` | 8000 | 每个分片的最大字符数 |
| `-meeting-time` | (当前时间) | 会议时间，用于报告标题 |

## 输出文件

### Full Test 模式输出

```
output/fulltest_{model}_{timestamp}/
├── full_test_report.md       # Markdown 汇总报告
├── full_test_report.html     # HTML 可视化报告（暗色主题，内嵌 ECharts）
├── request_response.log      # 完整请求/响应日志
├── benchmark/                # Phase 1: 性能测试结果
│   ├── results.jsonl         # 每个请求的详细结果
│   ├── summary.json          # 聚合统计数据
│   └── report.html           # 可视化 HTML 报告
└── summary/                  # Phase 4: 会议纪要结果
    ├── meeting_summary.md    # 最终会议纪要
    ├── performance_report.md # 性能指标报告
    ├── performance_metrics.json
    └── intermediate/         # 分片处理中间结果
```

### Summary Benchmark 模式输出

```
output/summarybench_{model}_{timestamp}/
├── summary_bench_report.json # JSON 详细报告
└── summary_bench_report.md   # Markdown 报告
```

### 对比报告输出

```
local/comparison_{timestamp}.html  # 多模型对比 HTML 报告
```

### Benchmark 模式输出

- `results.jsonl` - 每个请求的详细结果
- `summary.json` - 聚合统计数据
- `report.html` - 可视化 HTML 报告

### Summary 模式输出

- `meeting_summary.md` - 最终会议纪要
- `performance_report.md` - 性能指标报告
- `performance_metrics.json` - JSON 格式性能数据
- `intermediate/` - 分片处理中间结果

## 指标说明

### 核心指标详解

| 指标 | 全称 | 说明 |
|------|------|------|
| **TTFT** | Time To First Token | 从请求发出到收到第一个内容 token 的时间。反映模型响应速度，用户体验的关键指标。 |
| **Latency** | 总延迟 | 从请求发出到完整响应结束的时间。包含 TTFT + 生成时间。 |
| **Throughput** | 吞吐量 | 每秒生成的 token 数（tokens/s）或字符数（chars/s）。反映模型生成效率。 |
| **RPS** | Requests Per Second | 每秒成功完成的请求数。反映服务整体处理能力。 |
| **Success Rate** | 成功率 | 成功请求数 / 总请求数。反映服务稳定性。 |

### 百分位数指标

| 指标 | 说明 |
|------|------|
| **P50** (中位数) | 50% 的请求在此时间内完成。代表典型用户体验。 |
| **P95** | 95% 的请求在此时间内完成。用于评估大多数用户的体验。 |
| **P99** | 99% 的请求在此时间内完成。用于发现长尾延迟问题。 |

### 指标解读示例

```
Avg TTFT:     84.67 ms   → 平均 85ms 开始输出，用户感知快
Avg Latency:  1890.70 ms → 平均 1.9 秒完成生成
P50 TTFT:     77 ms      → 一半请求在 77ms 内开始输出
P95 TTFT:     122 ms     → 95% 请求在 122ms 内开始输出
P99 TTFT:     137 ms     → 99% 请求在 137ms 内开始输出（无严重长尾）
RPS:          4.77       → 服务每秒处理约 4.8 个请求
Throughput:   501.28/s   → 每秒生成约 500 个 token
```

### Token 统计模式

| 模式 | 说明 |
|------|------|
| `usage` | 使用 API 返回的 `usage` 字段统计（最准确） |
| `chars` | 按字符数统计，当 API 不返回 token 数时使用 |
| `disabled` | 不统计 token，只关注延迟指标 |

## 构建

```bash
# 当前平台
make build

# 所有平台
make build-all

# Docker
make docker
```

## 项目结构

```
.
├── cmd/llm-benchmark-kit/   # CLI 入口
├── pkg/
│   ├── config/              # 配置定义
│   ├── embedded/            # 内嵌资源（测试文本）
│   ├── fulltest/            # Full Test 模式实现
│   │   ├── templates/       # HTML 报告模板
│   │   └── assets/          # 内嵌 JS/字体资源
│   ├── provider/            # Provider 接口和实现
│   │   └── openai/          # OpenAI Provider
│   ├── result/              # 结果类型
│   ├── runner/              # 基准测试运行器
│   │   └── templates/       # Benchmark HTML 模板
│   ├── sse/                 # SSE 解析器
│   ├── stats/               # 统计工具
│   ├── summarizer/          # 会议纪要生成器
│   ├── summarybench/        # 会议纪要并发压测
│   └── workload/            # 工作负载定义
├── tools/
│   └── compare/             # 多模型对比报告工具
│       ├── compare_report.py
│       └── requirements.txt
├── compare.sh               # 一键对比报告脚本
└── local/                   # 本地输出（已 gitignore）
```

## License

MIT License
