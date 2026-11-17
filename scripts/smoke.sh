#!/usr/bin/env bash
# 对已启动的服务做真实冒烟（默认 http://127.0.0.1:8787）
set -euo pipefail
cd "$(dirname "$0")/.."
node scripts/smoke.mjs
