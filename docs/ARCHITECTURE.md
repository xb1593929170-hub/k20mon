# 架构说明

## 一句话

三个进程：**Go 后端只读采集 + 提供 HTTP**，**Vue 前端只做展示和触发**，**Python root 代理独占所有特权写操作**，三者之间只有两条边界——HTTP（浏览器↔后端）和 Unix socket（后端↔代理）。

```text
┌──────────────┐   HTTP :8899    ┌───────────────┐   HTTP :8888    ┌──────────────────┐
│   浏览器      │ ──────────────► │ nginx (web)   │ ──────────────► │  Go 后端 (server) │
│  Vue 3 SPA   │ ◄────────────── │ 静态 + /api   │ ◄────────────── │  Gin + GORM       │
└──────────────┘                 └───────────────┘                 └────────┬─────────┘
                                                                            │
                                              ┌─────────────────────────────┼──────────────────┐
                                              │ 只读                        │ Unix socket      │
                                              ▼                             ▼                  │
                                     ┌────────────────┐        ┌────────────────────────┐    │
                                     │ /sys  /proc    │        │ root 代理 (Python)      │    │
                                     │ 电池 充电器     │        │ 白名单动作              │    │
                                     │ CPU GPU 温度   │        │ ├─ sysfs 写入(充电守护) │    │
                                     │ 网络            │        │ ├─ nmcli (Wi-Fi)        │    │
                                     └────────────────┘        │ ├─ docker (frp 重启)    │    │
                                                               │ ├─ frpc.ini 读写        │    │
                                                               │ └─ systemd timer 查询   │    │
                                                               └────────────────────────┘    │
                                                                            │                 │
                                                                            ▼                 │
                                                                   ┌────────────────┐        │
                                                                   │    MySQL       │ ◄──────┘
                                                                   │ 历史采样/服务目录│
                                                                   └────────────────┘
```

## 进程与职责边界

| 进程 | 运行身份 | 能做 | 不能做 |
|---|---|---|---|
| `web`（nginx） | 容器内 root | 托管静态文件、反代 `/api` | 任何业务逻辑 |
| `server`（Go） | 容器内 root（但只挂只读 `/sys` `/proc`） | 采集、鉴权、落库、转发控制请求 | 改 sysfs、跑 nmcli、重启容器 |
| `k20mon-agent`（Python） | 宿主机 root | 白名单内的所有特权操作 | 对外提供 TCP 端口（只有 Unix socket） |

**为什么写操作必须走代理**：手机上的 sysfs 写入（`current_max`）、`nmcli`、`docker restart` 都需要 root，而后端跑在容器里，把 `/sys` 以读写方式挂进容器等于把宿主机内核参数的控制权交给一个暴露在 8899 端口的 HTTP 服务。所以后端只读挂载 `/sys`，写操作交给一个**只监听 Unix socket、只有白名单动作、不暴露任何网络端口**的 root 进程。

代理的 `control_action` 是一张 `if/elif` 分派表，表外的动作一律返回 `{"ok": false, "error": "不支持的操作"}`；Go 侧还有一份 `allowedControlActions` 白名单做双重校验（`server/service/control.go`）。

## 数据采集层

全部在 `server/collector/collector.go`。核心设计：`Collector.root` 可注入，默认 `/`，测试时传 `t.TempDir()`，于是采集逻辑能在任何机器上单测（`collector_test.go`）。

### 路径

```go
const (
    batteryPath = "sys/class/power_supply/qcom-battery"
    chargerPath = "sys/class/power_supply/pm8150b-charger"
)
```

GPU devfreq 路径写死 `sys/devices/platform/soc@0/2c00000.gpu/devfreq/2c00000.gpu`，找不到时依次回退到 `sys/class/devfreq/*gpu*` 和 `sys/class/devfreq/*`。

### 读取与回退

每个数值节点优先读独立文件，读不到再回退到同目录 `uevent` 里的 `POWER_SUPPLY_*` 键（`readIntOrUevent`）。这是因为部分高通内核只暴露 uevent 而不暴露单独文件。

### 单位换算

| 字段 | 原始单位 | 换算 | 代码位置 |
|---|---|---|---|
| `VoltageV` | µV | ÷ 1e6 | `collectBattery` |
| `CurrentMA` | µA | ÷ 1e3 | `collectBattery` |
| `PowerW` | µV × µA | ÷ 1e12 | `collectBattery` |
| `TemperatureC` | 0.1 ℃ | ÷ 10 | `collectBattery` |
| `CurrentMaxUA` | µA | 原值 | `collectCharger` |
| `FrequencyMHz` (CPU) | kHz | ÷ 1000 | `cpuFrequencies` |
| `FrequencyMHz` (GPU) | Hz | ÷ 1e6 | `readFreqMHz` |
| 温度（thermal） | m℃ | ÷ 1000 | `temperatures` |
| 内存 | kB | ÷ 1024 → MB | `memory` |

### 为什么全用指针

`*float64` / `*int64` / `*bool` 用来区分三种状态：

- `nil` —— 内核没有这个节点（比如只提供 `energy_*` 不提供 `charge_*` 的电池驱动）
- `&0` —— 节点存在且值确实是 0（比如放电时 `current_now = 0`）

如果用零值表示"不可用"，前端就无法区分"电流为 0"和"读不到电流"，图表会出现假的 0 值（`server/model/metrics.go` 顶部注释写明了这一点）。

## 采集分层策略

`Collect`（全量）和 `CollectPower`（只读电池+充电器）是两条路径：

- `Collect` 读 procfs、thermal、cpufreq、devfreq，其中 **thermal 是最慢的**（部分 zone 读一次要几百毫秒）。
- `CollectPower` 故意跳过所有这些，只读两个 power_supply 目录。

`MetricsService.Current` 只调 `CollectPower`，然后把上一次的 `System` / `Network` 原样复用（`server/service/metrics.go` `Current`）。再叠加 `currentSnapshotTTL = 8s`：快照 8 秒内直接返回，不重复采集。

配合后台采样器（`StartSampler`）每 `K20M_INTERVAL`（默认 5 秒）跑一次 `Collect` 全量并 publish，前端拿到的 `/current` 永远是"电池充电器实时 + 系统数据最多 5 秒旧"，响应时间稳定在毫秒级。

thermal 还有独立熔断：单次读 200ms 超时、整轮 750ms 预算，一旦超时或读失败就置 `thermalReadDisabled = true`，后续轮次直接跳过整个 thermal 扫描（`temperatures`）。

## 存储层

两张表，都由 `AutoMigrate` 自动创建（`server/main.go`）：

### `k20mon_metric_samples`

```go
type MetricSample struct {
    ID                       uint      `gorm:"primaryKey"`
    SampledAt                time.Time `gorm:"index;not null"`
    Payload                  string    `gorm:"type:longtext;not null"`   // 完整快照 JSON
    BatteryCapacity          *int64
    BatteryCurrentMA         *float64  `gorm:"column:battery_current_ma"`
    BatteryVoltageV          *float64  `gorm:"column:battery_voltage_v"`
    BatteryStatus            string    `gorm:"column:battery_status;size:24"`
    BatteryDesignCapacityUah *int64    `gorm:"column:battery_design_capacity_uah"`
    BatteryPowerW            *float64
    BatteryConsumptionW      *float64
    BatteryTemperatureC      *float64
    CPUUsagePercent          *float64
    HottestTemperatureC      *float64
    MemoryUsedPercent        *float64
    NetworkRXBPS             *float64
    NetworkTXBPS             *float64
}
```

**payload + 冗余列**的组合是有意的：

- `payload` 存完整快照，保证任何字段以后都能回溯，不用改表；
- 冗余列给图表和容量估算用，避免每次都 `JSON_EXTRACT` 扫全表（MySQL 对 JSON 列的表达式索引支持有限）。

`History` 走 payload 反序列化（要完整数据）；`loadBatteryHistory` 只 `SELECT` 冗余列（只要 8 个数），并对 MySQL 做 `COALESCE(列, JSON_EXTRACT(payload, ...))` 兼容早期只写了 payload 的数据（`server/service/battery_history.go`）。

### `k20mon_service_catalog`

服务宫格的数据源，`id` 是业务主键（字符串），`sort_order` 控制拖拽顺序，`owner_type` 只有 `mine` / `other`。

### 保留期

`retentionDuration()` 读 `K20M_RETENTION_H`（默认 720 小时 = 30 天）。采样器每小时 `Prune` 一次，删除 `sampled_at < now - retention` 的行。

按 5 秒采样算，30 天约 51.8 万行，单表可控；`History` 默认 `limit=20000`（约 1 天），上限 60 万。

## 电池容量估算算法

驱动通常只提供 `charge_full_design`（设计容量），不提供真实满充容量。`deriveBatteryHistory`（`server/service/battery_history.go`）用充放电过程反推：

**原理**：一段充放电区间内，电流对时间积分得到转移的电荷量 `ΔQ`；同时电量百分比变化 `ΔP`。那么实际满充容量 ≈ `ΔQ × 100 / |ΔP|`。

**步骤**：

1. 相邻两行样本，`elapsed = t[i] - t[i-1]`。`elapsed <= 0` 或 `> 30s`（`batteryMaxSampleGap`）视为采样断档，结束当前区间。
2. 判定方向：状态含 `charging` → 充电；`discharging` / `not charging` → 放电。
3. 算这段的电荷量：两行都有 `currentMa` 时用梯形积分 `(|i_prev| + |i_cur|) / 2 × hours × 1000`；否则若设计容量已知且百分比朝正确方向变化，退回 `|Δpercent| × designUah / 100` 并标记 `UsedDesignFallback`。
4. 累计区间：只在方向合法（充电时百分比不降、放电时百分比不升）且方向没变时累计 `chargeUah`、`startPercent`、`endPercent`、`completeCurrent`。
5. 区间结束时，若整段都有电流数据且 `|Δpercent| >= 5` 且 `chargeUah > 0`，算一个估算值 `chargeUah × 100 / |Δpercent|`，落在 `[100mAh, 20Ah]` 才采纳。
6. 多个区间的估算值**取中位数**（偶数个取中间两个平均）作为 `EstimatedFullUah`，`EstimatedHealthPercent = median / designUah × 100`。
7. `EquivalentCycles = dischargedUah / fullUah`；`fullUah` 优先用估算值，没有才用设计容量。

**为什么用中位数**：个别区间会因为采样抖动（比如 30 秒边界上跨了一次状态切换）产生离群估算，平均值会被一两个坏区间拉飞，中位数不会。

**为什么不阻塞**：这个算法要扫最多 60 万行，绝不能放在 HTTP 请求里同步跑。`batteryHistorySummary` 用 1 分钟 TTL 缓存 + 后台 goroutine 刷新：请求进来先返回缓存值（可能是空的），同时触发一次刷新，下一个请求就能拿到新值。刷新带 2 分钟超时，`batteryHistoryRefreshing` 防重入。

## 控制协议

### 报文格式

一行 JSON 请求 → 一行 JSON 响应，双方都按 `\n` 分帧。

```json
{"action":"wifi_scan","data":{"interface":"wld0"}}
{"ok":true,"interface":"wld0","networks":[{"ssid":"MyNet","signal":88,"security":"WPA2"}]}
```

Go 侧实现在 `server/service/control.go` `ControlClient.Call`：

- 动作必须在 `allowedControlActions` 里，否则直接报错不发请求；
- `net.Dialer{Timeout: 35s}` 连 socket，`SetDeadline` 同步设 35 秒；
- 写 `payload + "\n"`，然后用 `json.NewDecoder(bufio.NewReader(conn)).Decode(&response)` 读一行。

Python 侧在 `deploy/agent/k20mon_agent.py` `control_server` + `handle_connection`：

- `socket.AF_UNIX` / `SOCK_STREAM`，`listen(8)`；
- 每个连接起一个线程处理，主循环不阻塞（5 个并发请求实测全部有响应）；
- 单次请求上限 128 KiB，超时 35 秒；
- 解析失败回 `{"ok": false, "error": "请求格式错误: JSONDecodeError"}`，进程不挂。

### 白名单动作全表

| action | data | 关键返回字段 | 特权来源 |
|---|---|---|---|
| `charge_guard_get` | `{}` | `config`, `mode`, `capacity`, `current_max_ua` | 读 sysfs + 配置文件 |
| `charge_guard_set` | `{enabled, low_threshold, resume_threshold, low_current_ua, normal_current_ua}` | 同 get + `message` | 写 sysfs `current_max` + 配置文件 |
| `wifi_status` | `{}` | `wifi{available, interface, state, connected, ssid, ip, cidr, gateway, signal, frequency_mhz}` | `nmcli device show` + `ip` |
| `wifi_scan` | `{interface?}` | `networks[{ssid, in_use, signal, security, channel, frequency_mhz, interface}]` | `nmcli device wifi list --rescan yes` |
| `wifi_connect` | `{interface?, ssid, password}` | `ok`, `message` | `nmcli device wifi connect` |
| `wifi_disconnect` | `{interface?}` | `ok`, `message` | `nmcli device disconnect` |
| `cloudflare_status` | `{}` | `cloudflare{running, mode, tunnel_id, ingress[], token_configured}` | 读 `config.yml` + `ps -eo args=` |
| `frp_config` | `{}` | `tunnels{name:{type, local_ip, local_port, remote_port, status}}` | `docker inspect` 找挂载 + 读 ini |
| `frp_apply` | `{tunnels:[...]}` | `ok`, `message`, `tunnels` | 写 frpc.ini（保留 common 段） |
| `frp_restart` | `{}` | `ok`, `message` | `docker restart` |
| `services_status` | `{services?}` | `total`, `online`, `offline`, `services[]` | 并发 `curl` 探活 |
| `services_sync` | `{services:[...]}` | `ok`, `total` | 写 `/etc/k20mon/services.json` |
| `rate_sync_status` | `{}` | `rate_sync{...}` | `systemctl show` + 读日志 |
| `rate_sync_config` | 配置对象 | 同 status | 写 systemd unit 环境 |
| `rate_sync_run` | `{}` | `ok`, `...` | 触发一次外部脚本 |

### 错误约定

- 成功：`{"ok": true, ...}`，可能带 `message`
- 业务失败：`{"ok": false, "error": "<中文原因>"}`，可能带 `detail`（截断到 240 字符的内部错误）
- Go 侧 `callAction` 把 `ok: false` 转成 HTTP 200 + `{"code":7,"msg":<error>}`

### socket 权限

代理启动时 `os.chmod(socket, int(os.environ.get("K20MON_CONTROL_SOCKET_MODE", "0o660"), 8))`。

默认 `0660`（root:root）意味着只有 root 能连。后端容器默认以 root 运行所以能连；如果把后端改成非 root，必须把 `K20MON_CONTROL_SOCKET_MODE` 设成 `0666`，或者把 socket 目录 chgrp 到后端用户的 gid。

## HTTP API 设计约定

- **统一包**：`{"code":0,"data":...,"msg":"..."}`，HTTP 一律 200；业务失败 `code=7`。
- **鉴权失败**：HTTP 401 + `{"code":7,"data":null,"msg":"未登录或非法访问，请登录"}`。
- **鉴权头**：`x-token`，JWT HS256，解析时显式检查 `token.Method == jwt.SigningMethodHS256`（防 alg 混淆攻击）。
- **SSE**：`/k20mon/metrics/stream`，格式 `event: metrics\ndata: {json}\n\n`，连上先推一次当前快照，之后每次 publish 推一次。
- **用户表**：直接复用 gin-vue-admin 的 `sys_users`（`server/main.go` `userRecord`），这样能从已有部署里平滑迁移账号。

## 前端结构

```
src/
├── main.js          装 Element Plus + 挂 globalThis.__K20MON_REQUEST__
├── App.vue          登录页 / 顶栏 / 三视图切换
├── api/
│   ├── client.js    axios 单例 + 请求/响应拦截器
│   └── auth.js      登录态 localStorage + login/getUserInfo
└── k20mon/
    ├── api/metrics.js              全部业务接口
    ├── utils/dashboard-cache.js    localStorage 容错读写
    ├── utils/dashboard-refresh.js  去重 + summary 节流
    └── views/{dashboard,operations,services}.vue
```

### 不用 vue-router

三个页面靠 `App.vue` 里的 `navItems` 数组 + `<component :is="activeComponent">` 切换。理由：整个应用只有三个平级页面，没有嵌套路由、没有 URL 参数、没有前进后退需求，引入 vue-router 只是多一个依赖和一套配置。

### 请求层

`client.js` 的响应拦截器直接 `return response.data`，所以业务代码拿到的是后端的 `{code,data,msg}` 而不是 axios 的包装。401 时清登录态并广播 `k20mon:session-expired` 事件，`App.vue` 监听后踢回登录页——这样任何一处的 token 过期都能统一处理。

### 仪表盘轮询

`createDashboardRefresher` 解决两个问题：

1. **去重**：`currentInFlight` 守卫，同一时刻只有一个 current 请求在飞，重复调用复用同一个 Promise。避免定时器和手动刷新撞车。
2. **节流**：summary 比 current 重得多（要扫历史表），所以单独用 `nextSummaryAt` 控制，默认 60 秒一次，`forceSummary` 可以强制刷新。

单个请求失败只调 `onError(error, 'current'|'summary')`，不影响另一个——summary 挂了仪表盘照样显示实时数据。

再叠加 localStorage 缓存（`dashboard-cache.js`）：刷新页面时先渲染上一次的数据，再等新数据覆盖，避免白屏。

## 容错设计

| 故障 | 表现 | 处理 |
|---|---|---|
| MySQL 不可用 | 后端照常启动 | `connectDatabase` 后台每 10 秒重试，`/health` 正常，登录返回"数据库暂不可用" |
| MySQL 中途断开 | 采样继续 | `Save` 返回错误，快照追加 warning `history persistence unavailable`，`/current` 不受影响 |
| root 代理没起 | 操作中心全报错 | 接口返回 `root control agent is unavailable`，仪表盘（只读）照常工作 |
| 代理动作超时 | 请求挂 35 秒 | `net.Dialer` + `SetDeadline` 双重超时 |
| thermal 节点卡住 | 采集变慢 | 200ms 单次超时 + 750ms 整轮预算 + 熔断 |
| token 过期 | 任意接口 401 | 拦截器清登录态 + 广播事件 → 回登录页 |
| localStorage 不可用 | 隐私模式 | 所有读写 try/catch，退化成仅内存登录态 |

## 已知限制

- **采集路径写死 SM8150**：换机型要改 `batteryPath` / `chargerPath` / GPU devfreq 路径。
- **单用户体系**：没有多租户、没有权限分级，`sys_users` 里能登录的账号权限都一样。
- **无审计表**：控制操作不留审计记录（早期版本有 `sys_operation_records`，但没有任何调用点，已删除）。
- **不支持 SQLite**：只用 MySQL，手机上没有轻量替代方案。
- **`rate_sync.py` 依赖外部脚本**：`pricing-sync.sh` + systemd timer 不属于本项目，缺了只是相关按钮报错。
- **前端没有代码分割**：打包成单个 1.2 MB 的 js（gzip 380 KB），对局域网使用够了，公网访问可以再优化。
- **`K20M_RETENTION_H` 只是上限**：实际统计跨度取决于数据库里真实存在的样本。
