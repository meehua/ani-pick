#!/usr/bin/env bash
# 完整构建：前端 -> 嵌入 -> 检查 -> 单二进制
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> [1/4] 构建前端"
(cd frontend && npm install --no-audit --no-fund && npm run build)

echo "==> [2/4] 嵌入前端产物"
mkdir -p internal/server/dist
find internal/server/dist -mindepth 1 ! -name '.gitkeep' -delete
cp -r frontend/dist/. internal/server/dist/

echo "==> [3/4] Go 检查与测试"
gofmt -l . | tee /tmp/anipick-gofmt.txt
test ! -s /tmp/anipick-gofmt.txt
go vet ./...
go test ./...

echo "==> [4/4] 构建二进制"
go build -o anipick .

echo ""
echo "构建完成 -> ./anipick"
echo "首次运行前：cp .env.example .env && 修改数据库与管理员密码"
echo "运行：./anipick        管理后台：http://localhost:8787/admin"
