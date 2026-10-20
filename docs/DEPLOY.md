# 部署指南

## 前置条件

| 项 | 要求 | 检查命令 |
|---|---|---|
| 系统 | Linux（arm64 / amd64 都验证过） | `uname -m` |
| Docker | 20.10+ | `docker version` |
| Docker Compose | v2（`docker compose`，不是 `docker-compose`） | `docker compose version` |
| MySQL | 5.7+ / 8.x | `mysql --version` |
| Python | 3.10+（只有代理需要） | `python3 --version` |
| 内存 | ≥ 512 MB 空闲 | `free -m` |
| 磁盘 | ≥ 2 GB（镜像 + 数据） | `df -h /` |

本项目的参考环境：Redmi K20 Pro（SM8150 / aarch64），Android 上跑 Ubuntu 26.04 chroot，8 核 / 8 GB 内存。

## 目录约定

把整个仓库放到 `/opt/k20mon`：

```bash
sudo mkdir -p /opt/k20mon
sudo chown "$USER:$USER" /opt/k20mon
git clone <你的仓库地址> /opt/k20mon
cd /opt/k20mon
```

放 `/opt` 的理由：代理脚本要装到 `/usr/local/libexec`、配置要装到 `/etc/k20mon`，都属于系统级路径，源码放在 `/opt` 便于对应；而且 `/opt` 不在 `ProtectHome` 的保护范围内。

## 部署步骤

### 1. 准备 MySQL

```bash
mysql -uroot -p <<'SQL'
CREATE DATABASE IF NOT EXISTS k20mon DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_general_ci;
CREATE USER IF NOT EXISTS 'k20mon'@'%' IDENTIFIED BY '换成你的强密码';
GRANT ALL PRIVILEGES ON k20mon.* TO 'k20mon'@'%';
FLUSH PRIVILEGES;
SQL
```

建用户表（后端复用 GVA 的 `sys_users` 结构，字段必须和 `server/main.go` 的 `userRecord` 对应）：

```bash
mysql -uroot -p k20mon <<'SQL'
CREATE TABLE IF NOT EXISTS sys_users (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  uuid          VARCHAR(191) NOT NULL DEFAULT '',
  username      VARCHAR(191) NOT NULL UNIQUE,
  password      VARCHAR(255) NOT NULL,
  nick_name     VARCHAR(191) NOT NULL DEFAULT '',
  header_img    VARCHAR(255) NOT NULL DEFAULT '',
  authority_id  BIGINT UNSIGNED NOT NULL DEFAULT 888,
  phone         VARCHAR(32)  NOT NULL DEFAULT '',
  email         VARCHAR(191) NOT NULL DEFAULT '',
  enable        INT NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
SQL
```

生成 bcrypt 密码并插入管理员：

```bash
# 方式一：有 python3-bcrypt
python3 -c "import bcrypt; print(bcrypt.hashpw(b'你的登录密码', bcrypt.gensalt(rounds=10)).decode())"

# 方式二：有 htpasswd（apache2-utils）
htpasswd -bnBC 10 "" '你的登录密码' | tr -d ':\n'; echo

# 方式三：用容器，不污染宿主机
docker run --rm python:3.12-alpine sh -c \
  "pip install -q bcrypt && python -c \"import bcrypt;print(bcrypt.hashpw(b'你的登录密码', bcrypt.gensalt()).decode())\""
```

```bash
mysql -uroot -p k20mon -e "
INSERT INTO sys_users (uuid, username, password, nick_name, authority_id, enable)
VALUES (UUID(), 'admin', '<上一步输出的 hash>', '管理员', 888, 1);"
```

> hash 里有 `$` 符号，在 shell 里**必须用单引号**包起来，否则会被当变量展开。

### 2. 写配置

```bash
cd /opt/k20mon/deploy
cp .env.example .env
cp config.yaml.example config.yaml
chmod 600 config.yaml      # 里面有数据库密码
```

编辑 `config.yaml`：

```yaml
mysql:
  path: 172.19.0.1        # ← 关键，见下方「MySQL 地址怎么填」
  port: "3306"
  config: charset=utf8mb4&parseTime=True&loc=Local
  db-name: k20mon
  username: k20mon
  password: 换成你的强密码

jwt:
  signing-key: 换成一串至少32位的随机字符串
  expires-time: 7d
  issuer: k20mon

system:
  addr: 8888
```

生成随机 JWT 密钥：

```bash
head -c 48 /dev/urandom | base64 | tr -d '\n=+/' | head -c 48; echo
```

编辑 `.env`（一般只改端口和路径）：

```ini
K20MON_WEB_PORT=8899
K20MON_SERVER_PORT=18888
K20MON_CONFIG_PATH=./config.yaml
K20MON_CONTROL_DIR=/run/k20mon-status
K20M_INTERVAL=5
K20M_RETENTION_H=720
TZ=Asia/Shanghai
K20MON_IMAGE_TAG=latest
```

### 3. 安装 root 控制代理

```bash
cd /opt/k20mon/deploy

sudo install -d -m 0755 /usr/local/libexec
sudo install -d -m 0755 /etc/k20mon
sudo install -d -m 0755 /run/k20mon-status

sudo install -m 0755 agent/k20mon_agent.py     /usr/local/libexec/
sudo install -m 0644 agent/rate_sync.py        /usr/local/libexec/
sudo install -m 0644 agent/k20mon-agent.service /etc/systemd/system/
sudo install -m 0644 services.json.example     /etc/k20mon/services.json

sudo systemctl daemon-reload
sudo systemctl enable --now k20mon-agent
```

验证：

```bash
systemctl status k20mon-agent --no-pager
ls -l /run/k20mon-status/control.sock
# 期望：srw-rw---- 1 root root ... control.sock
journalctl -u k20mon-agent -n 20 --no-pager
# 期望看到：k20mon agent listening on /run/k20mon-status/control.sock
```

直接测一个动作：

```bash
python3 - <<'PY'
import json, socket
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(35)
s.connect('/run/k20mon-status/control.sock')
s.sendall(b'{"action":"wifi_status","data":{}}\n')
print(s.recv(65536).decode())
PY
```

### 4. 起服务

```bash
cd /opt/k20mon/deploy
docker compose up -d --build
```

首次构建要拉 `golang:1.24-alpine`、`node:22-alpine`、`alpine:3.20`、`nginx:alpine` 四个镜像，5-15 分钟。国内网络建议先设置：

```bash
export K20MON_GOPROXY=https://goproxy.cn,direct
# 或者写进 .env
```

### 5. 验证

```bash
# 后端健康
curl -s http://127.0.0.1:18888/health
# {"status":"ok","service":"k20mon"}

# 前端
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8899/
# 200

# 经 nginx 的 API
curl -s http://127.0.0.1:8899/api/health
# {"status":"ok","service":"k20mon"}

# 鉴权拦截生效
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8899/api/k20mon/metrics/current
# 401

# 登录
curl -s -X POST http://127.0.0.1:8899/api/base/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"你的密码"}'
# {"code":0,...,"msg":"登录成功"}
```

浏览器打开 `http://<设备IP>:8899`，用刚才的账号登录。

## 环境变量总表

| 变量 | 默认值 | 作用 | 在哪读 |
|---|---|---|---|
| `K20MON_WEB_PORT` | `8899` | 前端对外端口 | compose |
| `K20MON_SERVER_PORT` | `18888` | 后端对外端口 | compose |
| `K20MON_CONFIG_PATH` | `./config.yaml` | 后端配置文件路径（宿主机） | compose |
| `K20MON_CONTROL_DIR` | `/run/k20mon-status` | 代理 socket 目录（宿主机） | compose |
| `K20MON_IMAGE_TAG` | `latest` | 镜像 tag | compose |
| `K20MON_GOPROXY` | `https://proxy.golang.org,direct` | 构建时 Go 代理 | compose build args |
| `K20MON_SERVER_RUNTIME` | `alpine:3.20` | 后端运行时基础镜像 | compose build args |
| `K20MON_WEB_RUNTIME` | `nginx:alpine` | 前端运行时基础镜像 | compose build args |
| `K20M_INTERVAL` | `5` | 采样间隔（秒，可小数） | 后端 |
| `K20M_RETENTION_H` | `720` | 历史保留小时数 | 后端 |
| `K20M_CONTROL_SOCKET` | `/run/k20mon-status/control.sock` | 代理 socket 路径（容器内） | 后端 + 代理 |
| `K20MON_ADDR` | `:8888` | 后端监听地址 | 后端 |
| `K20MON_MYSQL_DSN` | — | 直接给完整 DSN，覆盖配置文件 | 后端 |
| `K20MON_JWT_KEY` | — | JWT 签名密钥，覆盖配置文件 | 后端 |
| `K20MON_JWT_ISSUER` | `k20mon` | JWT issuer | 后端 |
| `K20MON_JWT_EXPIRES` | `168h` | token 有效期（Go duration 格式） | 后端 |
| `K20MON_CONTROL_SOCKET_MODE` | `0o660` | socket 权限 | 代理 |
| `K20MON_SERVICES_FILE` | `/etc/k20mon/services.json` | 服务目录缓存 | 代理 |
| `K20MON_FRP_CONTAINER` | `natfrp-frpc` | frpc 容器名 | 代理 |
| `K20MON_CHARGE_GUARD_CONFIG` | `/etc/k20-charge-guard.conf` | 充电守护配置 | 代理 |
| `K20MON_CHARGE_GUARD_STATE` | `/run/k20-charge-guard.state` | 充电守护状态 | 代理 |
| `K20MON_CHARGE_CURRENT_MAX` | `/sys/class/power_supply/pm8150b-charger/current_max` | 限流节点 | 代理 |
| `K20MON_CHARGE_CAPACITY` | `/sys/class/power_supply/qcom-battery/capacity` | 电量节点 | 代理 |
| `K20MON_WIFI_DEVICE` | 自动探测 | 指定 Wi-Fi 网卡 | 代理 |
| `K20MON_NMCLI` | `nmcli` | nmcli 路径 | 代理 |
| `K20MON_CLOUDFLARED_CONFIG` | `/etc/cloudflared/config.yml` | CF Tunnel 配置 | 代理 |
| `K20MON_CLOUDFLARED_TOKEN_FILE` | `/etc/cloudflared/token` | CF Tunnel token | 代理 |

## 端口与网络

| 位置 | 端口 | 说明 |
|---|---|---|
| 宿主机 | 8899 | 前端，对外唯一入口 |
| 宿主机 | 18888 | 后端，**建议只在局域网开放或干脆不暴露** |
| web 容器 | 8080 | nginx |
| server 容器 | 8888 | Go |

nginx 反代规则（`web/nginx.conf`）：

- `/` → 静态文件，`try_files` 回落到 `index.html`，`Cache-Control: no-store`
- `/assets/` → 长缓存 30 天（文件名带 hash）
- `/api/*` → `http://server:8888/*`（去掉 `/api` 前缀），15 秒超时
- `/api/k20mon/metrics/stream` → 同上但超时 1 小时、`proxy_buffering off`

### MySQL 地址怎么填（最容易踩的坑）

容器里的 `127.0.0.1` 是**容器自己**，不是宿主机。三个正确写法：

**写法 A：用 docker 网关 IP（推荐，最简单）**

```bash
docker network inspect deploy_default --format '{{(index .IPAM.Config 0).Gateway}}'
# 输出类似 172.19.0.1
```

config.yaml 里 `mysql.path` 填这个 IP。

**写法 B：`host.docker.internal`**

compose 的 server 服务加一行：

```yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

config.yaml 里 `mysql.path` 填 `host.docker.internal`。

**写法 C：host 网络**

给 server 加 `network_mode: host`，然后 `mysql.path` 填 `127.0.0.1`。但这样 nginx 就找不到 `server` 这个主机名了，要改成 `127.0.0.1:8888`，不推荐。

> 判断填对没：`docker logs k20mon-server-1` 如果一直刷 `K20Mon database unavailable ... dial tcp x.x.x.x:3306: connect: connection timed out`，就是地址错了。

## 故障排查

### 后端崩溃循环（restart 计数一直涨）

**症状**：`docker ps` 里 server 显示 `Restarting`，`docker inspect ... {{.RestartCount}}` 是几百。

**原因**（旧版本）：MySQL 连不上就 `panic`，容器重启，无限循环。

**排查**：

```bash
docker logs --tail 30 k20mon-server-1
```

- `panic: connect mysql: dial tcp x.x.x.x:3306: connect: connection timed out` → 地址不通，见上面「MySQL 地址怎么填」
- `panic: connect mysql: ... Access denied for user` → 账号密码错
- `panic: K20MON_JWT_KEY or jwt.signing-key is required` → `config.yaml` 里 `jwt.signing-key` 空着，或文件读不到（权限问题）

**验证地址通不通**：

```bash
# 从容器网络里测
docker run --rm --network deploy_default alpine:3.20 sh -c \
  'apk add -q netcat-openbsd && nc -zv -w3 172.19.0.1 3306'
```

> 新版后端已经不会因为这个 panic 了，它会一直重试并在日志里打印 `K20Mon database unavailable (attempt N)`。看到这个说明只是地址/密码问题，不是崩溃。

### 容器 unhealthy

**症状**：`docker ps` 显示 `(unhealthy)`，web 起不来（`depends_on: service_healthy` 挡住了）。

**排查**：

```bash
docker inspect k20mon-server-1 --format '{{json .State.Health}}' | head -c 1000
```

看 `Log[].ExitCode`：

- `4` → wget 网络错误，服务没在监听
- `8` → 服务器返回错误码

**手动确认端口**：

```bash
docker exec k20mon-server-1 sh -c 'wget -qO- http://127.0.0.1:8888/health'
```

没输出就是进程没起来，看 `docker logs`。

### 前端 502 / 空白页

**502** → nginx 连不上 `server:8888`。检查：

```bash
docker exec k20mon-web-1 sh -c 'wget -qO- http://server:8888/health'
```

连不上就是 server 没起或没在同一个网络里。

**空白页** → 打开浏览器控制台看报错：

- `Failed to load module` → 静态文件没打进镜像，检查 `web/Dockerfile` 里 `COPY --from=builder /app/dist/`
- `404 /api/...` → nginx 的 `/api` 反代规则被改了

### 登录报「数据库暂不可用」

MySQL 还没连上。等 10 秒重试，或：

```bash
docker logs k20mon-server-1 | grep -i database | tail -5
```

### 操作中心报「root control agent is unavailable」

代理没跑或路径不对：

```bash
systemctl status k20mon-agent --no-pager
ls -l /run/k20mon-status/control.sock
```

socket 存在但还是连不上 → 权限问题。容器里的后端以 root 运行，socket 是 `root:root 0660` 应该能连。如果后端改成了非 root，把 `K20MON_CONTROL_SOCKET_MODE` 设成 `0666`：

```bash
sudo systemctl edit k20mon-agent
# 加：
# [Service]
# Environment=K20MON_CONTROL_SOCKET_MODE=0666
sudo systemctl restart k20mon-agent
```

### 端口被占用

```bash
ss -ltnp | grep -E ':(8899|18888)\b'
```

换端口：改 `.env` 里的 `K20MON_WEB_PORT` / `K20MON_SERVER_PORT` 再 `docker compose up -d`。

### 镜像拉不下来

**GOPROXY 超时**：

```bash
export K20MON_GOPROXY=https://goproxy.cn,direct
docker compose build --no-cache server
```

**`alpine:3.21` 被镜像站拒绝（403）**：本项目默认用 `alpine:3.20`。要换：

```bash
export K20MON_SERVER_RUNTIME=alpine:3.20
# 或任何本地已有的 alpine 版本
docker images | grep alpine
```

**`nginx:alpine` 拉不下来**：复用已有的 nginx 镜像：

```bash
docker images | grep -i nginx
export K20MON_WEB_RUNTIME=<你本地的 nginx 镜像>:<tag>
```

**`node:22-alpine` 拉不下来**：把前端在本机构建好，然后只构建后端：

```bash
cd web && npm ci && npm run build && cd ..
# 改 web/Dockerfile，去掉 builder 阶段，直接 COPY dist
```

### 前端能开但没数据

`/api/k20mon/metrics/current` 返回 200 但字段全 null → 采集路径不对。检查：

```bash
docker exec k20mon-server-1 ls /sys/class/power_supply/
```

应该看到 `qcom-battery` 和 `pm8150b-charger`。没有的话说明机型不同，要改 `server/collector/collector.go` 里的 `batteryPath` / `chargerPath`。

### 历史数据不增长

```bash
docker logs k20mon-server-1 | grep -i "history persistence"
```

出现 `history persistence unavailable` 就是落库失败，查数据库连接和 `k20mon_metric_samples` 表是否存在（首次启动会自动建）。

## 升级与回滚

### 只重建后端（前端不动）

```bash
cd /opt/k20mon/deploy
docker compose up -d --build --no-deps server
```

### 只重建前端

```bash
docker compose up -d --build --no-deps web
```

### 用 tag 做版本管理

```bash
# 发布
export K20MON_IMAGE_TAG=$(date +%Y%m%d%H%M%S)
docker compose build
docker compose up -d

# 回滚到上一个 tag
export K20MON_IMAGE_TAG=20261016021540
docker compose up -d --no-build
```

`docker images | grep k20mon` 能看到所有历史 tag。

### 回滚代理

```bash
sudo cp /usr/local/libexec/k20mon_agent.py /usr/local/libexec/k20mon_agent.py.bak
# 换回旧版本
sudo systemctl restart k20mon-agent
```

## 安全建议

1. **JWT 密钥必须长随机**：至少 32 位。密钥泄漏等于任何人都能伪造管理员 token（后端没有额外的会话校验）。
2. **`config.yaml` 权限 0600**：里面有 MySQL 密码。
3. **18888 不要暴露公网**：前端已经通过 nginx 反代了，18888 只在局域网或干脆只绑 `127.0.0.1`：

   ```yaml
   ports:
     - "127.0.0.1:18888:8888"
   ```

4. **8899 也别直接暴露公网**：用 Cloudflare Tunnel 或 frp 加一层，本项目自带 CF Tunnel 状态检测，说明本来就是这么用的。
5. **socket 权限保持 0660**：改成 0666 意味着容器里任何进程都能调特权动作。如果必须改，至少确保 `/run/k20mon-status` 目录只有可信用户能进。
6. **代理只监听 Unix socket**：不要为了图方便给它加 TCP 监听，那等于把 root 权限挂到网上。
7. **`sys_users` 密码用 bcrypt**：不要用明文或 MD5。GVA 老数据如果是 MD5，要重新生成。
8. **定期清理历史**：`K20M_RETENTION_H` 设小一点（比如 168 = 7 天），避免手机存储被写满。

## 卸载

```bash
cd /opt/k20mon/deploy
docker compose down --rmi local --volumes

sudo systemctl disable --now k20mon-agent
sudo rm -f /etc/systemd/system/k20mon-agent.service /usr/local/libexec/k20mon_agent.py /usr/local/libexec/rate_sync.py
sudo systemctl daemon-reload

sudo rm -rf /etc/k20mon /run/k20mon-status
mysql -uroot -p -e "DROP DATABASE k20mon;"
```
