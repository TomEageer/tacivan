#!/bin/sh
# 重新生成 vendor/ 并打上 patches/ 里的补丁。改了 go.mod 之后跑一次。
set -e
cd "$(dirname "$0")/.."
rm -rf vendor
go mod vendor
for p in patches/*.diff; do
  patch -p1 -d vendor/github.com/wailsapp/wails/v2 < "$p"
done
echo "vendor 已就绪"
