#!/usr/bin/env bash
# 构建本机平台的插件二进制、刷新包摘要并跑一遍宿主契约校验。
# 用法：scripts/prepare-package.sh [extension-root]（默认 plugins/sforum.related-topics）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="${1:-$ROOT/plugins/sforum.related-topics}"
SFORUM_DIR="${SFORUM_DIR:-$(cd "$ROOT/.." && pwd)/SForum}"

if [ ! -d "$SFORUM_DIR/apps/api" ]; then
  echo "找不到 SForum 检出目录：$SFORUM_DIR" >&2
  echo "可用 SFORUM_DIR=/path/to/SForum 指定，或把本仓库与 SForum 放在同一父目录下。" >&2
  exit 1
fi

# 与宿主 dev watcher（ExtensionPackage.BuildBackend）和 CI 同一组参数：
# CGO_ENABLED=0 + -buildvcs=false。否则同一份源码在「本地 prepare」与
# 「站点自动构建」两条路上会产出不同字节，摘要来回拉扯、永不收敛。
#
# 注意：工具链版本同样是构建身份的一部分，但这里**不**钉 GOTOOLCHAIN——
# 宿主 dev watcher 盯着本仓库，会用自己进程里的 go 重新构建并回写清单摘要，
# 钉一个与它不同的版本只会让摘要每几秒被改回去。摘要不是跨环境不变量，
# 真正需要精确的地方是发布流水线（每个平台各自重写）。
echo "==> 构建 backend/plugin（CGO_ENABLED=0）"
rm -f "$PKG/backend/plugin"
(cd "$PKG/backend" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$PKG/backend/plugin" ./)

echo "==> 刷新包摘要（extension digest --write）"
(cd "$SFORUM_DIR/apps/api" && go run ./cmd/sforum extension digest --write "$PKG")

echo "==> 校验清单（extension validate）"
(cd "$SFORUM_DIR/apps/api" && go run ./cmd/sforum extension validate "$PKG")

echo "==> 品牌资产（extension brand check）"
# 品牌不是运行时制品、也不进 packageFiles，但它随包树摘要变化，
# 所以同样在门禁里校验：icon.svg 的 viewBox、PNG 尺寸与字节上限。
(cd "$SFORUM_DIR/apps/api" && go run ./cmd/sforum extension brand check "$PKG")

echo "==> 宿主契约检查（extension test）"
(cd "$SFORUM_DIR/apps/api" && go run ./cmd/sforum extension test "$PKG")

echo "完成：$PKG"
