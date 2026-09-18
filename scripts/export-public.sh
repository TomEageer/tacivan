#!/bin/sh
# 导出可公开的源码副本：不带插件、测试、竞品拆解、vendor、构建产物和任何本机私有文件。
# 用法：scripts/export-public.sh [目标目录，默认 ../tacivan]
set -e
cd "$(dirname "$0")/.."
DEST=${1:-../tacivan}
mkdir -p "$DEST"
rsync -a --delete \
  --exclude '.git' \
  --exclude 'plugins/' \
  --exclude 'vendor/' \
  --exclude 'build/bin/' \
  --exclude 'dist/' \
  --exclude 'node_modules/' \
  --exclude 'frontend/dist/' \
  --exclude '.DS_Store' \
  --exclude '.audit-patterns' \
  --exclude 'docs/private/' \
  --exclude 'design/icons/candidates/' \
  --exclude 'design/icons/sheets/' \
  --exclude '*_test.go' \
  --exclude '*.test.ts' \
  --exclude '*.log' \
  ./ "$DEST/"
# 再核对一遍：私有字符串一个都不能有
if [ -s .audit-patterns ] && grep -rIlqf .audit-patterns "$DEST" --exclude-dir=.git; then
  echo "导出失败：公开副本里仍有私有字符串：" >&2
  grep -rIlf .audit-patterns "$DEST" --exclude-dir=.git >&2
  exit 1
fi
if find "$DEST" -name '*_test.go' -o -name '*.test.ts' | grep -q .; then
  echo "导出失败：公开副本里仍有测试文件" >&2; exit 1
fi
echo "已导出到 $DEST"
