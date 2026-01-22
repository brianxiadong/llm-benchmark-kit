#!/bin/bash

# Memory-optimized vLLM configuration
# Reduces max sequence length to free up memory for higher concurrency

vllm serve /data/modelscope/hub/models/Qwen/Qwen3-32B \
    --tensor-parallel-size 4 \
    --trust-remote-code \
    --port 30000 \
    --max-model-len 8192 \
    --gpu-memory-utilization 0.75 \
    --enable-auto-tool-choice \
    --tool-call-parser hermes \
    --max-num-batched-tokens 2048 \
    --max-num-seqs 16 \
    --enable-prefix-caching