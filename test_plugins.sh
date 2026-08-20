#!/bin/bash
set -e

echo "1. Downloading Go dependencies..."
go mod tidy

echo "2. Building RPC Dummy Plugin..."
cd plugins/dummy_rpc
go build -o f4-rpc-dummy-plugin
cd ../..

echo "3. Running f4 in test mode (Output will go to debug.log)..."
rm -f debug.log
VTUI_DEBUG=1 go run ./cmd/f4 -test-plugins

echo ""
echo "=== debug.log Output ==="
cat debug.log
