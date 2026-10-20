#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# 建库、建表、建管理员账号 —— 一条命令搞定。
#
# 用法：
#   ./create-admin.sh --host 127.0.0.1 --user root --password rootpass \
#       --db k20mon --username admin --password '你的登录密码'
#
# 参数（都能用环境变量代替，见下方默认值）：
#   --host        MySQL 主机       默认 127.0.0.1
#   --port        MySQL 端口       默认 3306
#   --user        MySQL 账号       默认 root
#   --password    MySQL 密码       （必填，或用 MYSQL_PWD 环境变量）
#   --db          数据库名         默认 k20mon
#   --username    要创建的应用账号  默认 admin
#   --app-pass    应用账号的密码    默认随机生成
#   --app-password
#   --skip-schema 只建管理员，不执行 01-schema.sql
#   --sql-only    只打印 SQL，不执行（方便 DBA 审）
#
# 依赖：mysql 客户端；没有的话自动用 docker 跑 ${MYSQL_CLIENT_IMAGE:-mysql:8} 镜像。
#       bcrypt 生成依赖 python3（没有 python3 也会自动用 docker 跑 python 镜像）。
#
# 可覆盖的环境变量：
#   MYSQL_CLIENT_IMAGE   docker 里的 mysql 客户端镜像   默认 mysql:8
#   PYTHON_IMAGE         docker 里的 python 镜像        默认 python:3.12-alpine
# ---------------------------------------------------------------------------
set -euo pipefail

HOST="${MYSQL_HOST:-127.0.0.1}"
PORT="${MYSQL_PORT:-3306}"
ROOT_USER="${MYSQL_USER:-root}"
ROOT_PASS="${MYSQL_PASSWORD:-${MYSQL_PWD:-}}"
DB="${K20MON_DB:-k20mon}"
ADMIN_USER="admin"
ADMIN_PASS=""
SKIP_SCHEMA=0
SQL_ONLY=0

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

MYSQL_CLIENT_IMAGE="${MYSQL_CLIENT_IMAGE:-mysql:8}"
PYTHON_IMAGE="${PYTHON_IMAGE:-python:3.12-alpine}"

die() { echo "错误: $*" >&2; exit 1; }
info() { echo "==> $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host)          HOST="$2"; shift 2 ;;
    --port)          PORT="$2"; shift 2 ;;
    --user)          ROOT_USER="$2"; shift 2 ;;
    --password)      ROOT_PASS="$2"; shift 2 ;;
    --db)            DB="$2"; shift 2 ;;
    --username)      ADMIN_USER="$2"; shift 2 ;;
    --app-pass|--app-password) ADMIN_PASS="$2"; shift 2 ;;
    --skip-schema)   SKIP_SCHEMA=1; shift ;;
    --sql-only)      SQL_ONLY=1; shift ;;
    -h|--help)       sed -n '2,26p' "$0"; exit 0 ;;
    *)               die "未知参数: $1（用 --help 看用法）" ;;
  esac
done

[[ -n "$ROOT_PASS" ]] || die "缺少 MySQL 密码，用 --password 或环境变量 MYSQL_PWD 指定"

if [[ -z "$ADMIN_PASS" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    ADMIN_PASS="$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-18)"
  else
    ADMIN_PASS="$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | cut -c1-18)"
  fi
  GENERATED_PASS=1
fi

# ---------------------------------------------------------------------------
# bcrypt：优先本地 python3，没有就用 docker
# ---------------------------------------------------------------------------
bcrypt_hash() {
  local password="$1"
  if command -v python3 >/dev/null 2>&1 && python3 -c 'import bcrypt' 2>/dev/null; then
    python3 -c "import bcrypt,sys;print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt(rounds=10)).decode())" "$password"
    return
  fi
  if command -v htpasswd >/dev/null 2>&1; then
    htpasswd -bnBC 10 "" "$password" | tr -d ':\n'
    return
  fi
  if command -v docker >/dev/null 2>&1; then
    docker run --rm "$PYTHON_IMAGE" sh -c \
      'pip install -q bcrypt >/dev/null 2>&1 && python -c "import bcrypt,sys;print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt(rounds=10)).decode())" "$1"' _ "$password"
    return
  fi
  die "生成 bcrypt 需要 python3(bcrypt) / htpasswd / docker 三者之一"
}

# ---------------------------------------------------------------------------
# mysql 客户端：优先本地，没有就用 docker
# ---------------------------------------------------------------------------
mysql_exec() {
  local sql="$1"
  if command -v mysql >/dev/null 2>&1; then
    MYSQL_PWD="$ROOT_PASS" mysql -h "$HOST" -P "$PORT" -u "$ROOT_USER" --default-character-set=utf8mb4 -e "$sql"
  else
    docker run --rm -i -e MYSQL_PWD="$ROOT_PASS" "$MYSQL_CLIENT_IMAGE" \
      mysql -h "$HOST" -P "$PORT" -u "$ROOT_USER" --default-character-set=utf8mb4 -e "$sql"
  fi
}

mysql_exec_file() {
  local file="$1"
  if command -v mysql >/dev/null 2>&1; then
    MYSQL_PWD="$ROOT_PASS" mysql -h "$HOST" -P "$PORT" -u "$ROOT_USER" --default-character-set=utf8mb4 < "$file"
  else
    docker run --rm -i -e MYSQL_PWD="$ROOT_PASS" "$MYSQL_CLIENT_IMAGE" \
      mysql -h "$HOST" -P "$PORT" -u "$ROOT_USER" --default-character-set=utf8mb4 < "$file"
  fi
}

info "生成 bcrypt hash ..."
HASH="$(bcrypt_hash "$ADMIN_PASS")"
[[ -n "$HASH" ]] || die "bcrypt 生成失败"

ADMIN_SQL="USE \`$DB\`;
INSERT INTO \`sys_users\` (\`uuid\`,\`username\`,\`password\`,\`nick_name\`,\`authority_id\`,\`enable\`)
VALUES (UUID(), '$ADMIN_USER', '$HASH', '管理员', 888, 1)
ON DUPLICATE KEY UPDATE \`password\`=VALUES(\`password\`), \`nick_name\`=VALUES(\`nick_name\`), \`enable\`=1;"

if [[ "$SQL_ONLY" -eq 1 ]]; then
  echo "-- 建库建表 SQL 见 $SCRIPT_DIR/01-schema.sql"
  echo "$ADMIN_SQL"
  exit 0
fi

if [[ "$SKIP_SCHEMA" -eq 0 ]]; then
  info "执行 $SCRIPT_DIR/01-schema.sql ..."
  # 文件里写死了 CREATE DATABASE k20mon，这里按 --db 覆盖
  sed "s/CREATE DATABASE IF NOT EXISTS \`k20mon\`/CREATE DATABASE IF NOT EXISTS \`$DB\`/; \
       s/^USE \`k20mon\`;/USE \`$DB\`;/" "$SCRIPT_DIR/01-schema.sql" > "$SCRIPT_DIR/.schema.tmp.sql"
  mysql_exec_file "$SCRIPT_DIR/.schema.tmp.sql"
  rm -f "$SCRIPT_DIR/.schema.tmp.sql"
fi

info "创建/更新管理员账号 '$ADMIN_USER' ..."
mysql_exec "$ADMIN_SQL"

info "校验..."
mysql_exec "SELECT id, username, enable, (LENGTH(password) > 50) AS is_bcrypt FROM \`$DB\`.sys_users WHERE username='$ADMIN_USER';"

echo
echo "----------------------------------------------------------------------"
echo " 完成"
echo "   数据库   : $DB @ $HOST:$PORT"
echo "   登录账号 : $ADMIN_USER"
if [[ "${GENERATED_PASS:-0}" -eq 1 ]]; then
  echo "   登录密码 : $ADMIN_PASS      <-- 自动生成，请立刻保存"
else
  echo "   登录密码 : (你指定的)"
fi
echo "   浏览器   : http://<设备IP>:8899"
echo "----------------------------------------------------------------------"
