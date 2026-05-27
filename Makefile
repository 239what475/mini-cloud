# mini-cloud 的 Makefile 只放“本地工程动作”：
# - 代码格式、静态检查、单元测试
# - 二进制构建、release 构建
# - proto 生成、清理构建产物
#
# 会启动数据库、服务进程或 runtime 容器的流程不放在这里。
# 那类流程属于环境编排测试，入口放在 scripts/ 下，例如：
# - ./scripts/test-integration.sh
# - ./scripts/smoke.sh

# 使用 bash 是因为 check 目标里会用到 [[ ... ]]、pipefail 等 bash 行为。
SHELL := /bin/bash

# -e: 任意命令失败就退出
# -u: 使用未定义变量就退出
# -o pipefail: 管道中任意一段失败，整条管道失败
.SHELLFLAGS := -eu -o pipefail -c

# 让一个 target 的多行命令在同一个 shell 进程中执行。
# 这样变量赋值、cd、函数定义可以在同一个 target 内继续生效。
.ONESHELL:

# ROOT_DIR 固定为执行 make 时所在的项目目录。
# 本 Makefile 预期在 projects/mini-cloud/ 下运行。
ROOT_DIR := $(CURDIR)

# tests 是一个独立 Go module，不能只靠根 module 的 go test ./... 覆盖。
TESTS_DIR := $(ROOT_DIR)/tests

# web 目录是可选的：没有前端时 web-check / web-build 会自动跳过。
WEB_DIR := $(ROOT_DIR)/web

# 默认开发构建输出目录。
# 调用方可以用 `make build BINARY_DIR=/tmp/bin` 覆盖。
BINARY_DIR ?= $(ROOT_DIR)/dist/bin

# release 默认构建 linux/amd64，方便部署到常见 Linux 主机。
# 调用方可以用 TARGET_GOOS / TARGET_GOARCH 覆盖。
TARGET_GOOS ?= linux
TARGET_GOARCH ?= amd64

# release 产物按目标平台分目录，避免不同平台二进制互相覆盖。
RELEASE_DIR ?= $(ROOT_DIR)/dist/release/$(TARGET_GOOS)-$(TARGET_GOARCH)

# 声明这些名字不是文件名，避免同名文件影响 make 的执行判断。
.PHONY: help check test vet staticcheck lint tests terraform-fmt buf-lint shellcheck web-check web-build build build-release proto clean

# 打印当前保留的工程入口。
# scripts/ 下的环境编排测试不在这里列为 make target。
help:
	@printf '%s\n' \
	  'mini-cloud make targets' \
	  '' \
	  '  make check          Run the full local quality gate.' \
	  '  make test           Run root Go tests.' \
	  '  make build          Build local development binaries.' \
	  '  make build-release  Build linux/amd64 release binaries by default.' \
	  '  make proto          Generate protobuf code with buf.' \
	  '  make clean          Remove local build output.' \
	  '' \
	  'Variables:' \
	  '  BINARY_DIR=dist/bin TARGET_GOOS=linux TARGET_GOARCH=amd64 RELEASE_DIR=dist/release/linux-amd64'

# 本地日常质量门禁。
# 这里保持线性、直接：能用工具原生命令完成的检查，就直接调用工具。
check:
	@echo "[check] gofmt"

	# gofmt 可以直接接收目录；这里检查源码目录和 tests 子模块入口。
	# gofmt -l 只打印未格式化文件，不会修改文件。
	unformatted="$$(gofmt -l ./cmd ./internal ./pkg ./tests)"
	if [[ -n "$$unformatted" ]]; then
	  echo "[check] gofmt found unformatted files:" >&2
	  printf '%s\n' "$$unformatted" >&2
	  exit 1
	fi

	# 根 module 的基础 Go 检查。
	$(MAKE) --no-print-directory test
	$(MAKE) --no-print-directory vet

	# 只做语法检查，不执行脚本。
	# deploy/ 和 scripts/ 都可能包含 shell 入口。
	echo "[check] bash -n deploy/*.sh scripts/*.sh"
	find ./deploy ./scripts -type f -name '*.sh' -exec bash -n {} +

	# 更严格的 Go 静态检查和 lint。
	$(MAKE) --no-print-directory staticcheck
	$(MAKE) --no-print-directory lint

	# tests 是独立 module，需要单独进入后测试。
	$(MAKE) --no-print-directory tests

	# 下面几项按工具是否安装决定是否执行。
	# 这让没有安装 terraform / buf / shellcheck / web 依赖的机器仍可运行基础检查。
	$(MAKE) --no-print-directory terraform-fmt
	$(MAKE) --no-print-directory buf-lint
	$(MAKE) --no-print-directory shellcheck
	$(MAKE) --no-print-directory web-check
	$(MAKE) --no-print-directory web-build

test:
	@echo "[check] go test ./..."
	go test ./...

# go vet 是 Go 官方提供的基础静态检查。
vet:
	@echo "[check] go vet ./..."
	go vet ./...

# staticcheck 比 go vet 更严格，需要本机已经安装 staticcheck。
staticcheck:
	@echo "[check] staticcheck ./..."
	staticcheck ./...

# golangci-lint 聚合项目级 lint 规则，需要本机已经安装 golangci-lint。
lint:
	@echo "[check] golangci-lint run ./..."
	golangci-lint run ./...

# tests 子目录是独立 Go module，必须在自己的目录下执行 go test。
tests:
	@echo "[check] (cd tests && go test ./...)"
	cd "$(TESTS_DIR)"
	go test ./...

# Terraform 文件只做格式检查，不自动改写。
# 没安装 terraform 时跳过，避免把基础 Go 检查和 IaC 工具安装强绑定。
terraform-fmt:
	@if command -v terraform >/dev/null 2>&1; then
	  echo "[check] terraform fmt -check -recursive deploy/terraform"
	  terraform fmt -check -recursive deploy/terraform
	else
	  echo "[check] skip terraform fmt: terraform is not installed"
	fi

# buf lint 检查 proto 定义。
# 没安装 buf 时跳过；真正生成代码由 `make proto` 负责。
buf-lint:
	@if command -v buf >/dev/null 2>&1; then
	  echo "[check] buf lint"
	  buf lint
	else
	  echo "[check] skip buf lint: buf is not installed"
	fi

# shellcheck 做 shell 脚本静态检查；bash -n 只保证语法层面没问题。
shellcheck:
	@if command -v shellcheck >/dev/null 2>&1; then
	  echo "[check] shellcheck"
	  find ./deploy ./scripts -type f -name '*.sh' -exec shellcheck {} +
	else
	  echo "[check] skip shellcheck: shellcheck is not installed"
	fi

# 前端目录存在时才跑前端检查。
web-check:
	@if [[ -d "$(WEB_DIR)" ]]; then
	  echo "[check] (cd web && npm run check)"
	  cd "$(WEB_DIR)"
	  npm run check
	fi

# 前端目录存在时才跑前端构建。
web-build:
	@if [[ -d "$(WEB_DIR)" ]]; then
	  echo "[check] (cd web && npm run build)"
	  cd "$(WEB_DIR)"
	  npm run build
	fi

# 构建本机开发用二进制。
# 不设置 GOOS/GOARCH，让 go build 使用当前机器默认平台。
build:
	@echo "[build] output: $(BINARY_DIR)"
	mkdir -p "$(BINARY_DIR)"
	go build -trimpath -o "$(BINARY_DIR)/control-plane" ./cmd/control-plane
	go build -trimpath -o "$(BINARY_DIR)/cloud-plane" ./cmd/cloud-plane
	go build -trimpath -o "$(BINARY_DIR)/node-agent" ./cmd/node-agent

# 构建发布用二进制。
# 默认目标是 linux/amd64；调用方可以覆盖 TARGET_GOOS / TARGET_GOARCH。
# 每次都会重新生成 SHA256SUMS，方便发布前校验产物。
build-release:
	@echo "[build-release] output: $(RELEASE_DIR)"
	mkdir -p "$(RELEASE_DIR)"
	rm -f "$(RELEASE_DIR)/SHA256SUMS"
	build_bin() {
	  local name="$$1"
	  local pkg="$$2"
	  local target="$(RELEASE_DIR)/$$name"
	  echo "[build-release] building $$name -> $$target"
	  CGO_ENABLED=0 GOOS="$(TARGET_GOOS)" GOARCH="$(TARGET_GOARCH)" \
	    go build -trimpath -ldflags='-s -w' -o "$$target" "$$pkg"
	}
	build_bin control-plane ./cmd/control-plane
	build_bin cloud-plane ./cmd/cloud-plane
	build_bin node-agent ./cmd/node-agent
	cd "$(RELEASE_DIR)"
	sha256sum control-plane cloud-plane node-agent >SHA256SUMS
	echo "[build-release] done"

# 生成 proto 代码。
# 这里只调用 buf generate，不在 Makefile 里手写 protoc 参数。
proto:
	@echo "[proto] buf generate"
	buf generate

# 清理本地构建产物。
# 只删除本项目的 dist 目录，不清理 Go cache 或其它全局缓存。
clean:
	@echo "[clean] remove dist"
	rm -rf "$(ROOT_DIR)/dist"
