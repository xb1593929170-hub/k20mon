# 部署指南

## 前置条件

| 项 | 要求 | 检查命令 |
|---|---|---|
| 系统 | Linux（arm64 / amd64 都验证过） | `uname -m` |
| Docker | 20.10+ | `docker version` |
| Docker Compose | v2（`docker compose`，不是 `docker-compose`） | `docker compose version` |
| MySQL | 5.7+ / 8.x | `mysql --version` |
| Python | 3.10+（只有 root 代理需要） | `python3 --version` |
| 内存 | ≥ 512 MB 空闲 | `free -m` |
| 磁盘 | ≥ 2 GB（镜像 + 数据） | `df -h /` |

参考环境：Redmi K20 Pro（SM8150 / aarch64），Android 上跑 Ubuntu 26.04 chroot，8 核 / 8 GB 内存。

## 目录约定

```bash
sudo mkdir -p /opt/k20mon
sudo chown "$USER:$USER" /opt/k20mon
git clone <你的仓库地址> /opt/k20mon
cd /opt/k20mon
chmod +x scripts/*.sh deploy/sql/*.sh    # Windows 下 clone 会丢执行位
```

放 `/opt` 的理由：代理脚本要装到 `/usr/local/libexec`、配置要装到 `/etc/k20mon`，都属于系统级路径；而且 `/opt` 不在 systemd `ProtectHome` 的保护范围内。

如果你是**上传压缩包**而不是 `git clone`：

```bash
./scripts/package-release.sh --version 1.0.0
scp dist/k20mon-1.0.0.tar.gz root@<设备>:/tmp/
ssh root@<设备> 'tar -xzf /tmp/k20mon-1.0.0.tar.gz -C /opt && ls /opt/k20mon'
```

---

## 第 1 步：准备数据库

### 1.1 一键脚本（推荐）

`deploy/sql/create-admin.sh` 会把「建库 → 建表 → 建管理员」一次做完：

```bash
cd /opt/k20mon
./deploy/sql/create-admin.sh \
  --host 127.0.0.1 --port 3306 \
  --user root --password '你的 MySQL root 密码' \
  --db k20mon \
  --username admin
# 不指定 --app-pass 时会自动生成一个随机密码并打印出来
```

输出示例：

```text
==> 生成 bcrypt hash ...
==> 执行 .../01-schema.sql ...
==> 创建/更新管理员账号 'admin' ...
==> 校验...
id  username  enable  is_bcrypt
3   admin     1       1

----------------------------------------------------------------------
 完成
   数据库   : k20mon @ 127.0.0.1:3306
   登录账号 : admin
   登录密码 : Xk9mQ2vL7pR4tZ8w      <-- 自动生成，请立刻保存
   浏览器   : http://<设备IP>:8899
----------------------------------------------------------------------
```

脚本会自动解决依赖：

- 有 `python3` + `bcrypt` 就用它算 hash；没有就试 `htpasswd`；再没有就用 docker 跑 python 镜像
- 有 `mysql` 客户端就本地执行；没有就用 docker 跑 mysql 镜像
- 用 docker 跑客户端时，如果 `--host` 是 `127.0.0.1`，脚本会自动换成 `host.docker.internal`
  并加 `--add-host=host.docker.internal:host-gateway`（容器里的 127.0.0.1 是容器自己，连不到宿主机）
- 优先使用**本地已有**的镜像，避免在拉不到 Docker Hub 的机器上卡住
  （要指定就设 `MYSQL_CLIENT_IMAGE` / `PYTHON_IMAGE`）

常用变体：

```bash
# 自己指定应用账号的密码
./deploy/sql/create-admin.sh --password 'root密码' --app-pass '你要的登录密码'

# 只想看 SQL 不想执行（交给 DBA 审）
./deploy/sql/create-admin.sh --password 'root密码' --sql-only

# 库和表已经建好了，只加/改一个管理员
./deploy/sql/create-admin.sh --password 'root密码' --skip-schema --username ops
```

### 1.2 手工执行 SQL

仓库里有三个 SQL 相关文件：

| 文件 | 内容 |
|---|---|
| `deploy/sql/01-schema.sql` | 建库 `k20mon` + 三张表（`sys_users` / `k20mon_metric_samples` / `k20mon_service_catalog`） |
| `deploy/sql/02-admin-user.sql` | 建管理员的 SQL 模板（需要手工填 bcrypt hash） |
| `deploy/sql/create-admin.sh` | 上面两步的自动化版本 |

```bash
mysql -h 127.0.0.1 -P 3306 -u root -p < deploy/sql/01-schema.sql
```

> `k20mon_metric_samples` 和 `k20mon_service_catalog` 其实由后端 `AutoMigrate` 自动创建，
> 只跑建库 + `sys_users` 也能启动。提供完整 DDL 是为了让 DBA 能先审、或者让后端账号不需要
> DDL 权限（那样就要先手工建表）。

单独建应用账号（如果不想用 root 跑应用）：

```bash
mysql -u root -p <<'SQL'
CREATE USER IF NOT EXISTS 'k20mon'@'%' IDENTIFIED BY '换成强密码';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX ON k20mon.* TO 'k20mon'@'%';
FLUSH PRIVILEGES;
SQL
```

> 保留 `CREATE` / `ALTER` / `INDEX` 是因为 `AutoMigrate` 要在首次启动时建表加索引。
> 如果你已经用 `01-schema.sql` 手工建好表，可以收回这三个权限。

### 1.3 手工验证

```bash
mysql -h 127.0.0.1 -u root -p -e "
SHOW TABLES FROM k20mon;
SELECT id, username, enable, LENGTH(password) AS hash_len FROM k20mon.sys_users;"
```

`hash_len` 应该是 60（bcrypt 固定长度）。

---

## 第 2 步：写配置

```bash
cd /opt/k20mon/deploy
cp .env.example .env
cp config.yaml.example config.yaml
chmod 600 config.yaml      # 里面有数据库密码
```

生成随机 JWT 密钥：

```bash
head -c 48 /dev/urandom | base64 | tr -d '\n=+/' | head -c 48; echo
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
  signing-key: 上面生成的那串 48 位随机字符
  expires-time: 7d
  issuer: k20mon

system:
  addr: 8888
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

---

## 第 3 步：构建镜像

两种方式，按你有没有外网 / 是不是跨架构选。

### 3.1 在设备上直接构建（最常用）

```bash
cd /opt/k20mon
./scripts/build-images.sh --tag 1.0.0 --cn
```

`--cn` 会把 Go 模块代理换成 `goproxy.cn`，国内网络必需。脚本会打印构建参数、构建两个镜像、最后列出结果：

```text
==> 构建参数
    tag            : 1.0.0
    registry       : <本地>
    GOPROXY        : https://goproxy.cn,direct
    后端运行时镜像 : alpine:3.20
    前端运行时镜像 : nginx:alpine
    输出镜像       : k20mon/server:1.0.0 / k20mon/web:1.0.0
==> docker compose build ...
==> 构建完成
k20mon/server:1.0.0  19.4MB  (5 seconds ago)
k20mon/web:1.0.0     52.1MB  (3 seconds ago)
```

等价的裸命令：

```bash
cd deploy
K20MON_IMAGE_TAG=1.0.0 K20MON_GOPROXY=https://goproxy.cn,direct docker compose build
```

### 3.2 在别处构建、导出离线包再传过去

手机上拉镜像往往很慢甚至被镜像站拒绝，推荐在开发机构建好后传过去：

```bash
# 开发机（arm64，和手机同架构）
./scripts/build-images.sh --tag 1.0.0 --cn --save
# 产出 dist/k20mon-images-1.0.0.tar.gz（约 70 MB）

scp dist/k20mon-images-1.0.0.tar.gz root@<设备>:/tmp/

# 设备上
gunzip -c /tmp/k20mon-images-1.0.0.tar.gz | docker load
cd /opt/k20mon/deploy
K20MON_IMAGE_TAG=1.0.0 docker compose up -d --no-build
```

> **跨架构**：开发机是 x86 而手机是 arm64 时不能直接用 `--save`（架构不匹配）。
> 用 `--multiarch` 构建多架构镜像并推到镜像仓库，设备上再 pull：
>
> ```bash
> ./scripts/build-images.sh --tag 1.0.0 --registry registry.example.com/ --multiarch --push
> ```

### 3.3 镜像里到底装了什么

| 镜像 | 构建阶段 | 运行阶段 | 大小 |
|---|---|---|---|
| `k20mon/server` | `golang:1.24-alpine` 编译静态二进制 | `alpine:3.20` + 二进制，约 20 MB | 20 MB |
| `k20mon/web` | `node:22-alpine` 跑 `npm run build` | `nginx:alpine` + `dist/`，约 52 MB | 52 MB |

两个镜像都支持用 build arg 换基础镜像（旧设备上某些 tag 可能拉不到）：

```bash
export K20MON_SERVER_RUNTIME=alpine:3.20
export K20MON_WEB_RUNTIME=nginx:alpine
```

---

## 第 4 步：安装 root 控制代理

代理必须在**宿主机**上以 root 跑，不能进容器——它要写 sysfs、跑 nmcli、重启 frp 容器。

```bash
cd /opt/k20mon
make agent-install
```

等价于手工执行：

```bash
sudo install -d -m 0755 /usr/local/libexec /etc/k20mon /run/k20mon-status

sudo install -m 0755 deploy/agent/k20mon_agent.py        /usr/local/libexec/
sudo install -m 0644 deploy/agent/rate_sync.py           /usr/local/libexec/
sudo install -m 0644 deploy/agent/k20mon-agent.service   /etc/systemd/system/
sudo install -m 0644 deploy/services.json.example        /etc/k20mon/services.json

sudo systemctl daemon-reload
sudo systemctl enable --now k20mon-agent
```

验证：

```bash
systemctl is-active k20mon-agent
# active

ls -l /run/k20mon-status/control.sock
# srw-rw---- 1 root root 0 ... /run/k20mon-status/control.sock

journalctl -u k20mon-agent -n 20 --no-pager
# k20mon agent listening on /run/k20mon-status/control.sock
```

直接测一个动作（不经过后端）：

```bash
python3 - <<'PY'
import socket
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(35)
s.connect('/run/k20mon-status/control.sock')
s.sendall(b'{"action":"wifi_status","data":{}}\n')
print(s.recv(65536).decode())
PY
```

期望输出类似：

```json
{"ok": true, "wifi": {"available": true, "interface": "wld0", "connected": true, "ssid": "MyWiFi", "ip": "192.168.1.2"}}
```

### 禁用动态定价模块（可选）

`rate_sync.py` 控制的是一个外部脚本，不属于本项目。不想用可以删掉：

```bash
sudo rm -f /usr/local/libexec/rate_sync.py
# 同时删掉 k20mon_agent.py 顶部的三行 import 和 control_action 里的三个分支
```

其它功能不受影响。

---

## 第 5 步：启动

```bash
cd /opt/k20mon/deploy
docker compose up -d

# 或者用 make
cd /opt/k20mon && make up
```

首次 `up` 如果不加 `--build`，需要镜像已经构建好（第 3 步做过）。

观察启动：

```bash
docker compose ps
# k20mon-server-1  Up ... (healthy)
# k20mon-web-1     Up ...

docker compose logs -f --tail=50 server
```

后端日志正常长这样：

```text
K20Mon backend listening on :8888
K20Mon database connected
```

如果看到：

```text
K20Mon database unavailable (attempt 1): dial tcp 172.19.0.1:3306: connect: connection refused
```

说明数据库地址不对——后端**不会退出**，会每 10 秒重试，但所有需要数据库的功能（登录、历史、服务宫格）都不可用。

---

## 第 6 步：验证

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

# 登录（用第 1 步创建的账号）
curl -s -X POST http://127.0.0.1:8899/api/base/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"你的密码"}'
# {"code":0,"data":{"user":{...},"token":"eyJ...","expiresAt":...},"msg":"登录成功"}
```

拿到 token 后测实时数据：

```bash
TOKEN='<上面返回的 token>'
curl -s http://127.0.0.1:8899/api/k20mon/metrics/current -H "x-token: $TOKEN" | head -c 400
```

浏览器打开 `http://<设备IP>:8899`，用同一套账号登录。

---

## 完整流程速查（从零到跑起来）

```bash
# 0. 拿代码
sudo mkdir -p /opt/k20mon && sudo chown "$USER:$USER" /opt/k20mon
git clone <仓库> /opt/k20mon && cd /opt/k20mon && chmod +x scripts/*.sh deploy/sql/*.sh

# 1. 数据库
./deploy/sql/create-admin.sh --password 'MySQL密码' --db k20mon --username admin

# 2. 配置
cd deploy && cp .env.example .env && cp config.yaml.example config.yaml
chmod 600 config.yaml
# 编辑 config.yaml：mysql.path/username/password、jwt.signing-key

# 3. 镜像
cd .. && ./scripts/build-images.sh --tag 1.0.0 --cn

# 4. 代理
make agent-install

# 5. 启动
cd deploy && K20MON_IMAGE_TAG=1.0.0 docker compose up -d

# 6. 验证
curl -s http://127.0.0.1:18888/health
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8899/
```

---

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
| `MYSQL_PWD` / `MYSQL_HOST` / `MYSQL_PORT` / `MYSQL_USER` | — | `create-admin.sh` 与 `make db-*` 用 | 脚本 |

---

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

**写法 A：docker 网关 IP（推荐）**

```bash
cd deploy
docker network inspect "$(basename "$PWD")_default" --format '{{(index .IPAM.Config 0).Gateway}}'
# 输出类似 172.19.0.1
```

> 项目名由 compose 自动推导：`<目录名>_default`。用 `-p` 指定过项目名的按项目名来。

config.yaml 里 `mysql.path` 填这个 IP。

**写法 B：`host.docker.internal`**

compose 的 server 服务加一行：

```yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

config.yaml 里 `mysql.path` 填 `host.docker.internal`。换网络/换机器都不会失效，推荐长期用这个。

**写法 C：不用容器跑 MySQL**

如果 MySQL 也是 compose 里的服务，`mysql.path` 直接填服务名（比如 `mysql`），走容器网络。

**不要写 `host` 网络模式**：那样 nginx 就找不到 `server` 这个主机名，要额外改成 `127.0.0.1:8888`，得不偿失。

> 判断填对没：`docker compose logs server | grep database`。一直刷
> `K20Mon database unavailable ... connection timed out` 就是地址不通；
> 出现一次 `K20Mon database connected` 就对了。

---

## 故障排查

### 后端一直重启（RestartCount 持续增长）

**症状**：`docker compose ps` 显示 `Restarting`，`docker inspect k20mon-server-1 --format '{{.RestartCount}}'` 是几百。

**排查**：

```bash
docker compose logs --tail 30 server
```

| 日志 | 原因 | 解决 |
|---|---|---|
| `panic: connect mysql: ... connection timed out` | 数据库地址不通 | 见上一节「MySQL 地址怎么填」 |
| `panic: connect mysql: ... Access denied` | 账号密码错 | 核对 `config.yaml` 与 `create-admin.sh` 的结果 |
| `panic: K20MON_JWT_KEY or jwt.signing-key is required` | `jwt.signing-key` 空着 | 填上随机密钥 |
| 日志里只有 `K20Mon database unavailable (attempt N)` 且**不重启** | 新版正常行为 | 只是地址/密码问题，容器本身是活的 |

验证地址通不通：

```bash
docker compose run --rm --entrypoint sh server -c \
  'wget -qO- http://172.19.0.1:3306 --timeout=3 2>&1 | head -2'
```

### 容器 unhealthy

```bash
docker inspect k20mon-server-1 --format '{{json .State.Health}}' | head -c 1000
```

看 `Log[].ExitCode`：`4` = wget 连不上（服务没监听），`8` = 服务器返回错误码。

```bash
docker compose exec server wget -qO- http://127.0.0.1:8888/health
```

### 前端 502 / 空白页

**502** 是 nginx 连不上 `server:8888`：

```bash
docker compose exec web wget -qO- http://server:8888/health
```

**空白页**看浏览器控制台：

- `Failed to load module` → 静态文件没打进镜像，检查 `web/Dockerfile` 里 `COPY --from=builder /app/dist/`
- `404 /api/...` → nginx 的 `/api` 反代规则被改了

### 登录报「数据库暂不可用」

```bash
docker compose logs server | grep -i database | tail -5
```

### 操作中心报「root control agent is unavailable」

```bash
systemctl status k20mon-agent --no-pager
ls -l /run/k20mon-status/control.sock
```

socket 存在但还是连不上 → 权限。后端容器默认以 root 运行，`root:root 0660` 的 socket 应该能连。如果后端改成了非 root：

```bash
sudo systemctl edit k20mon-agent
# [Service]
# Environment=K20MON_CONTROL_SOCKET_MODE=0666
sudo systemctl restart k20mon-agent
```

### 端口被占用

```bash
ss -ltnp | grep -E ':(8899|18888)\b'
```

改 `.env` 里的端口再 `docker compose up -d`。

### 镜像构建失败

| 报错 | 原因 | 解决 |
|---|---|---|
| `go mod download` 超时 | 拉不到 Go 模块 | `./scripts/build-images.sh --cn` |
| `alpine:3.21: 403 Forbidden` | 镜像站不放行这个 tag | `export K20MON_SERVER_RUNTIME=alpine:3.20` |
| `nginx:alpine` 拉不到 | 同上 | `export K20MON_WEB_RUNTIME=<本地已有的 nginx:tag>`，或 `docker images \| grep nginx` |
| `node:22-alpine` 拉不到 | 同上 | 本机构建前端后 `--with-web-dist` 打包上传 |
| `no space left on device` | 磁盘满 | `docker system prune -af` |
| 构建长时间无输出 | Docker Hub 不可达，`docker build` 在静默重试 | `build-images.sh` 有预检会提前提示；按提示换本地镜像或走离线包 |

> **关于 Docker Hub**：手机这类设备经常连不上 `registry-1.docker.io`（TLS 中断或被限流），
> 表现为 `docker pull` 长时间挂住而不是报错。`scripts/build-images.sh` 会先探一次并给出
> 三个替代方案。最稳的做法是**在能联网的机器上构建好，导出离线包传过去**（见
> [RELEASE.md §4](RELEASE.md#4-离线分发手机不能拉镜像时)）。

### 前端能开但没数据

说明采集路径不对：

```bash
docker compose exec server ls /sys/class/power_supply/
```

应该有 `qcom-battery` 和 `pm8150b-charger`。没有就是机型不同，改 `server/collector/collector.go` 里的 `batteryPath` / `chargerPath`。

### 历史数据不增长

```bash
docker compose logs server | grep -i "history persistence"
```

出现 `history persistence unavailable` 就是落库失败，检查数据库账号有没有 `INSERT` 权限、`k20mon_metric_samples` 表在不在。

### SQL 执行失败

| 报错 | 原因 | 解决 |
|---|---|---|
| `Access denied for user 'root'@'...'` | 密码错，或 root 不允许远程 | 本地 `mysql -uroot -p` 进；或加 `--host 127.0.0.1` |
| `Unknown database 'k20mon'` | 建库语句没执行 | 别加 `--skip-schema`，或手工跑 `01-schema.sql` |
| `Table 'sys_users' already exists` | 重复执行 | 幂等，忽略即可 |
| `Incorrect string value` | 客户端字符集不对 | 脚本已指定 `--default-character-set=utf8mb4` |
| `bcrypt 生成失败` | 三个依赖都没有 | 装 `python3-bcrypt`：`apt install python3-bcrypt` |

---

## 升级与回滚

### 升级后端（前端不动）

```bash
cd /opt/k20mon && git pull
./scripts/build-images.sh --tag $(date +%Y%m%d%H%M%S) --cn
cd deploy && docker compose up -d --no-deps server
```

### 升级前端

```bash
cd /opt/k20mon/deploy
docker compose up -d --build --no-deps web
```

### 用 tag 做版本管理

```bash
# 发布
./scripts/build-images.sh --tag 1.1.0 --cn
cd deploy && K20MON_IMAGE_TAG=1.1.0 docker compose up -d

# 回滚（镜像还在本地）
K20MON_IMAGE_TAG=1.0.0 docker compose up -d --no-build
```

`docker images | grep k20mon` 能看到所有历史 tag。回滚代理：

```bash
sudo cp /usr/local/libexec/k20mon_agent.py /usr/local/libexec/k20mon_agent.py.bak-$(date +%s)
# 换回旧版
sudo systemctl restart k20mon-agent
```

### 数据库回滚

表结构变更由 `AutoMigrate` 处理，只加不删。真要回滚结构：

```bash
mysqldump -h 127.0.0.1 -u root -p k20mon > k20mon-backup-$(date +%Y%m%d).sql
```

---

## 安全建议

1. **JWT 密钥必须长随机**：至少 32 位。密钥泄漏等于任何人都能伪造管理员 token（后端没有额外的会话校验）。
2. **`config.yaml` 权限 0600**：里面有 MySQL 密码。
3. **18888 不要暴露公网**：只绑本地或局域网。
   ```yaml
   ports:
     - "127.0.0.1:18888:8888"
   ```
4. **8899 也别直接暴露**：用 Cloudflare Tunnel 或 frp 加一层。
5. **socket 权限保持 0660**：改成 0666 意味着容器里任何进程都能调特权动作。
6. **代理只监听 Unix socket**：不要给它加 TCP 监听，那等于把 root 权限挂到网上。
7. **应用数据库账号最小权限**：如果不想让它有 DDL 权限，就先用 `01-schema.sql` 手工建表，然后只给 `SELECT, INSERT, UPDATE, DELETE`。
8. **定期清理历史**：`K20M_RETENTION_H` 设小一点（比如 168 = 7 天）。
9. **`sys_users` 密码用 bcrypt**：不要用明文或 MD5。

---

## 卸载

```bash
cd /opt/k20mon/deploy
docker compose down --rmi local --volumes

sudo systemctl disable --now k20mon-agent
sudo rm -f /etc/systemd/system/k20mon-agent.service \
           /usr/local/libexec/k20mon_agent.py /usr/local/libexec/rate_sync.py
sudo systemctl daemon-reload

sudo rm -rf /etc/k20mon /run/k20mon-status /opt/k20mon
mysql -u root -p -e "DROP DATABASE k20mon;"
```
