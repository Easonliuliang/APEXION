#!/bin/bash
set -e

# 检查 Go 是否安装
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install from https://go.dev/dl/"
    exit 1
fi

# 检查 API Key
if [ -z "$ANTHROPIC_API_KEY" ]; then
    echo "❌ ANTHROPIC_API_KEY is not set."
    echo "   export ANTHROPIC_API_KEY=sk-ant-..."
    exit 1
fi

cd "$(dirname "$0")"

echo "📦 Downloading dependencies..."
go mod tidy

echo "🔨 Building..."
go build -o aictl-poc .

echo "🚀 Starting aictl POC..."
echo ""
./aictl-poc
