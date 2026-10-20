#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# 构建 K20Mon 的 Docker 镜像（server + web）。
#
# 用法：
#   ./scripts/build-images.sh                      # tag 用日期时间
#   ./scripts/build-images.sh --tag 1.0.0
#   ./scripts/build-images.sh --tag 1.0.0 --push    # 推到 ${REGISTRY}
#   ./scripts/build-images.sh --cn                  # 用国内镜像源（goproxy.cn）
#   ./scripts/build-images.sh --multiarch           # buildx 构建 amd64+arm64
#   ./scripts/build-images.sh --save                # 额外导出镜像 tar，便于离线部署
#
# 环境变量：
#   REGISTRY         镜像仓库前缀，默认空（本地镜像）
#   K20MON_GOPROXY   Go 模块代理，默认 https://proxy.golang.org,direct
#   K20MON_SERVER_RUNTIME  后端运行时基础镜像，默认 alpine:3.20
#   K20MON_WEB_RUNTIME     前端运行时基础镜像，默认 nginx:alpine
# ---------------------------------------------------------------------------
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$REPO_ROOT/deploy"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/dist}"

REGISTRY="${REGISTRY:-}"
TAG="$(date +%Y%m%d%H%M%S)"
PUSH=0
MULTIARCH=0
SAVE=0

info() { echo "==> $*"; }
die()  { echo "错误: $*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)        TAG="$2"; shift 2 ;;
    --registry)   REGISTRY="$2"; REGISTRY="${REGISTRY%/}/"; shift 2 ;;
    --push)       PUSH=1; shift ;;
    --multiarch)  MULTIARCH=1; shift ;;
    --save)       SAVE=1; shift ;;
    --cn)         export K20MON_GOPROXY="https://goproxy.cn,direct"; shift ;;
    -h|--help)    sed -n '2,22p' "$0"; exit 0 ;;
    *)            die "未知参数: $1（用 --help 看用法）" ;;
  esac
done

[[ -f "$DEPLOY_DIR/docker-compose.yml" ]] || die "找不到 $DEPLOY_DIR/docker-compose.yml"

export K20MON_IMAGE_TAG="$TAG"
export K20MON_GOPROXY="${K20MON_GOPROXY:-https://proxy.golang.org,direct}"
export K20MON_SERVER_RUNTIME="${K20MON_SERVER_RUNTIME:-alpine:3.20}"
export K20MON_WEB_RUNTIME="${K20MON_WEB_RUNTIME:-nginx:alpine}"

SERVER_IMAGE="${REGISTRY}k20mon/server:${TAG}"
WEB_IMAGE="${REGISTRY}k20mon/web:${TAG}"

info "构建参数"
cat <<EOF
    tag            : $TAG
    registry       : ${REGISTRY:-<本地>}
    GOPROXY        : $K20MON_GOPROXY
    后端运行时镜像 : $K20MON_SERVER_RUNTIME
    前端运行时镜像 : $K20MON_WEB_RUNTIME
    输出镜像       : $SERVER_IMAGE / $WEB_IMAGE
EOF

# ---------------------------------------------------------------------------
# 预检：基础镜像是否本地已有 / 能不能拉。
# 在拉不到 Docker Hub 的机器上（手机很常见），docker build 会静默卡很久，
# 这里先快速探一次，把问题提前说清楚。
# ---------------------------------------------------------------------------
BASE_IMAGES=(
  "golang:1.24-alpine"
  "node:22-alpine"
  "$K20MON_SERVER_RUNTIME"
  "$K20MON_WEB_RUNTIME"
)
MISSING=()
for img in "${BASE_IMAGES[@]}"; do
  docker image inspect "$img" >/dev/null 2>&1 || MISSING+=("$img")
done

if [[ ${#MISSING[@]} -gt 0 ]]; then
  info "本地缺少这些基础镜像：${MISSING[*]}"
  if timeout 8 docker pull --quiet "${MISSING[0]}" >/dev/null 2>&1; then
    info "Docker Hub 可达，构建时按需拉取"
  else
    echo
    echo "警告：拉不到 Docker Hub，且上面这些镜像本地也没有。"
    echo
    echo "  三个办法（按推荐顺序）："
    echo "    1. 换成本地已有的镜像："
    echo "         export K20MON_SERVER_RUNTIME=alpine:3.20"
    echo "         export K20MON_WEB_RUNTIME=nginx:alpine"
    echo "      先看看本地有什么：docker images | grep -E 'alpine|nginx|golang|node'"
    echo "    2. 在能联网的机器上构建好，导出离线包再传过来："
    echo "         ./scripts/build-images.sh --tag $TAG --save"
    echo "         gunzip -c k20mon-images-$TAG.tar.gz | docker load"
    echo "    3. 配置镜像加速器后重试"
    echo
    echo "继续尝试构建（可能失败或长时间无响应）..."
    echo
  fi
fi

if [[ "$MULTIARCH" -eq 1 ]]; then
  command -v docker >/dev/null || die "需要 docker"
  docker buildx version >/dev/null 2>&1 || die "需要 docker buildx 插件"

  PLATFORMS="linux/amd64,linux/arm64"
  OUTPUT="--load"
  if [[ "$PUSH" -eq 1 ]]; then
    OUTPUT="--push"
  else
    info "提示：多架构构建不 --push 时无法 --load 到本地，将只做构建校验"
    OUTPUT=""
  fi

  info "buildx 构建 server（$PLATFORMS）..."
  docker buildx build --platform "$PLATFORMS" $OUTPUT \
    --build-arg GOPROXY="$K20MON_GOPROXY" \
    --build-arg RUNTIME_IMAGE="$K20MON_SERVER_RUNTIME" \
    -t "$SERVER_IMAGE" "$REPO_ROOT/server"

  info "buildx 构建 web（$PLATFORMS）..."
  docker buildx build --platform "$PLATFORMS" $OUTPUT \
    --build-arg RUNTIME_IMAGE="$K20MON_WEB_RUNTIME" \
    -t "$WEB_IMAGE" "$REPO_ROOT/web"
else
  info "docker compose build ..."
  ( cd "$DEPLOY_DIR" && docker compose build )
fi

info "构建完成"
docker images --format '{{.Repository}}:{{.Tag}}  {{.Size}}  ({{.CreatedSince}})' \
  | grep -E "k20mon/(server|web):${TAG}" || true

if [[ "$PUSH" -eq 1 && "$MULTIARCH" -eq 0 ]]; then
  info "推送镜像 ..."
  docker push "$SERVER_IMAGE"
  docker push "$WEB_IMAGE"
fi

if [[ "$SAVE" -eq 1 ]]; then
  mkdir -p "$OUT_DIR"
  ARCHIVE="$OUT_DIR/k20mon-images-${TAG}.tar"
  info "导出离线镜像包 $ARCHIVE ..."
  # 兼容没有多架构 manifest 的场景：直接 save，不依赖 buildx
  docker save "$SERVER_IMAGE" "$WEB_IMAGE" -o "$ARCHIVE"
  if command -v gzip >/dev/null 2>&1; then
    gzip -f "$ARCHIVE"
    ARCHIVE="${ARCHIVE}.gz"
  fi
  echo
  echo "----------------------------------------------------------------------"
  echo " 离线部署包已生成：$ARCHIVE"
  echo " 在目标机器上："
  echo "   gunzip -c $(basename "$ARCHIVE") | docker load"
  echo "   K20MON_IMAGE_TAG=${TAG} docker compose up -d --no-build"
  echo "----------------------------------------------------------------------"
fi

echo
echo "下一步："
echo "  1. 本地起服务   cd deploy && docker compose up -d"
echo "  2. 部署到设备   见 docs/DEPLOY.md"
