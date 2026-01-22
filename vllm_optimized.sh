#!/bin/bash

# Optimized vLLM configuration for Qwen3-32B
# Uses all 8 GPUs and optimized settings for better throughput

vllm serve /data/modelscope/hub/models/Qwen/Qwen3-32B \
    --tensor-parallel-size 8 \
    --trust-remote-code \
    --port 30000 \
    --max-model-len 16384 \
    --gpu-memory-utilization 0.85 \
    --enable-auto-tool-choice \
    --tool-call-parser hermes \
    --max-num-batched-tokens 4096 \
    --max-num-seqs 8