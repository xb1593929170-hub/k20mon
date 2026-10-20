# K20Mon

在 Android 手机上（Redmi K20 Pro / SM8150，Ubuntu 26.04 chroot）运行的**设备监控 + 运维控制台**。

一个 Go 后端 + 一个 Vue 3 前端 + 一个 Python root 控制代理，用 Docker Compose 部署。

```
浏览器 ──► nginx(前端, :8899) ──/api──► Go 后端(:8888) ──unix socket──► Python root 代理
                                              │
                                              ├── 只读挂载 /sys /proc  →  采集电池 / 充电器 / 网络 / CPU / GPU / 温度
                                              └── MySQL                →  历史采样 + 服务目录
```

## 功能

| 模块 | 说明 |
|---|---|
| 仪表盘 | 电池容量/电流/功率/温度/健康度/预计时间、充电器 PD 协商与限流、CPU 频率与温度、GPU 频率占用、内存与 swap、网络速率、历史统计（充入/放出容量、等效循环、容量估算） |
| 操作中心 | 充电守护（限充阈值 + 电流限制，直接写 sysfs）、Wi-Fi 扫描/连接/断开（nmcli）、Cloudflare Tunnel 状态、frp 隧道配置与重启、动态定价任务 |
| 服务宫格 | 服务目录 CRUD + 归属切换 + 拖拽排序 + 在线探测（并发 curl 探活） |

## 目录结构

```
k20mon/
├── server/                     Go 后端（Gin + GORM + MySQL）
│   ├── main.go                 配置加载 / 路由 / JWT / 数据库后台重连
│   ├── collector/              procfs + sysfs 采集器（root 可注入，方便单测）
│   ├── model/                  指标 / 控制协议 / 服务目录 数据结构
│   ├── service/                采样调度、历史查询、容量估算、控制客户端、服务目录
│   └── Dockerfile              多阶段构建 → 静态二进制
├── web/                        Vue 3 前端（Element Plus + Vite）
│   ├── src/main.js             入口：装 Element Plus，挂 axios 句柄
│   ├── src/App.vue             登录页 + 布局壳 + 三视图切换（不用 vue-router）
│   ├── src/api/                client.js（拦截器）/ auth.js（登录态）
│   ├── src/k20mon/             api / utils / views
│   ├── nginx.conf              静态托管 + /api 反代（含 SSE 长连接特例）
│   └── Dockerfile              node 构建 → nginx 运行
├── deploy/
│   ├── docker-compose.yml      server + web
│   ├── .env.example            端口 / 配置路径 / 镜像 tag
│   ├── config.yaml.example     MySQL / JWT / 端口
│   ├── services.json.example   服务宫格初始数据
│   └── agent/                  root 控制代理（纯标准库 Python）
│       ├── k20mon_agent.py     Unix socket 服务 + 动作白名单
│       ├── k20mon-agent.service
│       └── rate_sync.py        可选：外部定价任务控制
└── docs/
    ├── AI-RESTORE.md           交给 AI 完整还原本项目（可直接整段粘贴）
    ├── ARCHITECTURE.md         数据流、算法、协议细节
    ├── API.md                  全部 HTTP 接口契约
    ├── DEPLOY.md               从零部署 / 排错 / 升级回滚
    └── CLEANUP.md              相对原始代码删掉了什么、为什么
```

## 快速开始

```bash
# 1. 配置
cp deploy/.env.example deploy/.env
cp deploy/config.yaml.example deploy/config.yaml   # 填 MySQL 账号密码

# 2. 起服务（手机上或任意 aarch64/amd64 Linux）
cd deploy && docker compose up -d --build

# 3. 装 root 控制代理（可选，但操作中心依赖它）
sudo install -d -m 0755 /usr/local/libexec /etc/k20mon /run/k20mon-status
sudo install -m 0755 deploy/agent/k20mon_agent.py /usr/local/libexec/
sudo install -m 0644 deploy/agent/rate_sync.py    /usr/local/libexec/
sudo install -m 0644 deploy/agent/k20mon-agent.service /etc/systemd/system/
sudo install -m 0644 deploy/services.json.example /etc/k20mon/services.json
sudo systemctl daemon-reload && sudo systemctl enable --now k20mon-agent

# 4. 打开 http://<设备IP>:8899，用 MySQL sys_users 表里的账号登录
```

前端默认账号是 `admin`，密码取自 `sys_users.password`（bcrypt）。建库建表的 SQL 见
[docs/DEPLOY.md](docs/DEPLOY.md#1-准备-mysql)。

> 注意：`config.yaml` 里 MySQL 地址**不能填 `127.0.0.1`**（那是容器自己），要么填
> `docker network inspect <项目名>_default` 得到的网关 IP，要么用 `host.docker.internal`。
> 详见 [docs/DEPLOY.md](docs/DEPLOY.md#mysql-地址怎么填最容易踩的坑)。

## 开发与校验

```bash
make check      # gofmt + go vet + go test + npm test + python unittest
make build      # 本地构建后端二进制 + 前端产物
make up / down / logs
```

详细步骤、端口占用处理、回滚方式见 [docs/DEPLOY.md](docs/DEPLOY.md)。

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `K20M_INTERVAL` | `5` | 采样间隔（秒） |
| `K20M_RETENTION_H` | `720` | 历史保留小时数（30 天） |
| `K20M_CONTROL_SOCKET` | `/run/k20mon-status/control.sock` | root 代理 socket |
| `K20MON_ADDR` | `:8888` | 后端监听地址 |
| `K20MON_MYSQL_DSN` | — | 直接给 DSN 时覆盖配置文件 |
| `K20MON_JWT_KEY` | 取配置文件 | JWT 签名密钥 |
| `K20MON_CONFIG_PATH` | `/etc/k20mon/config.yaml` | 配置文件路径 |
| `K20MON_WEB_PORT` | `8899` | 前端映射端口 |
| `K20MON_SERVER_PORT` | `18888` | 后端映射端口 |

## 接口一览

所有业务接口都需要 `x-token` 请求头（登录返回的 JWT）。响应统一 `{code, data, msg}`，`code=0` 为成功。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/base/login` | 登录，返回 `{user, token, expiresAt}` |
| GET | `/health` | 健康检查（无鉴权） |
| GET | `/user/getUserInfo` | 当前用户 |
| GET | `/k20mon/metrics/current` | 当前快照 |
| GET | `/k20mon/metrics/history` | 历史采样（`from`/`to` RFC3339，`limit`） |
| GET | `/k20mon/metrics/summary` | 区间统计 |
| GET | `/k20mon/metrics/stream` | SSE 实时推送 |
| GET/PUT | `/k20mon/operations/charge-guard` | 充电守护读写 |
| GET | `/k20mon/operations/wifi/status` | Wi-Fi 状态 |
| POST | `/k20mon/operations/wifi/scan` | 扫描 |
| POST | `/k20mon/operations/wifi/connect` | 连接 |
| POST | `/k20mon/operations/wifi/disconnect` | 断开 |
| GET | `/k20mon/operations/cloudflare/status` | Cloudflare Tunnel |
| GET | `/k20mon/operations/rate-sync` | 定价任务状态 |
| PUT | `/k20mon/operations/rate-sync/config` | 定价配置 |
| POST | `/k20mon/operations/rate-sync/run` | 立即执行 |
| GET/PUT | `/k20mon/operations/frp/config` | frp 隧道读写 |
| POST | `/k20mon/operations/frp/restart` | 重启 frpc |
| GET | `/k20mon/operations/services/status` | 服务探活 |
| POST/PUT/DELETE | `/k20mon/operations/services[/:id]` | 服务目录 CRUD |
| PUT | `/k20mon/operations/services/owner` | 切换归属 |
| POST | `/k20mon/operations/services/order` | 拖拽排序 |

## 设计要点

- **后端无 root 权限**：容器只读挂载 `/sys`、`/proc`，写操作全部交给 root 代理，通过 Unix socket 单行 JSON 请求/响应，动作走白名单。
- **指针语义**：所有指标字段用 `*float64` / `*int64`，区分「值为 0」和「内核不提供」。
- **采集分层**：`CollectPower` 只读电池/充电器，保证 `/metrics/current` 不被慢速 thermal 节点拖住；完整 `Collect` 才读 procfs / thermal / cpufreq / devfreq。
- **历史不阻塞**：电池容量估算会扫全保留期样本，用 TTL 缓存 + 后台 goroutine 刷新，首次请求先返回旧值。
- **前端同源**：生产构建写死 `/api`，由 nginx 反代，不暴露后端端口；SSE 单独放宽超时。
- **无状态**：后端除 MySQL 外不落盘，容器可随时重建。

## 开发

```bash
# 后端
cd server && go test ./... && go build -o k20mon-server .

# 前端
cd web && npm install && npm run test && npm run dev   # 8898 端口，代理到 8888
```

## 说明

- 采集路径是 SM8150 的，换机型需要改 `collector/collector.go` 里的 `batteryPath` / `chargerPath` / GPU devfreq 路径。
- 控制代理的白名单动作和前端按钮一一对应，加动作要同时改 `service/control.go` 的白名单和 Python 的 `control_action`。
- 动态定价（`rate_sync.py`）操作的是外部脚本 `sync-group-rate.sh` 和 systemd timer，不属于本项目，缺少时相关按钮会报错，其余功能不受影响。

## License

MIT
