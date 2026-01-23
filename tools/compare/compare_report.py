#!/usr/bin/env python3
"""
LLM FullTest Comparison Report Generator

Scans output directory for fulltest results and generates a comparative
HTML report with interactive charts for all three phases.
"""

import argparse
import json
import os
import re
import sys
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import List, Optional

try:
    import plotly.graph_objects as go
    from plotly.subplots import make_subplots
except ImportError:
    print("Error: plotly is required. Install with: pip install plotly")
    sys.exit(1)


@dataclass
class FullTestResult:
    """Holds parsed fulltest results from a single test."""
    # Basic Info
    name: str  # Directory name / test identifier
    model: str  # Model name
    
    # Phase 1: Performance Metrics (from benchmark/summary.json)
    started_at: str = ""
    wall_time_ms: int = 0
    total_requests: int = 0
    success: int = 0
    failure: int = 0
    success_rate: float = 0.0
    avg_ttft_ms: float = 0.0
    p50_ttft_ms: int = 0
    p95_ttft_ms: int = 0
    p99_ttft_ms: int = 0
    avg_latency_ms: float = 0.0
    p50_latency_ms: int = 0
    p95_latency_ms: int = 0
    p99_latency_ms: int = 0
    token_throughput: float = 0.0
    rps: float = 0.0
    ttft_distribution: List[int] = field(default_factory=list)
    latency_distribution: List[int] = field(default_factory=list)
    
    # Phase 2: Function Call
    fc_supported: bool = False
    fc_function_name: str = ""
    fc_arguments: str = ""
    fc_latency_ms: float = 0.0
    
    # Phase 3: Long Context Test
    lc_max_supported: int = 0
    lc_avg_ttft_ms: float = 0.0
    lc_avg_latency_ms: float = 0.0
    lc_avg_throughput: float = 0.0
    lc_results: List[dict] = field(default_factory=list)  # List of {context_length, ttft_ms, latency_ms, throughput, success}
    
    # Phase 4: Meeting Summary (from summary/performance_metrics.json)
    summary_total_chunks: int = 0
    summary_total_tokens: int = 0
    summary_prompt_tokens: int = 0
    summary_completion_tokens: int = 0
    summary_processing_time_sec: float = 0.0
    summary_tokens_per_second: float = 0.0


def find_fulltest_dirs(output_dir: str, pattern: Optional[str] = None) -> List[Path]:
    """Find all directories containing fulltest results."""
    output_path = Path(output_dir)
    if not output_path.exists():
        print(f"Error: Output directory '{output_dir}' does not exist")
        return []
    
    results = []
    for item in output_path.iterdir():
        if not item.is_dir():
            continue
        
        # Check if this looks like a fulltest result (has benchmark/summary.json)
        summary_path = item / "benchmark" / "summary.json"
        if summary_path.exists():
            if pattern is None or pattern in item.name:
                results.append(item)
    
    return sorted(results, key=lambda x: x.name)


def parse_function_call_from_md(md_path: Path) -> dict:
    """Parse Function Call result from full_test_report.md."""
    result = {
        "supported": False,
        "function_name": "",
        "arguments": "",
        "latency_ms": 0.0
    }
    
    if not md_path.exists():
        return result
    
    try:
        content = md_path.read_text(encoding='utf-8')
        
        # Check if function call is supported
        if "✅ **支持 Function Call**" in content:
            result["supported"] = True
            
            # Extract function name
            fn_match = re.search(r'- 函数名: `([^`]+)`', content)
            if fn_match:
                result["function_name"] = fn_match.group(1)
            
            # Extract arguments
            args_match = re.search(r'- 参数: `([^`]+)`', content)
            if args_match:
                result["arguments"] = args_match.group(1)
            
            # Extract latency
            latency_match = re.search(r'- 响应延迟: ([\d.]+) ms', content)
            if latency_match:
                result["latency_ms"] = float(latency_match.group(1))
        
    except Exception as e:
        print(f"Warning: Failed to parse function call from {md_path}: {e}")
    
    return result


def parse_long_context_from_md(md_path: Path) -> dict:
    """Parse Long Context test results from full_test_report.md."""
    result = {
        "max_supported": 0,
        "avg_ttft_ms": 0.0,
        "avg_latency_ms": 0.0,
        "avg_throughput": 0.0,
        "results": []
    }
    
    if not md_path.exists():
        return result
    
    try:
        content = md_path.read_text(encoding='utf-8')
        
        # Find the Long Context section
        lc_section = re.search(r'## Phase 3: 长上下文测试\s*(.*?)(?=## Phase|$)', content, re.DOTALL)
        if not lc_section:
            return result
        
        section_content = lc_section.group(1)
        
        # Parse table rows: | 1000 字符 | 700 | 123.45 | 456.78 | 12.34 | ✅ |
        row_pattern = r'\|\s*(\d+)\s*字符\s*\|\s*(\d+)\s*\|\s*([\d.]+)\s*\|\s*([\d.]+)\s*\|\s*([\d.]+)\s*\|\s*([✅❌])\s*\|'
        for match in re.finditer(row_pattern, section_content):
            context_length = int(match.group(1))
            input_tokens = int(match.group(2))
            ttft_ms = float(match.group(3))
            latency_ms = float(match.group(4))
            throughput = float(match.group(5))
            success = match.group(6) == '✅'
            
            result["results"].append({
                "context_length": context_length,
                "input_tokens": input_tokens,
                "ttft_ms": ttft_ms,
                "latency_ms": latency_ms,
                "throughput": throughput,
                "success": success
            })
            
            if success:
                result["max_supported"] = max(result["max_supported"], context_length)
        
        # Parse summary line: **最大支持上下文**: 32000 字符 | **平均 TTFT**: 123.45 ms | **平均吞吐**: 12.34 tokens/s
        summary_match = re.search(r'\*\*最大支持上下文\*\*:\s*(\d+)\s*字符.*?\*\*平均 TTFT\*\*:\s*([\d.]+)\s*ms.*?\*\*平均吞吐\*\*:\s*([\d.]+)\s*tokens/s', section_content)
        if summary_match:
            result["max_supported"] = int(summary_match.group(1))
            result["avg_ttft_ms"] = float(summary_match.group(2))
            result["avg_throughput"] = float(summary_match.group(3))
        
        # Calculate avg latency from results if not in summary
        successful_results = [r for r in result["results"] if r["success"]]
        if successful_results:
            result["avg_latency_ms"] = sum(r["latency_ms"] for r in successful_results) / len(successful_results)
        
    except Exception as e:
        print(f"Warning: Failed to parse long context from {md_path}: {e}")
    
    return result


def parse_fulltest_result(result_dir: Path) -> Optional[FullTestResult]:
    """Parse a fulltest result directory."""
    # Parse benchmark/summary.json (Phase 1)
    summary_path = result_dir / "benchmark" / "summary.json"
    if not summary_path.exists():
        print(f"Warning: No benchmark/summary.json found in {result_dir}")
        return None
    
    try:
        with open(summary_path, 'r') as f:
            bench_data = json.load(f)
        
        result = FullTestResult(
            name=result_dir.name,
            model=bench_data.get('model', 'Unknown'),
            started_at=bench_data.get('started_at', ''),
            wall_time_ms=bench_data.get('wall_time_ms', 0),
            total_requests=bench_data.get('total_requests', 0),
            success=bench_data.get('success', 0),
            failure=bench_data.get('failure', 0),
            success_rate=bench_data.get('success_rate', 0),
            avg_ttft_ms=bench_data.get('avg_ttft_ms', 0),
            p50_ttft_ms=bench_data.get('p50_ttft_ms', 0),
            p95_ttft_ms=bench_data.get('p95_ttft_ms', 0),
            p99_ttft_ms=bench_data.get('p99_ttft_ms', 0),
            avg_latency_ms=bench_data.get('avg_latency_ms', 0),
            p50_latency_ms=bench_data.get('p50_latency_ms', 0),
            p95_latency_ms=bench_data.get('p95_latency_ms', 0),
            p99_latency_ms=bench_data.get('p99_latency_ms', 0),
            token_throughput=bench_data.get('token_throughput', 0),
            rps=bench_data.get('rps', 0),
            ttft_distribution=bench_data.get('ttft_distribution_ms', []),
            latency_distribution=bench_data.get('latency_distribution_ms', []),
        )
        
        # Parse summary/performance_metrics.json (Phase 3)
        metrics_path = result_dir / "summary" / "performance_metrics.json"
        if metrics_path.exists():
            with open(metrics_path, 'r') as f:
                summary_data = json.load(f)
            
            result.summary_total_chunks = summary_data.get('total_chunks', 0)
            result.summary_total_tokens = summary_data.get('total_tokens', 0)
            result.summary_prompt_tokens = summary_data.get('total_prompt_tokens', 0)
            result.summary_completion_tokens = summary_data.get('total_completion_tokens', 0)
            # Convert nanoseconds to seconds
            processing_time_ns = summary_data.get('total_processing_time', 0)
            result.summary_processing_time_sec = processing_time_ns / 1e9
            result.summary_tokens_per_second = summary_data.get('tokens_per_second', 0)
        
        # Parse function call from full_test_report.md (Phase 2)
        md_path = result_dir / "full_test_report.md"
        fc_data = parse_function_call_from_md(md_path)
        result.fc_supported = fc_data["supported"]
        result.fc_function_name = fc_data["function_name"]
        result.fc_arguments = fc_data["arguments"]
        result.fc_latency_ms = fc_data["latency_ms"]
        
        # Parse long context results from full_test_report.md (Phase 3)
        lc_data = parse_long_context_from_md(md_path)
        result.lc_max_supported = lc_data["max_supported"]
        result.lc_avg_ttft_ms = lc_data["avg_ttft_ms"]
        result.lc_avg_latency_ms = lc_data["avg_latency_ms"]
        result.lc_avg_throughput = lc_data["avg_throughput"]
        result.lc_results = lc_data["results"]
        
        return result
        
    except Exception as e:
        print(f"Error parsing {result_dir}: {e}")
        return None


# Premium color palette
COLORS = {
    'blue': '#6366f1',
    'purple': '#8b5cf6',
    'pink': '#ec4899',
    'green': '#10b981',
    'red': '#f43f5e',
    'orange': '#f59e0b',
    'cyan': '#06b6d4',
    'indigo': '#4f46e5',
}

DARK_LAYOUT = dict(
    template='plotly_dark',
    paper_bgcolor='rgba(0,0,0,0)',
    plot_bgcolor='rgba(0,0,0,0)',
    font=dict(family='Plus Jakarta Sans, sans-serif', color='#f0f0f5'),
    title_font=dict(size=18, color='#f0f0f5'),
    legend=dict(bgcolor='rgba(0,0,0,0)'),
)

def create_ttft_chart(results: List[FullTestResult]) -> str:
    """Create TTFT comparison chart."""
    names = [r.name for r in results]
    
    fig = go.Figure()
    
    fig.add_trace(go.Bar(
        name='Avg TTFT',
        x=names,
        y=[r.avg_ttft_ms for r in results],
        marker_color=COLORS['blue']
    ))
    fig.add_trace(go.Bar(
        name='P50 TTFT',
        x=names,
        y=[r.p50_ttft_ms for r in results],
        marker_color=COLORS['green']
    ))
    fig.add_trace(go.Bar(
        name='P95 TTFT',
        x=names,
        y=[r.p95_ttft_ms for r in results],
        marker_color=COLORS['orange']
    ))
    fig.add_trace(go.Bar(
        name='P99 TTFT',
        x=names,
        y=[r.p99_ttft_ms for r in results],
        marker_color=COLORS['red']
    ))
    
    fig.update_layout(
        title='Time To First Token (TTFT) 对比',
        xaxis_title='测试名称',
        yaxis_title='时间 (ms)',
        barmode='group',
        height=500,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_latency_chart(results: List[FullTestResult]) -> str:
    """Create Latency comparison chart."""
    names = [r.name for r in results]
    
    fig = go.Figure()
    
    fig.add_trace(go.Bar(
        name='Avg Latency',
        x=names,
        y=[r.avg_latency_ms for r in results],
        marker_color=COLORS['blue']
    ))
    fig.add_trace(go.Bar(
        name='P50 Latency',
        x=names,
        y=[r.p50_latency_ms for r in results],
        marker_color=COLORS['green']
    ))
    fig.add_trace(go.Bar(
        name='P95 Latency',
        x=names,
        y=[r.p95_latency_ms for r in results],
        marker_color=COLORS['orange']
    ))
    fig.add_trace(go.Bar(
        name='P99 Latency',
        x=names,
        y=[r.p99_latency_ms for r in results],
        marker_color=COLORS['red']
    ))
    
    fig.update_layout(
        title='总延迟 (Latency) 对比',
        xaxis_title='测试名称',
        yaxis_title='时间 (ms)',
        barmode='group',
        height=500,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_throughput_chart(results: List[FullTestResult]) -> str:
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
            marker_color=COLORS['purple'],
            name='Token Throughput'
        ),
        row=1, col=1
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.rps for r in results],
            marker_color=COLORS['cyan'],
            name='RPS'
        ),
        row=1, col=2
    )
    
    fig.update_layout(
        title='吞吐量对比',
        height=400,
        showlegend=False,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_radar_chart(results: List[FullTestResult]) -> str:
    """Create radar chart for multi-dimensional comparison."""
    if not results:
        return ""
    
    # Normalize values for radar chart (higher is better for all)
    max_ttft = max(r.avg_ttft_ms for r in results) or 1
    max_latency = max(r.avg_latency_ms for r in results) or 1
    max_throughput = max(r.token_throughput for r in results) or 1
    max_rps = max(r.rps for r in results) or 1
    max_summary_tps = max(r.summary_tokens_per_second for r in results) or 1
    
    fig = go.Figure()
    
    categories = ['响应速度<br>(1/TTFT)', '生成速度<br>(1/Latency)', 
                  '吞吐量', 'RPS', '成功率', '会议纪要<br>Token/s']
    
    for r in results:
        # Invert TTFT and Latency so higher is better
        ttft_score = (1 - r.avg_ttft_ms / max_ttft) * 100 if max_ttft > 0 else 0
        latency_score = (1 - r.avg_latency_ms / max_latency) * 100 if max_latency > 0 else 0
        throughput_score = (r.token_throughput / max_throughput) * 100 if max_throughput > 0 else 0
        rps_score = (r.rps / max_rps) * 100 if max_rps > 0 else 0
        success_score = r.success_rate * 100
        summary_score = (r.summary_tokens_per_second / max_summary_tps) * 100 if max_summary_tps > 0 else 0
        
        fig.add_trace(go.Scatterpolar(
            r=[ttft_score, latency_score, throughput_score, rps_score, success_score, summary_score],
            theta=categories,
            fill='toself',
            name=r.name
        ))
    
    fig.update_layout(
        title='综合性能雷达图',
        polar=dict(
            radialaxis=dict(
                visible=True,
                range=[0, 100],
                gridcolor='rgba(255,255,255,0.1)'
            ),
            bgcolor='rgba(0,0,0,0)',
            angularaxis=dict(gridcolor='rgba(255,255,255,0.1)')
        ),
        height=500,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_ttft_distribution_chart(results: List[FullTestResult]) -> str:
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
        height=400,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_latency_distribution_chart(results: List[FullTestResult]) -> str:
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
        height=400,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_summary_chart(results: List[FullTestResult]) -> str:
    """Create meeting summary performance comparison chart."""
    names = [r.name for r in results]
    
    fig = make_subplots(
        rows=1, cols=3,
        subplot_titles=('Token/s', '总 Tokens', '处理时间 (秒)')
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.summary_tokens_per_second for r in results],
            marker_color=COLORS['red'],
            name='Token/s'
        ),
        row=1, col=1
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.summary_total_tokens for r in results],
            marker_color=COLORS['blue'],
            name='Total Tokens'
        ),
        row=1, col=2
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.summary_processing_time_sec for r in results],
            marker_color=COLORS['green'],
            name='Processing Time'
        ),
        row=1, col=3
    )
    
    fig.update_layout(
        title='会议纪要性能对比',
        height=400,
        showlegend=False,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_long_context_chart(results: List[FullTestResult]) -> str:
    """Create long context test comparison chart."""
    fig = make_subplots(
        rows=1, cols=3,
        subplot_titles=('最大支持上下文 (字符)', '平均 TTFT (ms)', '平均吞吐 (tokens/s)')
    )
    
    names = [r.name for r in results]
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.lc_max_supported for r in results],
            marker_color=COLORS['purple'],
            name='Max Context'
        ),
        row=1, col=1
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.lc_avg_ttft_ms for r in results],
            marker_color=COLORS['blue'],
            name='Avg TTFT'
        ),
        row=1, col=2
    )
    
    fig.add_trace(
        go.Bar(
            x=names,
            y=[r.lc_avg_throughput for r in results],
            marker_color=COLORS['green'],
            name='Avg Throughput'
        ),
        row=1, col=3
    )
    
    fig.update_layout(
        title='长上下文测试对比',
        height=400,
        showlegend=False,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def create_long_context_detail_chart(results: List[FullTestResult]) -> str:
    """Create detailed long context performance chart showing TTFT vs context length."""
    fig = go.Figure()
    
    for r in results:
        if r.lc_results:
            x_vals = [res["context_length"] / 1000 for res in r.lc_results if res["success"]]  # Convert to K
            y_ttft = [res["ttft_ms"] for res in r.lc_results if res["success"]]
            
            if x_vals and y_ttft:
                fig.add_trace(go.Scatter(
                    x=x_vals,
                    y=y_ttft,
                    mode='lines+markers',
                    name=r.name,
                    marker=dict(size=10),
                    line=dict(width=3)
                ))
    
    fig.update_layout(
        title='长上下文 TTFT 曲线对比',
        xaxis_title='上下文长度 (K字符)',
        yaxis_title='TTFT (ms)',
        height=450,
        **DARK_LAYOUT
    )
    
    return fig.to_html(full_html=False, include_plotlyjs=False)


def generate_html_report(results: List[FullTestResult], output_path: str) -> None:
    """Generate the complete HTML comparison report with premium styling."""
    
    # Update chart templates to use dark theme
    import plotly.io as pio
    pio.templates.default = "plotly_dark"
        # Generate all charts
    ttft_chart = create_ttft_chart(results)
    latency_chart = create_latency_chart(results)
    throughput_chart = create_throughput_chart(results)
    radar_chart = create_radar_chart(results)
    ttft_dist_chart = create_ttft_distribution_chart(results)
    latency_dist_chart = create_latency_distribution_chart(results)
    long_context_chart = create_long_context_chart(results)
    long_context_detail_chart = create_long_context_detail_chart(results)
    summary_chart = create_summary_chart(results)
    
    # Generate Phase 1 summary table
    phase1_rows = ""
    for r in results:
        phase1_rows += f"""
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
    
    # Generate Phase 2 Function Call table
    fc_rows = ""
    for r in results:
        status = "✅ 支持" if r.fc_supported else "❌ 不支持"
        status_class = "success" if r.fc_supported else "error"
        fc_rows += f"""
        <tr>
            <td>{r.name}</td>
            <td class="{status_class}">{status}</td>
            <td>{r.fc_function_name or '-'}</td>
            <td><code>{r.fc_arguments or '-'}</code></td>
            <td>{f'{r.fc_latency_ms:.2f}' if r.fc_latency_ms else '-'}</td>
        </tr>
        """
    
    # Generate Phase 3 Long Context table
    lc_rows = ""
    for r in results:
        lc_rows += f"""
        <tr>
            <td>{r.name}</td>
            <td>{r.lc_max_supported:,} 字符</td>
            <td>{r.lc_avg_ttft_ms:.2f}</td>
            <td>{r.lc_avg_latency_ms:.2f}</td>
            <td>{r.lc_avg_throughput:.2f}</td>
        </tr>
        """
    
    # Generate Phase 4 Summary table
    summary_rows = ""
    for r in results:
        summary_rows += f"""
        <tr>
            <td>{r.name}</td>
            <td>{r.summary_total_chunks}</td>
            <td>{r.summary_prompt_tokens:,}</td>
            <td>{r.summary_completion_tokens:,}</td>
            <td>{r.summary_total_tokens:,}</td>
            <td>{r.summary_processing_time_sec:.2f}</td>
            <td>{r.summary_tokens_per_second:.2f}</td>
        </tr>
        """
    
    html_content = f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM FullTest 对比报告</title>
    <script src="https://cdn.plot.ly/plotly-2.27.0.min.js"></script>
    <style>
        @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600&family=Plus+Jakarta+Sans:wght@400;500;600;700&display=swap');
        
        :root {{
            --color-bg-primary: #0a0a0f;
            --color-bg-secondary: #12121a;
            --color-bg-card: rgba(255, 255, 255, 0.03);
            --color-border: rgba(255, 255, 255, 0.08);
            --color-text-primary: #f0f0f5;
            --color-text-secondary: #8b8b9e;
            --color-accent-blue: #6366f1;
            --color-accent-purple: #8b5cf6;
            --color-accent-pink: #ec4899;
            --color-accent-green: #10b981;
            --color-accent-red: #f43f5e;
            --color-accent-orange: #f59e0b;
            --font-sans: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            --font-mono: 'JetBrains Mono', 'SF Mono', 'Fira Code', monospace;
        }}
        
        * {{ margin: 0; padding: 0; box-sizing: border-box; }}
        html {{ scroll-behavior: smooth; }}
        
        body {{
            font-family: var(--font-sans);
            background: var(--color-bg-primary);
            color: var(--color-text-primary);
            min-height: 100vh;
            line-height: 1.6;
        }}
        
        body::before {{
            content: '';
            position: fixed;
            top: 0; left: 0; right: 0; bottom: 0;
            background: 
                radial-gradient(ellipse 80% 50% at 50% -20%, rgba(99, 102, 241, 0.15), transparent),
                radial-gradient(ellipse 60% 40% at 100% 0%, rgba(139, 92, 246, 0.1), transparent),
                radial-gradient(ellipse 50% 30% at 0% 100%, rgba(236, 72, 153, 0.08), transparent);
            pointer-events: none;
            z-index: -1;
        }}
        
        .container {{ max-width: 1400px; margin: 0 auto; padding: 40px 24px; }}
        
        .header {{
            text-align: center;
            padding: 60px 40px;
            margin-bottom: 48px;
            background: linear-gradient(135deg, rgba(99, 102, 241, 0.1) 0%, rgba(139, 92, 246, 0.05) 100%);
            border: 1px solid var(--color-border);
            border-radius: 24px;
            backdrop-filter: blur(20px);
            position: relative;
            overflow: hidden;
        }}
        
        .header::before {{
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0;
            height: 1px;
            background: linear-gradient(90deg, transparent, rgba(99, 102, 241, 0.5), rgba(139, 92, 246, 0.5), transparent);
        }}
        
        .logo-mark {{
            display: inline-flex;
            align-items: center;
            justify-content: center;
            width: 72px;
            height: 72px;
            background: linear-gradient(135deg, var(--color-accent-blue), var(--color-accent-purple));
            border-radius: 20px;
            margin-bottom: 24px;
            box-shadow: 0 20px 40px rgba(99, 102, 241, 0.3);
        }}
        
        .logo-mark svg {{ width: 40px; height: 40px; fill: white; }}
        
        h1 {{
            font-size: 2.75rem;
            font-weight: 700;
            letter-spacing: -0.02em;
            background: linear-gradient(135deg, #fff 0%, #a5b4fc 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
            margin-bottom: 12px;
        }}
        
        .subtitle {{
            color: var(--color-text-secondary);
            font-size: 1.1rem;
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 16px;
            flex-wrap: wrap;
        }}
        
        .section {{
            background: var(--color-bg-card);
            border: 1px solid var(--color-border);
            border-radius: 20px;
            padding: 32px;
            margin-bottom: 32px;
            backdrop-filter: blur(10px);
            transition: all 0.3s ease;
            animation: fadeIn 0.5s ease forwards;
        }}
        
        .section:hover {{
            border-color: rgba(99, 102, 241, 0.3);
            box-shadow: 0 8px 32px rgba(99, 102, 241, 0.1);
        }}
        
        .section h2 {{
            font-size: 1.5rem;
            font-weight: 600;
            margin-bottom: 24px;
            display: flex;
            align-items: center;
            gap: 12px;
            color: var(--color-text-primary);
        }}
        
        .phase-badge {{
            display: inline-flex;
            align-items: center;
            justify-content: center;
            padding: 6px 14px;
            border-radius: 10px;
            font-size: 0.85rem;
            font-weight: 600;
            letter-spacing: 0.02em;
        }}
        
        .phase-1 {{ background: linear-gradient(135deg, var(--color-accent-blue), var(--color-accent-purple)); color: white; }}
        .phase-2 {{ background: linear-gradient(135deg, var(--color-accent-purple), var(--color-accent-pink)); color: white; }}
        .phase-3 {{ background: linear-gradient(135deg, var(--color-accent-pink), var(--color-accent-orange)); color: white; }}
        .phase-4 {{ background: linear-gradient(135deg, var(--color-accent-green), #0ea5e9); color: white; }}
        
        .table-wrapper {{
            overflow-x: auto;
            border-radius: 12px;
            border: 1px solid var(--color-border);
            background: rgba(0, 0, 0, 0.2);
        }}
        
        table {{ width: 100%; border-collapse: collapse; font-size: 0.95rem; }}
        
        th {{
            background: rgba(99, 102, 241, 0.15);
            color: var(--color-text-primary);
            font-weight: 600;
            text-align: left;
            padding: 16px 20px;
            white-space: nowrap;
            border-bottom: 1px solid var(--color-border);
            text-transform: uppercase;
            font-size: 0.8rem;
            letter-spacing: 0.05em;
        }}
        
        td {{
            padding: 14px 20px;
            border-bottom: 1px solid var(--color-border);
            color: var(--color-text-secondary);
        }}
        
        tr:last-child td {{ border-bottom: none; }}
        tr:hover td {{ background: rgba(255, 255, 255, 0.02); }}
        
        .success {{ color: var(--color-accent-green); font-weight: 600; }}
        .error {{ color: var(--color-accent-red); font-weight: 600; }}
        
        code {{
            font-family: var(--font-mono);
            background: rgba(0, 0, 0, 0.3);
            padding: 4px 8px;
            border-radius: 6px;
            font-size: 0.85rem;
            color: var(--color-accent-purple);
        }}
        
        .chart-container {{
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid var(--color-border);
            border-radius: 16px;
            padding: 24px;
            margin-top: 20px;
        }}
        
        .chart-row {{
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
            gap: 24px;
        }}
        
        .footer {{
            text-align: center;
            padding: 40px 20px;
            color: var(--color-text-secondary);
            font-size: 0.9rem;
        }}
        
        .footer a {{ color: var(--color-accent-blue); text-decoration: none; }}
        .footer a:hover {{ color: var(--color-accent-purple); }}
        
        @keyframes fadeIn {{
            from {{ opacity: 0; transform: translateY(20px); }}
            to {{ opacity: 1; transform: translateY(0); }}
        }}
        
        @media (max-width: 768px) {{
            .container {{ padding: 20px 16px; }}
            .header {{ padding: 40px 20px; }}
            h1 {{ font-size: 2rem; }}
            .section {{ padding: 20px; }}
            .chart-row {{ grid-template-columns: 1fr; }}
            th, td {{ padding: 10px 12px; font-size: 0.85rem; }}
        }}
    </style>
</head>
<body>
    <div class="container">
        <header class="header">
            <div class="logo-mark">
                <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M13 3L4 14h7l-2 7 9-11h-7l2-7z" fill="currentColor"/>
                </svg>
            </div>
            <h1>LLM FullTest 对比报告</h1>
            <p class="subtitle">
                <span>📅 {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}</span>
                <span>📊 共 {len(results)} 个测试</span>
            </p>
        </header>
        
        <div class="section">
            <h2><span class="phase-badge phase-1">Phase 1</span> 性能测试汇总</h2>
            <div class="table-wrapper">
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
                        {phase1_rows}
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
        
        <div class="section">
            <h2><span class="phase-badge phase-2">Phase 2</span> Function Call 测试对比</h2>
            <div class="table-wrapper">
                <table>
                    <thead>
                        <tr>
                            <th>测试名称</th>
                            <th>支持状态</th>
                            <th>函数名</th>
                            <th>参数</th>
                            <th>延迟 (ms)</th>
                        </tr>
                    </thead>
                    <tbody>
                        {fc_rows}
                    </tbody>
                </table>
            </div>
        </div>
        
        <div class="section">
            <h2><span class="phase-badge phase-3">Phase 3</span> 长上下文测试对比</h2>
            <div class="table-wrapper">
                <table>
                    <thead>
                        <tr>
                            <th>测试名称</th>
                            <th>最大支持上下文</th>
                            <th>平均 TTFT (ms)</th>
                            <th>平均 Latency (ms)</th>
                            <th>平均吞吐 (tok/s)</th>
                        </tr>
                    </thead>
                    <tbody>
                        {lc_rows}
                    </tbody>
                </table>
            </div>
            <div class="chart-container">
                {long_context_chart}
            </div>
            <div class="chart-container">
                {long_context_detail_chart}
            </div>
        </div>
        
        <div class="section">
            <h2><span class="phase-badge phase-4">Phase 4</span> 会议纪要性能对比</h2>
            <div class="table-wrapper">
                <table>
                    <thead>
                        <tr>
                            <th>测试名称</th>
                            <th>分片数</th>
                            <th>Prompt Tokens</th>
                            <th>Completion Tokens</th>
                            <th>总 Tokens</th>
                            <th>处理时间 (秒)</th>
                            <th>Token/s</th>
                        </tr>
                    </thead>
                    <tbody>
                        {summary_rows}
                    </tbody>
                </table>
            </div>
            <div class="chart-container">
                {summary_chart}
            </div>
        </div>
        
        <footer class="footer">
            <p>Generated by LLM Benchmark Kit | 
               <a href="https://github.com/brianxiadong/llm-benchmark-kit">GitHub</a>
            </p>
        </footer>
    </div>
</body>
</html>
"""
    
    with open(output_path, 'w', encoding='utf-8') as f:
        f.write(html_content)
    
    print(f"✅ Report generated: {output_path}")


def main():
    parser = argparse.ArgumentParser(
        description='LLM FullTest Comparison Report Generator',
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
        help='Input directory containing fulltest results (default: ../../output)'
    )
    parser.add_argument(
        '--output', '-o',
        default='fulltest_comparison.html',
        help='Output HTML report path (default: fulltest_comparison.html)'
    )
    parser.add_argument(
        '--pattern', '-p',
        default=None,
        help='Filter directories by pattern (e.g., "fulltest_")'
    )
    
    args = parser.parse_args()
    
    print(f"🔍 Scanning {args.input} for fulltest results...")
    
    # Find all fulltest directories
    result_dirs = find_fulltest_dirs(args.input, args.pattern)
    
    if not result_dirs:
        print("❌ No fulltest results found!")
        sys.exit(1)
    
    print(f"📁 Found {len(result_dirs)} fulltest results:")
    for d in result_dirs:
        print(f"   - {d.name}")
    
    # Parse all results
    results = []
    for d in result_dirs:
        result = parse_fulltest_result(d)
        if result:
            results.append(result)
    
    if not results:
        print("❌ Failed to parse any fulltest results!")
        sys.exit(1)
    
    print(f"\n📊 Generating comparison report...")
    
    # Generate report
    generate_html_report(results, args.output)
    
    print(f"\n🎉 Done! Open {args.output} in your browser to view the report.")


if __name__ == '__main__':
    main()
