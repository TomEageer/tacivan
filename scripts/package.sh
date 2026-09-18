#!/bin/sh
# 打发行用的 DMG：通用包 + 全尺寸图标 + 自签名，再做隔离审计（不能带插件和任何私有信息）。
set -e
cd "$(dirname "$0")/.."
VER=$(grep -o 'Version = "[^"]*"' internal/app/app.go | cut -d'"' -f2)
wails build -clean -trimpath -platform darwin/universal 2>&1 | grep -E "Built|rror" || true
cp design/icons/tacivan-day.icns build/bin/Tacivan.app/Contents/Resources/iconfile.icns
codesign --force --deep -s - build/bin/Tacivan.app 2>/dev/null
# 隔离审计：.audit-patterns（不入库，每行一个 grep 模式）里的字符串一个都不能出现在包里。
if [ -s .audit-patterns ] && grep -rlqf .audit-patterns build/bin/Tacivan.app 2>/dev/null; then
  echo "审计失败：包里有私有字符串" >&2; exit 1
fi
if [ -d build/bin/Tacivan.app/Contents/Resources/plugins ]; then
  echo "审计失败：包里带了插件" >&2; exit 1
fi
rm -rf /tmp/tacivan-dmg && mkdir -p /tmp/tacivan-dmg dist
cp -R build/bin/Tacivan.app /tmp/tacivan-dmg/ && ln -s /Applications /tmp/tacivan-dmg/Applications
hdiutil create -volname "Tacivan" -srcfolder /tmp/tacivan-dmg -ov -format UDZO "dist/Tacivan-$VER.dmg" >/dev/null
ls -la "dist/Tacivan-$VER.dmg"; shasum -a 256 "dist/Tacivan-$VER.dmg"
