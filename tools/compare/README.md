# LLM Benchmark 对比报告生成器

扫描 output 目录下的多个基准测试结果，生成对比图表和 HTML 报告。

## 安装

```bash
pip install -r requirements.txt
```

## 使用方法

```bash
# 基础用法 - 扫描所有测试结果
python compare_report.py --input ../../output --output comparison.html

# 按模式过滤
python compare_report.py --input ../../output --pattern "fulltest_" --output fulltest_comparison.html

# 指定其他输出目录
python compare_report.py -i /path/to/output -o /path/to/report.html
```

## 参数说明

| 参数 | 简写 | 默认值 | 说明 |
|------|------|--------|------|
| `--input` | `-i` | `../../output` | 测试结果目录 |
| `--output` | `-o` | `comparison_report.html` | 输出报告路径 |
| `--pattern` | `-p` | (无) | 目录名过滤模式 |

## 生成的图表

1. **TTFT 对比柱状图** - Avg/P50/P95/P99 对比
2. **Latency 对比柱状图** - Avg/P50/P95/P99 对比
3. **吞吐量对比** - Token Throughput 和 RPS
4. **综合雷达图** - 多维度性能对比
5. **TTFT 分布箱线图** - 延迟分布可视化
6. **Latency 分布箱线图** - 延迟分布可视化

## 输入数据格式

工具会自动扫描以下目录结构：

```
output/
├── test-1/
│   └── benchmark/
│       └── summary.json
├── test-2/
│   └── benchmark/
│       └── summary.json
└── ...
```
