# LLM Benchmark Kit

高性能 LLM 基准测试工具，用于精准统计 **TTFT / Latency / Throughput / P50/P95/P99**，并生成离线可打开的自包含 HTML 报告。

## 特性

- **单文件交付**：CGO=0 静态二进制，客户环境直接运行
- **精准流式指标**：支持 SSE 流式处理，可靠计算 TTFT/Latency
- **多架构支持**：amd64/arm64 原生支持，Docker multi-arch
- **高扩展性**：Provider 接口 + 注册表机制，快速适配各类 LLM API
- **离线报告**：单文件 HTML（内嵌 ECharts/JS/数据），不依赖外网 CDN

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

## 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-url` | (必填) | API 端点 URL |
| `-model` | (必填) | 模型名称 |
| `-token` | | API 认证 Token |
| `-concurrency` | 1 | 并发 worker 数 |
| `-total-requests` | 10 | 总请求数 |
| `-duration` | 0 | 运行时长（秒） |
| `-rps` | 0 | 每秒请求数限制（0=不限制） |
| `-warmup` | 0 | 预热请求数（不计入统计） |
| `-max-tokens` | 256 | 响应最大 token 数 |
| `-token-mode` | usage | Token 统计模式：usage/chars/disabled |
| `-timeout` | 60 | 请求超时（秒） |
| `-insecure` | false | 跳过 TLS 验证 |
| `-ca-cert` | | 自定义 CA 证书路径 |
| `-workload-file` | | Prompts 文件路径 |
| `-out` | ./output | 输出目录 |
| `-provider` | openai | Provider 类型 |

## 输出文件

- `results.jsonl` - 每个请求的详细结果
- `summary.json` - 聚合统计数据
- `report.html` - 可视化 HTML 报告

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
│   ├── provider/            # Provider 接口和实现
│   │   └── openai/          # OpenAI Provider
│   ├── result/              # 结果类型
│   ├── runner/              # 基准测试运行器
│   ├── sse/                 # SSE 解析器
│   ├── stats/               # 统计工具
│   └── workload/            # 工作负载定义
└── doc/                     # 文档
```

## License

MIT License
