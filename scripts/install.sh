#!/bin/sh
# 构建并装到 /Applications，然后重新拉起。之后每次更新都走这一条。
set -e
cd "$(dirname "$0")/.."
wails build -clean -trimpath -platform darwin/universal 2>&1 | grep -E "Built|rror" || true
# Wails 只从一张 1024 PNG 缩出 5 个 @2x 尺寸；换成从 SVG 逐尺寸渲染的全套 10 个
# （@1x 给非 Retina，@2x 给 Retina / 4K / 5K），16/32 那几档不再是缩图，边缘清楚。
cp design/icons/tacivan-day.icns build/bin/Tacivan.app/Contents/Resources/iconfile.icns
codesign --force --deep -s - build/bin/Tacivan.app 2>/dev/null
pkill -f 'Tacivan.app/Contents/MacOS/Tacivan' 2>/dev/null || true
sleep 1
rm -rf /Applications/Tacivan.app
cp -R build/bin/Tacivan.app /Applications/Tacivan.app
open /Applications/Tacivan.app
echo "已更新 /Applications/Tacivan.app ($(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' /Applications/Tacivan.app/Contents/Info.plist 2>/dev/null))"
