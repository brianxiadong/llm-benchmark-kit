#!/bin/bash
# LLM FullTest 对比报告生成脚本
# 用法: ./compare.sh [pattern] [output_name]
#   pattern     - 可选，目录名过滤模式 (默认: fulltest_)
#   output_name - 可选，输出文件名 (默认: comparison_<timestamp>.html)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPARE_SCRIPT="${SCRIPT_DIR}/tools/compare/compare_report.py"
OUTPUT_DIR="${SCRIPT_DIR}/output"
LOCAL_DIR="${SCRIPT_DIR}/local"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印带颜色的消息
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 检查 Python 和依赖
check_dependencies() {
    if ! command -v python3 &> /dev/null; then
        error "Python3 未安装"
        exit 1
    fi
    
    # 检查 plotly 是否安装
    if ! python3 -c "import plotly" 2>/dev/null; then
        warn "正在安装依赖..."
        pip3 install -r "${SCRIPT_DIR}/tools/compare/requirements.txt" -q
        success "依赖安装完成"
    fi
}

# 列出可用的测试结果
list_results() {
    info "可用的测试结果:"
    echo ""
    local count=0
    for dir in "${OUTPUT_DIR}"/fulltest_*; do
        if [ -d "$dir" ]; then
            local name=$(basename "$dir")
            local summary="${dir}/benchmark/summary.json"
            if [ -f "$summary" ]; then
                echo "  • ${name}"
                ((count++))
            fi
        fi
    done
    echo ""
    info "共 ${count} 个测试结果"
}

# 主函数
main() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════╗"
    echo "║       LLM Benchmark Kit - 对比报告生成器                  ║"
    echo "╚══════════════════════════════════════════════════════════╝"
    echo ""
    
    # 检查依赖
    check_dependencies
    
    # 确保 local 目录存在
    mkdir -p "${LOCAL_DIR}"
    
    # 参数处理
    PATTERN="${1:-fulltest_}"
    TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
    OUTPUT_NAME="${2:-comparison_${TIMESTAMP}.html}"
    OUTPUT_PATH="${LOCAL_DIR}/${OUTPUT_NAME}"
    
    # 检查输出目录是否有测试结果
    if [ ! -d "${OUTPUT_DIR}" ]; then
        error "输出目录不存在: ${OUTPUT_DIR}"
        exit 1
    fi
    
    local result_count=$(find "${OUTPUT_DIR}" -maxdepth 1 -type d -name "${PATTERN}*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$result_count" -eq 0 ]; then
        error "未找到匹配的测试结果 (pattern: ${PATTERN}*)"
        list_results
        exit 1
    fi
    
    info "扫描目录: ${OUTPUT_DIR}"
    info "过滤模式: ${PATTERN}*"
    info "找到 ${result_count} 个测试结果"
    echo ""
    
    # 生成报告
    info "正在生成对比报告..."
    python3 "${COMPARE_SCRIPT}" \
        --input "${OUTPUT_DIR}" \
        --output "${OUTPUT_PATH}" \
        --pattern "${PATTERN}"
    
    if [ -f "${OUTPUT_PATH}" ]; then
        success "报告生成成功!"
        echo ""
        echo "  📊 报告路径: ${OUTPUT_PATH}"
        echo "  📁 相对路径: local/${OUTPUT_NAME}"
        echo ""
        
        # macOS 自动打开
        if [[ "$OSTYPE" == "darwin"* ]]; then
            read -p "是否在浏览器中打开报告? [Y/n] " -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]] || [[ -z $REPLY ]]; then
                open "${OUTPUT_PATH}"
            fi
        else
            info "使用浏览器打开: ${OUTPUT_PATH}"
        fi
    else
        error "报告生成失败"
        exit 1
    fi
}

# 帮助信息
if [[ "$1" == "-h" ]] || [[ "$1" == "--help" ]]; then
    echo "用法: ./compare.sh [pattern] [output_name]"
    echo ""
    echo "参数:"
    echo "  pattern      目录名过滤模式 (默认: fulltest_)"
    echo "  output_name  输出文件名 (默认: comparison_<timestamp>.html)"
    echo ""
    echo "示例:"
    echo "  ./compare.sh                           # 对比所有 fulltest_* 结果"
    echo "  ./compare.sh deepseek                  # 只对比包含 'deepseek' 的结果"
    echo "  ./compare.sh fulltest_ my_report.html  # 指定输出文件名"
    echo ""
    list_results
    exit 0
fi

main "$@"
