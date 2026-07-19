# ivideo 后端构建管理
# 用法：make <目标>，例如 make build / make run

# 变量
BINARY      := ivideo-server
BIN_DIR     := bin
PKG         := .
GO          := go
GOFLAGS     :=
LDFLAGS     := -s -w
# 交叉编译目标（部署到 Linux 服务器时用：make build-linux）
LINUX_ARCH  ?= amd64

.PHONY: all build build-linux run tidy fmt vet test clean help

## all: 默认执行，格式化 + 检查 + 构建
all: fmt vet build

## build: 编译当前平台可执行文件到 bin/
build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

## build-linux: 交叉编译 Linux 静态二进制（用于 Docker/服务器部署）
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_ARCH) \
		$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-linux-$(LINUX_ARCH) $(PKG)

## run: 直接运行（开发用）
run:
	$(GO) run $(PKG)

## tidy: 整理依赖
tidy:
	$(GO) mod tidy

## fmt: 格式化代码
fmt:
	$(GO) fmt ./...

## vet: 静态检查
vet:
	$(GO) vet ./...

## test: 运行测试
test:
	$(GO) test ./...

## clean: 清理构建产物
clean:
	rm -rf $(BIN_DIR)

## help: 显示所有可用目标
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
