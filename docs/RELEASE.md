# 开源发布流程

从本地仓库到 GitHub，再到设备上跑起来，全流程。每一步都有可复制命令。

---

## 0. 发布前检查清单

开源前必须确认这几件事，任何一条不通过就别推：

```bash
cd /path/to/k20mon

# 1. 没有任何个人/生产信息残留
#    把下面的模式换成你自己的：域名、主机名、内网网段、转发端口、习惯用的密码前缀等
grep -rniE '你的域名|你的主机名|你的内网网段前缀|你的转发端口' \
  --exclude-dir=.git . || echo "干净"

# 2. 没有把本地配置带进去
git status --short
git ls-files | grep -E 'deploy/(config\.yaml|\.env)$' && echo "！！配置文件被跟踪了" || echo "配置未跟踪"

# 3. 占位符 yourname 已经全部替换成你的仓库地址（否则 README 给的 AI 直链全是 404）
grep -rn 'yourname/k20mon' --exclude-dir=.git \
     --exclude='RELEASE.md' --exclude='CLEANUP.md' . \
  || echo "占位符已替换"

# 4. 全量校验
./scripts/verify.sh
```

第 4 条会跑：gofmt、go vet、go test、go build、python 单测、npm test、npm build、compose config。
任何一个 FAIL 都先修掉再发。

> `scripts/package-release.sh` 里还有一道保险：打包时会检查 `deploy/config.yaml`
> 和 `deploy/.env` 是否被误带进去，带了就直接中止。

---

## 1. 初始化 git 仓库

```bash
cd /path/to/k20mon
git init
git add -A

# 确认要提交的文件（应该只有 60 个左右，不含 node_modules / dist / config.yaml）
git status --short | wc -l
git ls-files --others --exclude-standard | head

git commit -m "feat: K20Mon device monitor for Android-hosted Linux"
```

### Windows 下注意执行位

Windows 的 git 默认不记录可执行位，clone 到 Linux 后 `./scripts/*.sh` 会报 permission denied。
两个办法：

```bash
# 办法一：把执行位写进 git 索引（推荐，一次搞定）
git update-index --chmod=+x scripts/*.sh deploy/sql/*.sh
git commit -m "chore: mark shell scripts executable"

# 办法二：clone 之后手动加
chmod +x scripts/*.sh deploy/sql/*.sh
```

---

## 2. 推到 GitHub

```bash
# 方式一：已经建好空仓库
git remote add origin git@github.com:<你的ID>/k20mon.git
git branch -M main
git push -u origin main

# 方式二：用 gh CLI 直接建
gh repo create k20mon --public --source=. --remote=origin --push
```

### 推之前必须改掉占位符 `yourname`

仓库里凡是要指向你自己仓库的地方，用的都是占位符 `yourname`：

| 位置 | 内容 |
|---|---|
| `server/go.mod` 和所有 `*.go` 的 import | `github.com/yourname/k20mon/server` |
| `README.md` 的「交给 AI 处理」章节 | `github.com/yourname/k20mon` 和三条 `raw.githubusercontent.com/yourname/...` 链接 |
| `deploy/agent/k20mon-agent.service` 的 `Documentation=` | 仓库主页 |
| `docs/*` 里的示例链接 | 同上 |

**不改的后果**：Go 模块路径不影响构建（只影响 `go get`），但 README 里给使用者复制的
AI 直链会全部 404。

一次性替换（假设你的仓库是 `github.com/<你的ID>/k20mon`）：

**注意要替换两个模式**：`github.com/yourname/k20mon` 匹配不到
`raw.githubusercontent.com/yourname/k20mon`（前者不是后者的子串），所以 README 里那三条
AI 直链要单独换一次。

```bash
# Linux / macOS / Git Bash
FILES=$(grep -rl 'yourname/k20mon' --include='*.go' --include='go.mod' \
        --include='*.md' --include='*.service' .)

# 1) 仓库地址（同时覆盖 go.mod、import、git clone 那条）
echo "$FILES" | xargs sed -i 's|github\.com/yourname/k20mon|github.com/<你的ID>/k20mon|g'
# 2) raw 直链（AI 读文档用，必须单独换）
echo "$FILES" | xargs sed -i 's|raw\.githubusercontent\.com/yourname/k20mon|raw.githubusercontent.com/<你的ID>/k20mon|g'
```

```powershell
# PowerShell
Get-ChildItem -Recurse -Include *.go,go.mod,*.md,*.service |
  Where-Object { $_.FullName -notlike '*\.git\*' } |
  ForEach-Object {
    (Get-Content $_ -Raw) `
      -replace 'github\.com/yourname/k20mon','github.com/<你的ID>/k20mon' `
      -replace 'raw\.githubusercontent\.com/yourname/k20mon','raw.githubusercontent.com/<你的ID>/k20mon' |
      Set-Content $_ -NoNewline
  }
```

改完确认代码和链接里一个都不剩：

```bash
# 排除本文件和 CLEANUP.md —— 它们本身就在讲解这个占位符，会被误命中
grep -rn 'yourname/k20mon' --exclude-dir=.git \
     --exclude='RELEASE.md' --exclude='CLEANUP.md' . \
  || echo "代码与链接里已无占位符"
```

实测：替换前全仓库 33 处命中，按上面两条 sed 跑完后，只剩本文件和 CLEANUP.md 里的说明文字。

然后重新验证（模块路径变了必须重建）：

```bash
cd server && go build ./... && cd .. && ./scripts/verify.sh --no-web
```

> 改名后实测通过：`go vet` / `go test` / `go build` 全部正常，README 里三条 raw 直链也都换成了新地址。

### 推送后验证 AI 直链真的能用

README 里给使用者复制的三条链接必须是 200，否则别人粘给 AI 就是 404：

```bash
OWNER=<你的ID>
for f in docs/AI-RESTORE.md docs/DEPLOY.md docs/API.md; do
  curl -sS -o /dev/null -w "%{http_code}  $f\n" \
    "https://raw.githubusercontent.com/$OWNER/k20mon/main/$f"
done
```

期望四行全是 `200`。

> **为什么用 `raw.githubusercontent.com` 而不是 `github.com/<owner>/<repo>/raw/...`**：
> 后者会 302 跨域跳到前者。实测部分 AI 的网页抓取工具**不跟随跨域重定向**，
> 会直接报 `fetch failed`。直链才稳。
> 同理，整包下载要用 `https://github.com/<owner>/<repo>/archive/refs/heads/main.tar.gz`（实测 200，返回 application/x-gzip）。

### 仓库设置建议

| 项 | 值 |
|---|---|
| Description | `Device monitor + ops console for Linux-on-Android (Redmi K20 Pro / SM8150)` |
| Topics | `golang` `vue3` `element-plus` `docker` `android` `sm8150` `monitoring` `sysfs` `self-hosted` |
| LICENSE | 已有 MIT（`LICENSE` 文件），GitHub 会自动识别 |
| CI | `.github/workflows/ci.yml` 已就绪，推上去自动跑 |

---

## 3. 打 tag 与发 Release

```bash
git tag -a v1.0.0 -m "K20Mon 1.0.0"
git push origin v1.0.0
```

### 源码发布包

```bash
# 纯源码包（dist/k20mon-1.0.0.tar.gz + .sha256）
./scripts/package-release.sh --version 1.0.0

# 附带前端构建产物（设备上没有 node 时可以直接用）
./scripts/package-release.sh --version 1.0.0 --with-web-dist
```

上传到 GitHub Release：

```bash
gh release create v1.0.0 \
  dist/k20mon-1.0.0.tar.gz \
  dist/k20mon-1.0.0.tar.gz.sha256 \
  --title "K20Mon 1.0.0" \
  --notes-file CHANGELOG.md
```

### 容器镜像

本项目**不强制发布镜像**：镜像里包含采集逻辑，和机型绑定较深，让使用者自己构建更合适。
如果你要发布（比如给同机型的人用）：

```bash
# 推到 Docker Hub
docker login
./scripts/build-images.sh --tag 1.0.0 --registry docker.io/<你的ID>/ --push

# 推到 GitHub Container Registry
echo "$GITHUB_TOKEN" | docker login ghcr.io -u <你的ID> --password-stdin
./scripts/build-images.sh --tag 1.0.0 --registry ghcr.io/<你的ID>/ --push

# 多架构（x86 开发机 + arm64 手机）
./scripts/build-images.sh --tag 1.0.0 --registry ghcr.io/<你的ID>/ --multiarch --push
```

推完之后，使用者在 `.env` 里改镜像名即可：

```ini
# deploy/.env
K20MON_IMAGE_TAG=1.0.0
```
```yaml
# deploy/docker-compose.yml 里的 image: 改成
    image: ghcr.io/<你的ID>/k20mon/server:${K20MON_IMAGE_TAG:-latest}
```

---

## 4. 离线分发（手机不能拉镜像时）

这是本项目最常见的分发方式：开发机构建好镜像，导出成 tar，传到手机 `docker load`。

```bash
# 开发机（架构要和手机一致，手机的 arm64 就用 arm64 机器构建）
./scripts/build-images.sh --tag 1.0.0 --cn --save
# → dist/k20mon-images-1.0.0.tar.gz（约 70 MB）

# 传到设备
scp dist/k20mon-images-1.0.0.tar.gz dist/k20mon-1.0.0.tar.gz root@<设备>:/tmp/

# 设备上部署
ssh root@<设备>
gunzip -c /tmp/k20mon-images-1.0.0.tar.gz | docker load

tar -xzf /tmp/k20mon-1.0.0.tar.gz -C /opt          # → /opt/k20mon
cd /opt/k20mon
chmod +x scripts/*.sh deploy/sql/*.sh

./deploy/sql/create-admin.sh --password 'MySQL密码' --db k20mon
cd deploy && cp .env.example .env && cp config.yaml.example config.yaml
chmod 600 config.yaml   # 编辑 mysql.path / username / password / jwt.signing-key

cd .. && make agent-install
cd deploy && K20MON_IMAGE_TAG=1.0.0 docker compose up -d --no-build
```

跨架构（x86 开发机 → arm64 手机）不能走 `--save`，必须用 `--multiarch --push` 再让设备 pull。

---

## 5. 数据库脚本说明

仓库里和数据库相关的三个文件：

| 文件 | 用途 | 谁用 |
|---|---|---|
| `deploy/sql/01-schema.sql` | 建库 + 三张表的完整 DDL | DBA 先审 DDL；或后端账号不给 DDL 权限时手工建表 |
| `deploy/sql/02-admin-user.sql` | 建管理员的 SQL 模板 | 手工执行时用（需自己填 bcrypt hash） |
| `deploy/sql/create-admin.sh` | 上面两步的自动化：建库 → 建表 → 建管理员 → 校验 | 日常用这个 |

```bash
# 一键（推荐）
./deploy/sql/create-admin.sh --host 127.0.0.1 --user root --password 'root密码' \
  --db k20mon --username admin
# 不指定 --app-pass 会自动生成随机密码并打印

# 只看 SQL 不执行
./deploy/sql/create-admin.sh --password 'root密码' --sql-only

# 库表已存在，只加管理员
./deploy/sql/create-admin.sh --password 'root密码' --skip-schema --username ops
```

`create-admin.sh` 会自动降级找依赖，不需要预先装东西：

- bcrypt：`python3 -c 'import bcrypt'` → `htpasswd` → `docker run python:3.12-alpine`
- mysql 客户端：本地 `mysql` → `docker run mysql:8 mysql`

> **`sys_users` 表为什么长这样**：后端复用了 gin-vue-admin 的表结构，只读它来登录
> （`server/main.go` 的 `userRecord`）。如果你是从已有的 GVA 部署迁移，这张表已经存在
> 且有数据，用 `--skip-schema` 只加一个能登录的账号即可。

---

## 6. 常见发布问题

| 问题 | 原因 | 解决 |
|---|---|---|
| CI 报 `gofmt` 失败 | 本地用了不同版本的 gofmt | `cd server && gofmt -w .` 再提交 |
| CI 报 `npm ci` 失败 | `package-lock.json` 和 `package.json` 不同步 | `cd web && npm install` 更新锁文件后提交 |
| clone 到 Linux 后脚本跑不了 | Windows 丢了执行位 | `git update-index --chmod=+x scripts/*.sh deploy/sql/*.sh` |
| 仓库里出现了 `deploy/config.yaml` | `.gitignore` 没生效（文件已被跟踪） | `git rm --cached deploy/config.yaml` 后提交 |
| 推送被拒（大文件） | 误提交了 `node_modules` 或镜像 tar | `git rm -r --cached web/node_modules dist` |
| `docker pull` 被限流 | Docker Hub 匿名拉取限制 | 用 `--save` 离线包，或换 registry |

---

## 7. 版本号约定

给使用者看的版本号，建议 `主版本.次版本.修订号`：

| 变更类型 | 例子 | 影响 |
|---|---|---|
| 主版本 | `1.0.0 → 2.0.0` | 数据库表结构变了、控制协议改动作名、配置格式不兼容 |
| 次版本 | `1.0.0 → 1.1.0` | 新增接口/动作、新增指标字段、前端加页面 |
| 修订号 | `1.0.0 → 1.0.1` | 修 bug、改文案、调样式 |

镜像 tag 用同一个版本号，方便回滚：

```bash
./scripts/build-images.sh --tag 1.1.0 --cn
cd deploy && K20MON_IMAGE_TAG=1.1.0 docker compose up -d
```
