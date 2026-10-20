# K20Mon 常用操作
# 依赖：go 1.24+ / node 20+ / docker compose v2 / python3 3.10+
#
# 直接 `make` 看所有目标。

SHELL := /bin/bash
SERVER_DIR := server
WEB_DIR    := web
DEPLOY_DIR := deploy
AGENT_DIR  := $(DEPLOY_DIR)/agent
SQL_DIR    := $(DEPLOY_DIR)/sql
SCRIPTS    := scripts

.PHONY: help
help: ## 列出所有目标
	@echo "K20Mon 可用目标："
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ======================= 校验 =======================
.PHONY: check verify

check: ## 快速校验（gofmt / vet / 单测，跳过构建）
	./$(SCRIPTS)/verify.sh --quick

verify: ## 全量校验（= CI 跑的东西）
	./$(SCRIPTS)/verify.sh

# ======================= 后端 =======================
.PHONY: server-deps server-fmt server-vet server-test server-build server-run

server-deps: ## 拉取 Go 依赖
	cd $(SERVER_DIR) && go mod download

server-fmt: ## 格式化 Go 代码
	cd $(SERVER_DIR) && gofmt -w .

server-vet: ## Go 静态检查
	cd $(SERVER_DIR) && go vet ./...

server-test: ## 跑后端测试
	cd $(SERVER_DIR) && go test ./...

server-build: ## 构建后端二进制
	cd $(SERVER_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o k20mon-server .

server-run: ## 本地直接跑后端（需先配好 config.yaml）
	cd $(SERVER_DIR) && K20MON_CONFIG_PATH=../$(DEPLOY_DIR)/config.yaml go run .

# ======================= 前端 =======================
.PHONY: web-deps web-test web-build web-dev web-preview

web-deps: ## 安装前端依赖
	cd $(WEB_DIR) && npm install

web-test: ## 跑前端测试
	cd $(WEB_DIR) && npm run test

web-build: ## 构建前端静态文件到 web/dist
	cd $(WEB_DIR) && npm run build

web-dev: ## 前端开发服务器（代理到 8888）
	cd $(WEB_DIR) && npm run dev

web-preview: ## 预览构建产物
	cd $(WEB_DIR) && npm run preview

# ======================= 控制代理 =======================
.PHONY: agent-check agent-test agent-install agent-restart

agent-check: ## 代理语法检查
	cd $(AGENT_DIR) && python3 -m py_compile k20mon_agent.py rate_sync.py

agent-test: ## 代理单元测试
	cd $(AGENT_DIR) && python3 -m unittest test_agent test_rate_sync -v

agent-install: ## 安装 root 代理到本机（需 sudo）
	sudo install -d -m 0755 /usr/local/libexec /etc/k20mon /run/k20mon-status
	sudo install -m 0755 $(AGENT_DIR)/k20mon_agent.py /usr/local/libexec/
	sudo install -m 0644 $(AGENT_DIR)/rate_sync.py    /usr/local/libexec/
	sudo install -m 0644 $(AGENT_DIR)/k20mon-agent.service /etc/systemd/system/
	sudo install -m 0644 $(DEPLOY_DIR)/services.json.example /etc/k20mon/services.json
	sudo systemctl daemon-reload
	sudo systemctl enable --now k20mon-agent
	@systemctl is-active k20mon-agent && ls -l /run/k20mon-status/control.sock

agent-restart: ## 重启 root 代理
	sudo systemctl restart k20mon-agent && journalctl -u k20mon-agent -n 10 --no-pager

# ======================= 数据库 =======================
.PHONY: db-init db-schema db-admin db-shell

db-init: ## 建库 + 建表 + 建管理员（用法见 deploy/sql/create-admin.sh --help）
	./$(SQL_DIR)/create-admin.sh

db-schema: ## 只建库建表（需要 MYSQL_PWD）
	@test -n "$$MYSQL_PWD" || { echo "请先 export MYSQL_PWD=<root密码>"; exit 1; }
	mysql -h $${MYSQL_HOST:-127.0.0.1} -P $${MYSQL_PORT:-3306} -u $${MYSQL_USER:-root} < $(SQL_DIR)/01-schema.sql

db-admin: ## 只建管理员（需要 MYSQL_PWD）
	./$(SQL_DIR)/create-admin.sh --skip-schema

db-shell: ## 进 MySQL 命令行
	mysql -h $${MYSQL_HOST:-127.0.0.1} -P $${MYSQL_PORT:-3306} -u $${MYSQL_USER:-root} -p $${K20MON_DB:-k20mon}

# ======================= 镜像与发布 =======================
.PHONY: images images-cn images-multiarch images-save release

images: ## 构建 server + web 镜像（自动打日期 tag）
	./$(SCRIPTS)/build-images.sh

images-cn: ## 构建镜像（用 goproxy.cn）
	./$(SCRIPTS)/build-images.sh --cn

images-multiarch: ## 构建 amd64 + arm64 多架构镜像
	./$(SCRIPTS)/build-images.sh --multiarch

images-save: ## 构建镜像并导出离线 tar.gz（手机离线部署用）
	./$(SCRIPTS)/build-images.sh --cn --save

release: ## 打源码发布包到 dist/
	./$(SCRIPTS)/package-release.sh

release-full: ## 打包并附带前端构建产物
	./$(SCRIPTS)/package-release.sh --with-web-dist

# ======================= 容器栈 =======================
.PHONY: up down restart ps logs logs-web logs-server

up: ## 启动容器栈（会构建）
	cd $(DEPLOY_DIR) && docker compose up -d --build

down: ## 停止并删除容器
	cd $(DEPLOY_DIR) && docker compose down

restart: ## 重启容器栈
	cd $(DEPLOY_DIR) && docker compose restart

ps: ## 查看容器状态
	cd $(DEPLOY_DIR) && docker compose ps

logs: ## 跟随全部日志
	cd $(DEPLOY_DIR) && docker compose logs -f --tail=100

logs-server: ## 只看后端日志
	cd $(DEPLOY_DIR) && docker compose logs -f --tail=100 server

logs-web: ## 只看前端日志
	cd $(DEPLOY_DIR) && docker compose logs -f --tail=100 web
