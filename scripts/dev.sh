#!/usr/bin/env bash
# 开发模式：Go 后端 :8787 + Vite 前端 :5173（/api 已代理）
set -euo pipefail
cd "$(dirname "$0")/.."

cleanup() {
  pkill -x anipick 2>/dev/null || true
  pkill -f "go run ." 2>/dev/null || true
}
trap cleanup EXIT

if [ ! -f .env ]; then
  echo "提示：未找到 .env，将使用内置默认值（数据库 ani_pick / 管理员 anipick）"
fi

echo "==> 启动 Go 后端 (:8787)"
(go run . &)

echo "==> 启动 Vite 前端 (:5173, /api -> 127.0.0.1:8787)"
cd frontend
npm run dev
