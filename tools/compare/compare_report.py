#!/usr/bin/env python3
"""
LLM Benchmark Comparison Report Generator

Scans output directory for benchmark results and generates a comparative
HTML report with interactive charts.
"""

import argparse
import json
import os
import sys
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import List, Optional, Dict, Any

try:
    import plotly.graph_objects as go
    from plotly.subplots import make_subplots
except ImportError:
    print("Error: plotly is required. Install with: pip install plotly")
    sys.exit(1)


@dataclass
class BenchmarkResult:
    """Holds parsed benchmark results from a single test."""
    name: str  # Directory name / test identifier
    model: str
    started_at: str
    wall_time_ms: int
    total_requests: int
    success: int
    failure: int
    success_rate: float
    avg_ttft_ms: float
    p50_ttft_ms: int
    p95_ttft_ms: int
    p99_ttft_ms: int
    avg_latency_ms: float
    p50_latency_ms: int
    p95_latency_ms: int
    p99_latency_ms: int
    token_throughput: float
    rps: float
    ttft_distribution: List[int]
    latency_distribution: List[int]


def find_benchmark_dirs(output_dir: str, pattern: Optional[str] = None) -> List[Path]:
    """Find all directories containing benchmark results."""
    output_path = Path(output_dir)
    if not output_path.exists():
        print(f"Error: Output directory '{output_dir}' does not exist")
        return []
    
    results = []
    for item in output_path.iterdir():
        if not item.is_dir():
            continue
        
        # Check if this looks like a benchmark result
        summary_path = item / "benchmark" / "summary.json"
        if summary_path.exists():
            if pattern is None or pattern in item.name:
                results.append(item)
    
    return sorted(results, key=lambda x: x.name)


def parse_benchmark_result(result_dir: Path) -> Optional[BenchmarkResult]:
    """Parse a benchmark result directory."""
    summary_path = result_dir / "benchmark" / "summary.json"
    
    if not summary_path.exists():
        print(f"Warning: No summary.json found in {result_dir}")
        return None
    
    try:
        with open(summary_path, 'r') as f:
            data = json.load(f)
        
        return BenchmarkResult(
            name=result_dir.name,
            model=data.get('model', 'Unknown'),
            started_at=data.get('started_at', ''),
            wall_time_ms=data.get('wall_time_ms', 0),
            total_requests=data.get('total_requests', 0),
            success=data.get('success', 0),
            failure=data.get('failure', 0),
            success_rate=data.get('success_rate', 0),
            avg_ttft_ms=data.get('avg_ttft_ms', 0),
            p50_ttft_ms=data.get('p50_ttft_ms', 0),
            p95_ttft_ms=data.get('p95_ttft_ms', 0),
            p99_ttft_ms=data.get('p99_ttft_ms', 0),
            avg_latency_ms=data.get('avg_latency_ms', 0),
            p50_latency_ms=data.get('p50_latency_ms', 0),
            p95_latency_ms=data.get('p95_latency_ms', 0),
            p99_latency_ms=data.get('p99_latency_ms', 0),
            token_throughput=data.get('token_throughput', 0),
            rps=data.get('rps', 0),
            ttft_distribution=data.get('ttft_distribution_ms', []),
            latency_distribution=data.get('latency_distribution_ms', []),
        )
    except Exception as e:
        print(f"Error parsing {summary_path}: {e}")
        return None


def create_ttft_chart(results: List[BenchmarkResult]) -> str:
    """Create TTFT comparison chart."""
    names = [r.name for r in results]
    
    fig = go.Figure()
    
    fig.add_trace(go.Bar(
        name='Avg TTFT',
        x=names,
        y=[r.avg_ttft_ms for r in results],
        marker_color='#3498db'
    ))
    fig.add_trace(go.Bar(
        name='P50 TTFT',
        x=names,
        y=[r.p50_ttft_ms for r in results],
        marker_color='#2ecc71'
    ))
    fig.add_trace(go.Bar(
        name='P95 TTFT',
        x=names,
        y=[r.p95_ttft_ms for r in results],
        marker_color='#f39c12'
    ))
    fig.add_trace(go.Bar(
        name='P99 TTFT',
        x=names,
        y=[r.p99_ttft_ms for r in results],
        marker_color='#e74c3c'
    ))
    
    fig.update_layout(
        title='Time To First Token (TTFT) 对比',
        xaxis_title='测试名称',
        yaxis_title='时间 (ms)',
        barmode='group',
        template='plotly_white',
        height=500
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_latency_chart(results: List[BenchmarkResult]) -> str:
    """Create Latency comparison chart."""
    names = [r.name for r in results]
    
    fig = go.Figure()
    
    fig.add_trace(go.Bar(
        name='Avg Latency',
        x=names,
        y=[r.avg_latency_ms for r in results],
        marker_color='#3498db'
    ))
    fig.add_trace(go.Bar(
        name='P50 Latency',
        x=names,
        y=[r.p50_latency_ms for r in results],
        marker_color='#2ecc71'
    ))
    fig.add_trace(go.Bar(
        name='P95 Latency',
        x=names,
        y=[r.p95_latency_ms for r in results],
        marker_color='#f39c12'
    ))
    fig.add_trace(go.Bar(
        name='P99 Latency',
        x=names,
        y=[r.p99_latency_ms for r in results],
        marker_color='#e74c3c'
    ))
    
    fig.update_layout(
        title='总延迟 (Latency) 对比',
        xaxis_title='测试名称',
        yaxis_title='时间 (ms)',
        barmode='group',
        template='plotly_white',
        height=500
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_throughput_chart(results: List[BenchmarkResult]) -> str:
    """Create throughput comparison chart."""
    names = [r.name for r in results]
    
    fig = make_subplots(
        rows=1, cols=2,
        subplot_titles=('Token 吞吐量 (tokens/s)', 'RPS (请求/秒)')
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.token_throughput for r in results],
            marker_color='#9b59b6',
            name='Token Throughput'
        ),
        row=1, col=1
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.rps for r in results],
            marker_color='#1abc9c',
            name='RPS'
        ),
        row=1, col=2
    )
    
    fig.update_layout(
        title='吞吐量对比',
        template='plotly_white',
        height=400,
        showlegend=False
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_radar_chart(results: List[BenchmarkResult]) -> str:
    """Create radar chart for multi-dimensional comparison."""
    if not results:
        return ""
    
    # Normalize values for radar chart (higher is better for all)
    # For latency metrics, we invert them so lower is better
    max_ttft = max(r.avg_ttft_ms for r in results) or 1
    max_latency = max(r.avg_latency_ms for r in results) or 1
    max_throughput = max(r.token_throughput for r in results) or 1
    max_rps = max(r.rps for r in results) or 1
    
    fig = go.Figure()
    
    categories = ['响应速度<br>(1/TTFT)', '生成速度<br>(1/Latency)', 
                  '吞吐量', 'RPS', '成功率']
    
    for r in results:
        # Invert TTFT and Latency so higher is better
        ttft_score = (1 - r.avg_ttft_ms / max_ttft) * 100 if max_ttft > 0 else 0
        latency_score = (1 - r.avg_latency_ms / max_latency) * 100 if max_latency > 0 else 0
        throughput_score = (r.token_throughput / max_throughput) * 100 if max_throughput > 0 else 0
        rps_score = (r.rps / max_rps) * 100 if max_rps > 0 else 0
        success_score = r.success_rate * 100
        
        fig.add_trace(go.Scatterpolar(
            r=[ttft_score, latency_score, throughput_score, rps_score, success_score],
            theta=categories,
            fill='toself',
            name=r.name
        ))
    
    fig.update_layout(
        title='综合性能雷达图',
        polar=dict(
            radialaxis=dict(
                visible=True,
                range=[0, 100]
            )
        ),
        template='plotly_white',
        height=500
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_ttft_distribution_chart(results: List[BenchmarkResult]) -> str:
    """Create TTFT distribution box plot."""
    fig = go.Figure()
    
    for r in results:
        if r.ttft_distribution:
            fig.add_trace(go.Box(
                y=r.ttft_distribution,
                name=r.name,
                boxpoints='outliers'
            ))
    
    fig.update_layout(
        title='TTFT 分布对比 (箱线图)',
        yaxis_title='TTFT (ms)',
        template='plotly_white',
        height=400
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_latency_distribution_chart(results: List[BenchmarkResult]) -> str:
    """Create Latency distribution box plot."""
    fig = go.Figure()
    
    for r in results:
        if r.latency_distribution:
            fig.add_trace(go.Box(
                y=r.latency_distribution,
                name=r.name,
                boxpoints='outliers'
            ))
    
    fig.update_layout(
        title='Latency 分布对比 (箱线图)',
        yaxis_title='Latency (ms)',
        template='plotly_white',
        height=400
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def generate_html_report(results: List[BenchmarkResult], output_path: str) -> None:
    """Generate the complete HTML comparison report."""
    
    # Generate all charts
    ttft_chart = create_ttft_chart(results)
    latency_chart = create_latency_chart(results)
    throughput_chart = create_throughput_chart(results)
    radar_chart = create_radar_chart(results)
    ttft_dist_chart = create_ttft_distribution_chart(results)
    latency_dist_chart = create_latency_distribution_chart(results)
    
    # Generate summary table rows
    table_rows = ""
    for r in results:
        table_rows += f"""
        <tr>
            <td>{r.name}</td>
            <td>{r.model}</td>
            <td>{r.avg_ttft_ms:.2f}</td>
            <td>{r.p99_ttft_ms}</td>
            <td>{r.avg_latency_ms:.2f}</td>
            <td>{r.p99_latency_ms}</td>
            <td>{r.token_throughput:.2f}</td>
            <td>{r.rps:.2f}</td>
            <td>{r.success_rate*100:.1f}%</td>
        </tr>
        """
    
    html_content = f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM Benchmark 对比报告</title>
    <script src="https://cdn.plot.ly/plotly-2.27.0.min.js"></script>
    <style>
        * {{
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }}
        body {{
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
            min-height: 100vh;
            color: #e0e0e0;
            padding: 20px;
        }}
        .container {{
            max-width: 1400px;
            margin: 0 auto;
        }}
        header {{
            text-align: center;
            padding: 40px 20px;
            background: rgba(255,255,255,0.05);
            border-radius: 16px;
            margin-bottom: 30px;
            backdrop-filter: blur(10px);
        }}
        h1 {{
            font-size: 2.5em;
            background: linear-gradient(90deg, #00d2ff, #3a7bd5);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 10px;
        }}
        .subtitle {{
            color: #888;
            font-size: 1.1em;
        }}
        .section {{
            background: rgba(255,255,255,0.05);
            border-radius: 16px;
            padding: 25px;
            margin-bottom: 25px;
            backdrop-filter: blur(10px);
        }}
        .section h2 {{
            color: #3a7bd5;
            margin-bottom: 20px;
            padding-bottom: 10px;
            border-bottom: 1px solid rgba(255,255,255,0.1);
        }}
        table {{
            width: 100%;
            border-collapse: collapse;
            margin-top: 15px;
        }}
        th, td {{
            padding: 12px 15px;
            text-align: left;
            border-bottom: 1px solid rgba(255,255,255,0.1);
        }}
        th {{
            background: rgba(58, 123, 213, 0.3);
            color: #fff;
            font-weight: 600;
        }}
        tr:hover {{
            background: rgba(255,255,255,0.05);
        }}
        .chart-container {{
            background: #fff;
            border-radius: 12px;
            padding: 15px;
            margin-bottom: 20px;
        }}
        .chart-row {{
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 20px;
        }}
        @media (max-width: 1000px) {{
            .chart-row {{
                grid-template-columns: 1fr;
            }}
        }}
        .footer {{
            text-align: center;
            padding: 20px;
            color: #666;
            font-size: 0.9em;
        }}
        .metric-highlight {{
            font-weight: 600;
            color: #00d2ff;
        }}
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>🚀 LLM Benchmark 对比报告</h1>
            <p class="subtitle">生成时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')} | 共 {len(results)} 个测试</p>
        </header>
        
        <div class="section">
            <h2>📊 数据汇总</h2>
            <div style="overflow-x: auto;">
                <table>
                    <thead>
                        <tr>
                            <th>测试名称</th>
                            <th>模型</th>
                            <th>Avg TTFT (ms)</th>
                            <th>P99 TTFT (ms)</th>
                            <th>Avg Latency (ms)</th>
                            <th>P99 Latency (ms)</th>
                            <th>Throughput (tok/s)</th>
                            <th>RPS</th>
                            <th>成功率</th>
                        </tr>
                    </thead>
                    <tbody>
                        {table_rows}
                    </tbody>
                </table>
            </div>
        </div>
        
        <div class="section">
            <h2>⚡ TTFT (首字延迟) 对比</h2>
            <div class="chart-container">
                {ttft_chart}
            </div>
        </div>
        
        <div class="section">
            <h2>⏱️ Latency (总延迟) 对比</h2>
            <div class="chart-container">
                {latency_chart}
            </div>
        </div>
        
        <div class="section">
            <h2>📈 吞吐量对比</h2>
            <div class="chart-container">
                {throughput_chart}
            </div>
        </div>
        
        <div class="section">
            <h2>🎯 综合性能对比</h2>
            <div class="chart-container">
                {radar_chart}
            </div>
        </div>
        
        <div class="section">
            <h2>📦 延迟分布对比</h2>
            <div class="chart-row">
                <div class="chart-container">
                    {ttft_dist_chart}
                </div>
                <div class="chart-container">
                    {latency_dist_chart}
                </div>
            </div>
        </div>
        
        <div class="footer">
            <p>Generated by LLM Benchmark Kit | 
               <a href="https://github.com/brianxiadong/llm-benchmark-kit" style="color: #3a7bd5;">GitHub</a>
            </p>
        </div>
    </div>
</body>
</html>
"""
    
    with open(output_path, 'w', encoding='utf-8') as f:
        f.write(html_content)
    
    print(f"✅ Report generated: {output_path}")


def main():
    parser = argparse.ArgumentParser(
        description='LLM Benchmark Comparison Report Generator',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python compare_report.py --input ../../output --output comparison.html
  python compare_report.py --input ./output --pattern "fulltest_" --output report.html
        """
    )
    parser.add_argument(
        '--input', '-i',
        default='../../output',
        help='Input directory containing benchmark results (default: ../../output)'
    )
    parser.add_argument(
        '--output', '-o',
        default='comparison_report.html',
        help='Output HTML report path (default: comparison_report.html)'
    )
    parser.add_argument(
        '--pattern', '-p',
        default=None,
        help='Filter directories by pattern (e.g., "fulltest_")'
    )
    
    args = parser.parse_args()
    
    print(f"🔍 Scanning {args.input} for benchmark results...")
    
    # Find all benchmark directories
    result_dirs = find_benchmark_dirs(args.input, args.pattern)
    
    if not result_dirs:
        print("❌ No benchmark results found!")
        sys.exit(1)
    
    print(f"📁 Found {len(result_dirs)} benchmark results:")
    for d in result_dirs:
        print(f"   - {d.name}")
    
    # Parse all results
    results = []
    for d in result_dirs:
        result = parse_benchmark_result(d)
        if result:
            results.append(result)
    
    if not results:
        print("❌ Failed to parse any benchmark results!")
        sys.exit(1)
    
    print(f"\n📊 Generating comparison report...")
    
    # Generate report
    generate_html_report(results, args.output)
    
    print(f"\n🎉 Done! Open {args.output} in your browser to view the report.")


if __name__ == '__main__':
    main()
