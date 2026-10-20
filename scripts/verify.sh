#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# 本地/CI 全量校验：格式、静态检查、单测、构建、编排。
# 这就是 .github/workflows/ci.yml 在跑的东西，本地先跑一遍再推。
#
# 用法：
#   ./scripts/verify.sh            # 全部
#   ./scripts/verify.sh --no-web   # 跳过前端（没装 node 时）
#   ./scripts/verify.sh --quick    # 跳过构建，只跑格式和单测
# ---------------------------------------------------------------------------
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_WEB=1
QUICK=0
FAILED=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-web) RUN_WEB=0; shift ;;
    --quick)  QUICK=1; shift ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

step() { echo; echo "===== $* ====="; }
check() {
  local name="$1"; shift
  if "$@"; then
    echo "PASS  $name"
  else
    echo "FAIL  $name"
    FAILED+=("$name")
  fi
}

step "Go 后端"
if command -v go >/dev/null 2>&1; then
  cd "$REPO_ROOT/server"
  check "gofmt" bash -c '[ -z "$(gofmt -l .)" ]'
  check "go vet" go vet ./...
  check "go test" go test ./...
  if [[ "$QUICK" -eq 0 ]]; then
    check "go build" bash -c 'CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/k20mon-server-verify .'
  fi
else
  echo "SKIP  go 未安装"
fi

step "控制代理（Python）"
if command -v python3 >/dev/null 2>&1; then
  cd "$REPO_ROOT/deploy/agent"
  check "py_compile" python3 -m py_compile k20mon_agent.py rate_sync.py
  check "unittest" python3 -m unittest test_agent test_rate_sync
else
  echo "SKIP  python3 未安装"
fi

if [[ "$RUN_WEB" -eq 1 ]]; then
  step "前端（Vue）"
  if command -v npm >/dev/null 2>&1; then
    cd "$REPO_ROOT/web"
    if [[ ! -d node_modules ]]; then
      echo "==> 安装依赖（首次会比较慢）"
      npm ci --no-audit --no-fund || npm install --no-audit --no-fund
    fi
    check "npm test" npm run test
    if [[ "$QUICK" -eq 0 ]]; then
      check "npm run build" npm run build
    fi
  else
    echo "SKIP  npm 未安装"
  fi
else
  step "前端（已跳过）"
fi

step "Compose 校验"
if command -v docker >/dev/null 2>&1; then
  cd "$REPO_ROOT/deploy"
  cp -n .env.example .env 2>/dev/null || true
  cp -n config.yaml.example config.yaml 2>/dev/null || true
  check "docker compose config" bash -c 'docker compose config > /dev/null'
else
  echo "SKIP  docker 未安装"
fi

echo
echo "======================================================="
if [[ ${#FAILED[@]} -eq 0 ]]; then
  echo " 全部通过"
  exit 0
fi
echo " 失败的检查项："
for item in "${FAILED[@]}"; do echo "   - $item"; done
exit 1
