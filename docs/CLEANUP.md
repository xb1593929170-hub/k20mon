# 删减说明：相对原始代码去掉了什么

这份文档记录本次整理**删除**了什么、**保留**了什么、以及每个删除决定的理由，方便 review 和以后往回加。

整理前是一个 gin-vue-admin（GVA）仓库，里面有大量和「设备监控」无关的框架代码。整理目标是：**只留下真实运行的那条链路，任何一行代码都能被 `grep` 到调用点。**

## 一、整体对比

| 项 | 整理前 | 整理后 |
|---|---|---|
| 顶层目录 | `server/`（GVA 全套）、`server-standalone/`、`web/`（GVA 前端）、`deploy/` | `server/`、`web/`、`deploy/`、`docs/` |
| 文件数 | 778（源码约 390） | 59 |
| 前端源码 | 238 个文件 / 3.4 MB | 12 个文件 |
| 前端依赖 | 60 个（含 echarts、wangeditor、form-create、vue-office、unocss…） | 5 个运行时 + 2 个构建期 |
| 后端 | GVA 框架 + 插件双份代码 | 只保留 standalone 后端 |
| 代理脚本 | 1112 行，含无消费者的快照循环 | 约 600 行，只做 socket 服务 |

## 二、整个删除的部分

### 1. `server/`（原 gin-vue-admin 后端，约 4 MB）

**删掉的内容**：`api/`、`cmd/`、`core/`、`global/`、`initialize/`、`mcp/`、`middleware/`、`model/`、`plugin/`（含 `plugin/k20mon` 的 GVA 版本）、`resource/`、`router/`、`service/`、`source/`、`task/`、`utils/`、`docs/`、`config.yaml`。

**理由**：当前部署跑的是 `server-standalone`（16 个文件、独立 `go.mod`），GVA 后端正则根本没有在运行容器里。两者的 `k20mon` 采集逻辑是**同一份代码的两份拷贝**（`plugin/k20mon/collector/collector.go` 与 `server-standalone/collector/collector.go` 内容几乎一致），保留两份只会导致改一处漏一处。

**影响**：不再支持「把 K20Mon 当成 GVA 插件挂进 gin-vue-admin」这种部署方式。如果需要，回滚到原仓库取 `server/plugin/k20mon` 即可，两份代码当时是同步的。

### 2. `web/src/` 的 GVA 前端主体（约 3.3 MB / 226 个文件）

**删掉的内容**：

| 目录/文件 | 说明 |
|---|---|
| `src/view/**` | GVA 的 83 个页面（dashboard、superAdmin、systemTools、example…） |
| `src/api/**` | 27 个接口封装（user、casbin、autoCode、skills…），standalone 只用到 3 个 |
| `src/components/**` | 26 个通用组件（upload、richtext、office、charts…） |
| `src/pinia/**` | 状态管理，standalone 完全没用 |
| `src/router/`、`src/permission.js`、`src/pathInfo.json` | vue-router 与动态路由，standalone 用组件切换 |
| `src/plugin/{announcement,email,auto}` | 其它插件 |
| `src/utils/**`、`src/core/**`、`src/directive/**`、`src/hooks/**`、`src/config/` | GVA 基础设施 |
| `src/style/**`、`src/assets/icons/**` | 只在被删页面里引用的样式和图标 |
| `src/main.js`、`src/App.vue`、`index.html`、`vite.config.js` | GVA 入口，被 standalone 入口替代 |
| `src/qa-licensed-demo.js`、`uno.config.js`、`limit.js`、`openDocument.js` | 演示脚本、unocss 配置、内存限制脚本、自动开浏览器脚本 |
| `vitePlugin/`、`Dockerfile.gva`、`nginx.gva.conf`、`.env.*`(GVA 版) | GVA 构建链 |

**理由**：standalone 构建的入口是 `index-standalone.html` → `standalone-main.js` → `StandaloneApp.vue` → `plugin/k20mon/view/{dashboard,operations,services}.vue`。实测这三条 import 链**只依赖** `vue`、`element-plus`、`@element-plus/icons-vue`、`axios`、`sortablejs`，以及 `@/plugin/k20mon/*` 内部文件。GVA 的 `view/`、`api/`、`pinia/`、`router/` 全部不可达。

**顺带去掉的两个依赖**：

- **UnoCSS**：原 `vite.config.standalone.js` 引入了 `UnoCSS()` 插件，但三个视图里**没有一处**使用 utility class、`@apply` 或 `theme()`（全部样式写在 `<style scoped>` 里）。删掉插件和 `uno.config.js`，去掉 4 个依赖。
- **sass**：原配置有 `css.preprocessorOptions.scss`，但没有任何 `<style lang="scss">`。删掉。

### 3. `deploy/k20mon/` 里的部署脚本与旧配置

| 删掉的文件 | 理由 |
|---|---|
| `deploy.ps1`（302 行） | Windows PowerShell 一键部署，写死了作者自己的域名、转发端口、旧容器名和 GVA 目录结构。作为公开仓库的手工部署脚本门槛太高，改成 README + DEPLOY.md 里的可复制步骤。 |
| `docker-compose.gva.yml` | GVA 后端编排，已随 `server/` 删除。 |
| `nginx.gva.conf`（deploy 版，与 web 版重复） | 重复文件，保留 `web/nginx.conf` 一份。 |
| `k20mon.service` | GVA 后端 systemd unit，standalone 部署用 Docker。 |
| `test_rate_sync.py` → 保留但移到 `agent/` 下 | 归属调整为「代理的测试」。 |
| `services.json` | 含作者个人域名，改成 `services.json.example` 占位数据。 |
| `.env` | 含真实端口配置，改成 `.env.example`。 |

### 4. GVA 的 `config.yaml`（约 6 KB）

原文件有 `aliyun-oss`、`aws-s3`、`cloudflare-r2`、`autocode`、`captcha`、`cors`、`email`、`excel`、`local`、`oss`、`redis`、`system`、`zap` 等十几段。standalone 后端实际只读 **3 段**：`mysql`、`jwt`、`system`（见 `server/main.go` 的 `fileConfig`）。

删除其余全部，并把 `config.yaml` 换成 `config.yaml.example`（原文件含真实的 MySQL 用户名密码，绝不能入库）。

### 5. 后端死代码

| 删除项 | 位置 | 理由 |
|---|---|---|
| `AuditEntry`、`RecordAudit`、`OperationRecord` | `service/control.go` | 定义了 50 行审计记录逻辑，但**全仓库没有任何调用点**（`grep -rn 'RecordAudit' .` 只命中定义本身）。它写的 `sys_operation_records` 表也不在 `AutoMigrate` 里。 |
| `socketPaths` 回退列表 | `service/control.go` | 原代码在环境变量为空时会尝试两个路径。这个 fallback 是为了掩盖「包级变量 `Operations` 在 `main()` 之前初始化、读不到配置文件里的 socket 路径」这个真 bug。改成 `ControlClient.socket()` **延迟解析**后，fallback 不再需要。 |
| `services_update`、`services_create`、`services_delete`（代理侧） | `k20mon_agent.py` + Go 白名单 | 代理里实现了这三个动作，但 Go 侧的服务目录 CRUD 直接操作 MySQL（`service/services.go`），完成后调 `services_sync` 同步给代理。这三个动作**永远不会被调用**。 |
| `NewMetricsServiceWithCollector` 保留 | `service/metrics.go` | 被测试使用，保留。 |
| `ControlResponse` 的 `omitempty` | `model/control.go` | 修 bug：`online: 0` / `offline: 0` / `total: 0` / `checked_at: 0` 因为 `omitempty` 被整个丢掉，前端拿到 `undefined` 而不是 `0`。已加 `model/control_test.go` 回归测试。 |

### 6. 代理脚本的无效快照循环（约 300 行）

原 `k20mon-status.py` 的主循环每 5 秒做一次：

1. `wifi_status()` — 被 socket 的 `wifi_status` 动作覆盖，重复
2. `frp_status()`（约 120 行）—— 解析 `docker logs` 找节点连接状态，**没有任何消费者**
3. `write_snapshot()` 写 `/run/k20mon-status/status.json` —— **没有任何进程读它**

**证据**：

```bash
# 后端二进制里没有任何 status.json 相关字符串
strings k20mon-server | grep -c status.json   # → 0
# 全仓库搜不到读它的代码
grep -rn 'status.json' server/ web/src/        # → 无结果
```

删除 `snapshot()`、`write_snapshot()`、`frp_status()`、`parse_timestamp()`、`OUT`、`INTERVAL` 以及主循环，代理只保留 socket 服务。这样每 5 秒的 `docker logs --since 12h --tail 300` 调用（在手机上是不小的开销）也一并消失。

## 三、整段保留的部分

| 保留 | 说明 |
|---|---|
| `server/collector/collector.go` | 采集核心，逻辑不动，只补了测试 |
| `server/service/battery_history.go` | 容量估算算法完整保留（这是项目最有价值的部分） |
| `server/model/*` | 数据结构不动（除了 `omitempty` 修复） |
| `server/service/metrics.go` | 采样/历史/统计/SSE 不动 |
| `server/service/services.go` | 服务目录不动，只把 `DB` 全局变量换成 `atomic.Pointer` 以支持后台重连 |
| `web/src/k20mon/**` | 三个视图 + api + utils 完整保留，只改了 import 路径 |
| `deploy/agent/k20mon_agent.py` 的动作实现 | charge_guard / wifi / frp / cloudflare / services 全部保留 |
| `deploy/agent/rate_sync.py` | 保留为可选模块（依赖外部脚本，缺失时只有相关接口报错） |

## 四、原始代码里被保留但改名的路径

| 原名 | 新名 | 原因 |
|---|---|---|
| `server-standalone/` | `server/` | 只保留一个后端后不再需要 `-standalone` 后缀 |
| `module github.com/<原作者>/k20mon/server-standalone` | `github.com/yourname/k20mon/server` | 去掉个人 handle，占位符便于 fork 后替换 |
| `deploy/k20mon/k20mon-status.py` | `deploy/agent/k20mon_agent.py` | 它已经不只是「status」，是控制代理；同时和删除的旧 unit 区分开 |
| `web/src/standalone-main.js` | `web/src/main.js` | 只有一份入口 |
| `web/src/standalone/StandaloneApp.vue` | `web/src/App.vue` | 同上 |
| `web/src/standalone/{request,auth,api}.js` | `web/src/api/{client,auth}.js` | 合并 `auth.js` + `api.js`（都是登录态相关），`request.js` → `client.js` |
| `web/src/plugin/k20mon/{view,api,utils}` | `web/src/k20mon/{views,api,utils}` | 不再是「插件」，是应用本身 |
| `web/dist-standalone/` | `web/dist/` | 只有一个构建产物目录 |
| `web/index-standalone.html` | `web/index.html` | 同上 |

## 五、私人数据清理清单

源码里的业务逻辑一行没动，但所有指向真实环境的数据都换成了占位值。逐项列出，方便核对：

| 位置 | 原来 | 现在 |
|---|---|---|
| `server/model/services.go` `DefaultServiceCatalog` | 8 条作者自己的真实服务与个人域名 | 只留 3 条占位：K20Mon 自己 + `example-panel` / `example-tools` 指向 `example.com`，且默认停用 |
| `deploy/services.json.example` | 同上 8 条真实域名 | 同上占位 |
| `deploy/agent/k20mon_agent.py` `DEFAULT_SERVICES` | 同上（agent 侧的一份拷贝） | **整段删除** —— 修掉死代码后 agent 只从 `/etc/k20mon/services.json` 读，不再内置任何服务 |
| `deploy/agent/k20mon_agent.py` `CLOUDFLARED_TOKEN_FILE` | `/etc/cloudflared/<作者名>.token` | `/etc/cloudflared/tunnel.token` |
| `deploy/agent/rate_sync.py` 脚本 / installer / timer / service 的默认名 | 指向作者自己那套外部脚本与 systemd 单元名 | 换成中性的 `pricing-sync.sh` / `install-pricing-sync.sh` / `pricing-sync.timer` / `.service`，且全部可由 `K20MON_RATE_SYNC_*` 环境变量覆盖 |
| `deploy/agent/rate_sync.py` 文档串与 `scope` 字段 | 点名了某个第三方 API 网关产品 | 改成 `external pricing job` 这类中性描述 |
| `web/src/k20mon/views/services.vue` `serviceIcon()` | 图标匹配里硬编码了作者的服务名与分类词 | 只按 `id` + `category` 的中英文关键词猜图标，不绑定任何具体服务 |
| `web/src/k20mon/views/operations.vue` 二次确认文案 | 提到该第三方产品 | 改成「外部定价脚本」 |
| `web/src/api/auth.js` `SESSION_KEY` | `k20mon.standalone.session.v1` | `k20mon.session.v1` |
| `docs/*` 所有示例 | 真实 Wi-Fi SSID、内网 IP、转发端口、网关 IP | `MyWiFi-5G` / `192.168.1.100` / 示例端口；服务名统一为 `example-*` |
| `deploy/config.yaml`（DB 密码、JWT 密钥） | 真实值 | 不入库，只留 `config.yaml.example` 占位 |
| `deploy/k20mon/deploy.ps1` | 写死主机名、转发端口、旧容器名 | 整个删除，改成文档里的可复制步骤 |
| JWT 默认签名密钥 | 内置一个固定值 | 改为**必填**，缺失直接启动失败 |

清理后的验证（把模式换成你自己的即可）：

```bash
grep -rniE '你的域名|你的主机名|你的内网网段|你的转发端口|你的业务关键词' \
  --exclude-dir=.git .    # → 无输出
```

## 六、有意不做的改动

以下问题**本次没有改**，因为它们超出「删减 + 开源」的范围，或者会改变行为：

1. **采集路径写死 SM8150**：`batteryPath`、`chargerPath`、GPU devfreq 路径都是这台机器的。抽象成可配置需要引入机型探测，改动面大，改成在 `docs/DEPLOY.md` 里说明。
2. **JWT 密钥无默认值**：原代码内置了一个固定的默认签名密钥，本项目改成**必填**（缺失直接启动失败）。这是有意的行为变更，避免公开仓库带着一个人人皆知的默认密钥。同理，原代码里的默认服务目录种子指向作者的私有域名，已替换为占位地址。
3. **没有前端代码分割**：单包 1.2 MB（gzip 380 KB）。局域网使用够，公网可以再做 `manualChunks`。
4. **`rate_sync.py` 仍是外部依赖**：它控制的是一个外部的定价脚本 + systemd timer，脚本本身不属于本项目，所以默认值只能给一个中性名字。要用的话设 `K20MON_RATE_SYNC_SCRIPT` / `K20MON_RATE_SYNC_TIMER` 等环境变量指向你自己的脚本；不设也不影响其它功能，只是相关按钮会报「任务不可用」。
5. **没有审计日志**：删掉死代码后就没有审计功能了。如果需要，应该在 `callAction` 里加，而不是恢复原来那段没人调用的代码。

## 七、验证方式

删减后的仓库经过以下验证（详见 README「开发与校验」）：

```bash
# Go
cd server && gofmt -l . && go vet ./... && go test ./...
# → gofmt 无输出；vet 无输出；collector / model / model/request / service 四包全绿

# Python（18 个用例）
cd deploy/agent && python3 -m py_compile k20mon_agent.py rate_sync.py && python3 -m unittest test_agent test_rate_sync

# 前端
cd web && npm ci && npm run test && npm run build
# → 4 个用例通过；产出 dist/index.html + dist/assets/*

# 编排
cd deploy && cp .env.example .env && cp config.yaml.example config.yaml && docker compose config
```

以及在真机上跑通了完整链路：容器 healthy、`/health` 返回 200、未授权访问返回 401、
带签名 token 能读到真实电池/CPU/GPU/温度数据、能调用充电守护/Wi-Fi/frp/服务探活。
