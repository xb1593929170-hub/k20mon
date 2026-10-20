#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# 打包源码发布包（给「没有 git、只能上传压缩包」的部署场景用）。
#
# 用法：
#   ./scripts/package-release.sh                 # 版本号用日期
#   ./scripts/package-release.sh --version 1.0.0
#   ./scripts/package-release.sh --with-web-dist # 额外把前端构建产物打进去
#
# 产物放在 dist/ 下：
#   k20mon-<version>.tar.gz         源码包（不含 .git / node_modules / dist）
#   k20mon-<version>.tar.gz.sha256  校验和
#
# 设备侧部署：
#   tar -xzf k20mon-1.0.0.tar.gz -C /opt
#   cd /opt/k20mon && ./deploy/sql/create-admin.sh ... && cd deploy && docker compose up -d --build
# ---------------------------------------------------------------------------
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/dist}"
VERSION="$(date +%Y%m%d)"
WITH_WEB_DIST=0

info() { echo "==> $*"; }
die()  { echo "错误: $*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)        VERSION="$2"; shift 2 ;;
    --with-web-dist)  WITH_WEB_DIST=1; shift ;;
    -h|--help)        sed -n '2,18p' "$0"; exit 0 ;;
    *)                die "未知参数: $1（用 --help 看用法）" ;;
  esac
done

command -v tar >/dev/null || die "需要 tar"

mkdir -p "$OUT_DIR"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

STAGE="$WORK/k20mon"
info "复制源码到暂存目录 ..."
mkdir -p "$STAGE"
(cd "$REPO_ROOT" && tar cf - \
  --exclude='.git' \
  --exclude='.github' \
  --exclude='node_modules' \
  --exclude='dist' \
  --exclude='__pycache__' \
  --exclude='*.pyc' \
  --exclude='deploy/config.yaml' \
  --exclude='./deploy/config.yaml' \
  --exclude='deploy/.env' \
  --exclude='./deploy/.env' \
  .) | (cd "$STAGE" && tar xf -)

# 保险一：暂存目录里检查本地配置（里面有数据库密码，绝不能进发布包）
for leaked in "deploy/config.yaml" "deploy/.env" "deploy/sql/.schema.tmp.sql"; do
  if [[ -e "$STAGE/$leaked" ]]; then
    die "$leaked 出现在了发布包里，已中止。请检查 .gitignore 和 tar 的 --exclude 规则"
  fi
done

if [[ "$WITH_WEB_DIST" -eq 1 ]]; then
  info "构建前端产物并打入包内 ..."
  ( cd "$REPO_ROOT/web" && npm ci --no-audit --no-fund && npm run build )
  mkdir -p "$STAGE/web-dist"
  cp -r "$REPO_ROOT/web/dist/." "$STAGE/web-dist/"
  info "前端产物已放在 web-dist/（设备上可以直接用 nginx 托管，跳过 node 构建）"
fi

# 记录来源信息，方便追溯这个包是从哪个 commit 打的
if command -v git >/dev/null 2>&1 && [[ -d "$REPO_ROOT/.git" ]]; then
  {
    echo "version:  $VERSION"
    echo "commit:   $(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
    echo "branch:   $(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
    echo "built_at: $(date -Iseconds)"
  } > "$STAGE/BUILD-INFO.txt"
fi

ARCHIVE="$OUT_DIR/k20mon-${VERSION}.tar.gz"
info "打包 $ARCHIVE ..."
( cd "$WORK" && tar czf "$ARCHIVE" k20mon )

# 保险二：直接检查成品压缩包的内容。
# 比检查暂存目录更可靠 —— 不受 tar 在不同平台上 --exclude 匹配差异的影响。
FORBIDDEN="$(tar tzf "$ARCHIVE" | grep -E '(^|/)(deploy/(config\.yaml|\.env)|\.schema\.tmp\.sql)$' || true)"
if [[ -n "$FORBIDDEN" ]]; then
  rm -f "$ARCHIVE"
  echo "错误：发布包里出现了不该有的文件，已删除成品：" >&2
  echo "$FORBIDDEN" | sed 's/^/  - /' >&2
  echo "这些文件含数据库密码，请修掉 --exclude 规则后重试。" >&2
  exit 1
fi
info "内容检查通过（无本地配置 / 密钥）"

if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$OUT_DIR" && sha256sum "$(basename "$ARCHIVE")" > "$(basename "$ARCHIVE").sha256" )
elif command -v shasum >/dev/null 2>&1; then
  ( cd "$OUT_DIR" && shasum -a 256 "$(basename "$ARCHIVE")" > "$(basename "$ARCHIVE").sha256" )
fi

FILE_COUNT="$(tar tzf "$ARCHIVE" | grep -c '[^/]$' || true)"
SIZE="$(du -h "$ARCHIVE" | cut -f1)"

echo
echo "----------------------------------------------------------------------"
echo " 发布包 : $ARCHIVE"
echo " 大小   : $SIZE   文件数: $FILE_COUNT"
[[ -f "${ARCHIVE}.sha256" ]] && echo " 校验   : $(cut -d' ' -f1 "${ARCHIVE}.sha256")"
echo
echo " 设备侧："
echo "   scp $ARCHIVE root@<设备>:/tmp/"
echo "   ssh root@<设备> 'tar -xzf /tmp/$(basename "$ARCHIVE") -C /opt'"
echo "   ssh root@<设备> 'cd /opt/k20mon && ./deploy/sql/create-admin.sh --help'"
echo "----------------------------------------------------------------------"
