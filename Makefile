# K20Mon 常用操作
# 依赖：go 1.24+ / node 20+ / docker compose v2

SHELL := /bin/bash
SERVER_DIR := server
WEB_DIR := web
DEPLOY_DIR := deploy
AGENT_DIR := $(DEPLOY_DIR)/agent

.PHONY: help
help: ## 列出所有目标
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------- 后端 ----------
.PHONY: server-deps server-fmt server-vet server-test server-build

server-deps: ## 拉取 Go 依赖
	cd $(SERVER_DIR) && go mod download

server-fmt: ## 格式化 Go 代码
	cd $(SERVER_DIR) && gofmt -w .

server-vet: ## 静态检查
	cd $(SERVER_DIR) && go vet ./...

server-test: ## 跑后端测试
	cd $(SERVER_DIR) && go test ./...

server-build: ## 构建后端二进制
	cd $(SERVER_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o k20mon-server .

# ---------- 前端 ----------
.PHONY: web-deps web-test web-build web-dev

web-deps: ## 安装前端依赖
	cd $(WEB_DIR) && npm install

web-test: ## 跑前端测试
	cd $(WEB_DIR) && npm run test

web-build: ## 构建前端静态文件
	cd $(WEB_DIR) && npm run build

web-dev: ## 起前端开发服务器（代理到 8888）
	cd $(WEB_DIR) && npm run dev

# ---------- 代理 ----------
.PHONY: agent-test agent-check

agent-test: ## 跑控制代理测试
	cd $(AGENT_DIR) && python3 -m unittest test_agent test_rate_sync -v

agent-check: ## 语法检查
	cd $(AGENT_DIR) && python3 -m py_compile k20mon_agent.py rate_sync.py

# ---------- 组合 ----------
.PHONY: check build up down logs

check: server-vet server-test web-test agent-test ## 全部检查（CI 同款）

build: server-build web-build ## 本地构建后端 + 前端

up: ## 启动容器栈
	cd $(DEPLOY_DIR) && docker compose up -d --build

down: ## 停止容器栈
	cd $(DEPLOY_DIR) && docker compose down

logs: ## 跟随日志
	cd $(DEPLOY_DIR) && docker compose logs -f --tail=100
