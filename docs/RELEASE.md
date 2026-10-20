# 开源发布流程

从本地仓库到 GitHub，再到设备上跑起来，全流程。每一步都有可复制命令。

---

## 0. 发布前检查清单

开源前必须确认这几件事，任何一条不通过就别推：

```bash
cd /path/to/k20mon

# 1. 没有任何个人/生产信息残留
grep -rniE 'doudou525|dpdns\.org|frp-bus|qqxy|172\.16\.42|57923' \
  --exclude-dir=.git . || echo "干净"

# 2. 没有把本地配置带进去
git status --short
git ls-files | grep -E 'deploy/(config\.yaml|\.env)$' && echo "！！配置文件被跟踪了" || echo "配置未跟踪"

# 3. 全量校验
./scripts/verify.sh
```

第 3 条会跑：gofmt、go vet、go test、go build、python 单测、npm test、npm build、compose config。
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

### 推之前建议改掉模块路径占位符

仓库里 Go 模块路径是 `github.com/yourname/k20mon/server`，`yourname` 是占位符。
你的仓库地址是 `github.com/<你的ID>/k20mon` 的话，改掉它：

```bash
# Linux / macOS / Git Bash
grep -rl 'github.com/yourname/k20mon' --include='*.go' --include='go.mod' . \
  | xargs sed -i 's|github.com/yourname/k20mon|github.com/<你的ID>/k20mon|g'

# PowerShell
Get-ChildItem -Recurse -Include *.go,go.mod |
  ForEach-Object { (Get-Content $_ -Raw) -replace 'github.com/yourname/k20mon','github.com/<你的ID>/k20mon' |
  Set-Content $_ -NoNewline }

# 改完必须重新验证
cd server && go build ./... && cd .. && ./scripts/verify.sh --no-web
```

> 不改也能用（模块路径只影响 `go get`，本地和服务端构建都不受影响），
> 但改了更规范。`docs/DEPLOY.md` 和本文件里的示例也要同步替换。

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
