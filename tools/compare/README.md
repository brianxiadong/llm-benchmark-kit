# LLM FullTest 对比报告生成器

扫描 output 目录下的多个 fulltest 测试结果，生成对比图表和 HTML 报告。

## 功能特性

对比 fulltest 的三个测试阶段：

| 阶段 | 对比内容 |
|------|----------|
| **Phase 1** | TTFT、Latency、Throughput、RPS、成功率 |
| **Phase 2** | Function Call 支持状态、函数名、参数、延迟 |
| **Phase 3** | 会议纪要 Token 数、处理时间、Token/s |

## 安装

```bash
pip install -r requirements.txt
```

## 使用方法

```bash
# 基础用法 - 扫描所有 fulltest 结果
python compare_report.py --input ../../output --output comparison.html

# 按模式过滤
python compare_report.py --input ../../output --pattern "fulltest_" --output fulltest_comparison.html

# 指定其他输出目录
python compare_report.py -i /path/to/output -o /path/to/report.html
```

## 参数说明

| 参数 | 简写 | 默认值 | 说明 |
|------|------|--------|------|
| `--input` | `-i` | `../../output` | fulltest 结果目录 |
| `--output` | `-o` | `fulltest_comparison.html` | 输出报告路径 |
| `--pattern` | `-p` | (无) | 目录名过滤模式 |

## 生成的图表

1. **TTFT 对比柱状图** - Avg/P50/P95/P99 对比
2. **Latency 对比柱状图** - Avg/P50/P95/P99 对比
3. **吞吐量对比** - Token Throughput 和 RPS
4. **综合雷达图** - 六维度性能对比（含会议纪要指标）
5. **TTFT 分布箱线图** - 延迟分布可视化
6. **Latency 分布箱线图** - 延迟分布可视化
7. **会议纪要性能对比** - Token/s、总 Tokens、处理时间

## 输入数据格式

工具会自动扫描以下目录结构：

```
output/
├── fulltest_model1/
│   ├── benchmark/
│   │   └── summary.json       # Phase 1 数据
│   ├── summary/
│   │   └── performance_metrics.json  # Phase 3 数据
│   └── full_test_report.md    # Phase 2 数据
├── fulltest_model2/
│   └── ...
└── ...
```
