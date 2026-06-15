# mini-cloud 的 Makefile 是项目的统一入口：
# - 代码格式、静态检查、单元测试
# - 二进制构建、release 构建、control-plane 镜像构建
# - 真实云部署、更新、e2e 和回收
# - proto 生成、清理构建产物

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

IMAGE ?= mini-cloud/control-plane:local
IMAGE_CONFIG ?= deploy/container/control-plane.yaml.example
IMAGE_PLATFORM ?= linux/amd64
CONFIG ?= deploy/ops/config.yaml
MINICTL := $(BINARY_DIR)/minictl

# 声明这些名字不是文件名，避免同名文件影响 make 的执行判断。
.PHONY: help check test vet staticcheck lint terraform-fmt buf-lint shellcheck web-check web-build build .release-binaries release image proto .preflight deploy update e2e destroy clean

help:
	@printf '%s\n' \
	  'mini-cloud make targets' \
	  '' \
	  '  make check          Run the full local quality gate.' \
	  '  make test           Run root Go tests.' \
	  '  make build          Build all local Go binaries.' \
	  '  make release        Build deployable linux/amd64 binaries and Web UI.' \
	  '  make image          Build the control-plane container image.' \
	  '  make deploy         Build, bootstrap, and install real-cloud resources.' \
	  '  make update         Update the serverless control-plane only.' \
	  '  make e2e            Run the full real-cloud Web UI e2e flow.' \
	  '  make destroy        Destroy real-cloud resources.' \
	  '  make proto          Generate protobuf code with buf.' \
	  '  make clean          Remove local build output.' \
	  '' \
	  'Variables:' \
	  '  BINARY_DIR=dist/bin TARGET_GOOS=linux TARGET_GOARCH=amd64 RELEASE_DIR=dist/release/linux-amd64' \
	  '  IMAGE=mini-cloud/control-plane:local IMAGE_CONFIG=deploy/container/control-plane.yaml.example' \
	  '  IMAGE_PLATFORM=linux/amd64 CONFIG=deploy/ops/config.yaml'

# 本地日常质量门禁。
# 这里保持线性、直接：能用工具原生命令完成的检查，就直接调用工具。
check:
	@echo "[check] gofmt"

	# gofmt 可以直接接收目录；这里只检查当前根 module 的源码目录。
	# gofmt -l 只打印未格式化文件，不会修改文件。
	unformatted="$$(gofmt -l ./cmd ./internal)"
	if [[ -n "$$unformatted" ]]; then
	  echo "[check] gofmt found unformatted files:" >&2
	  printf '%s\n' "$$unformatted" >&2
	  exit 1
	fi

	# 根 module 的基础 Go 检查。
	$(MAKE) --no-print-directory test
	$(MAKE) --no-print-directory vet

	# 只做语法检查，不执行脚本。
	echo "[check] bash -n deploy/*.sh"
	find ./deploy -type f -name '*.sh' -exec bash -n {} +

	# 更严格的 Go 静态检查和 lint。
	$(MAKE) --no-print-directory staticcheck
	$(MAKE) --no-print-directory lint

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
	@if command -v staticcheck >/dev/null 2>&1; then
	  echo "[check] staticcheck ./..."
	  staticcheck ./...
	else
	  echo "[check] skip staticcheck: staticcheck is not installed"
	fi

# golangci-lint 聚合项目级 lint 规则，需要本机已经安装 golangci-lint。
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then
	  echo "[check] golangci-lint run ./..."
	  golangci-lint run ./...
	else
	  echo "[check] skip golangci-lint: golangci-lint is not installed"
	fi

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
	  find ./deploy -type f -name '*.sh' -exec shellcheck {} +
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
	go build -trimpath -o "$(MINICTL)" ./cmd/minictl

# 构建发布用二进制。
# 默认目标是 linux/amd64；调用方可以覆盖 TARGET_GOOS / TARGET_GOARCH。
# 每次都会重新生成 SHA256SUMS，方便发布前校验产物。
.release-binaries:
	@echo "[release] output: $(RELEASE_DIR)"
	mkdir -p "$(RELEASE_DIR)"
	rm -f "$(RELEASE_DIR)/SHA256SUMS"
	build_bin() {
	  local name="$$1"
	  local pkg="$$2"
	  local target="$(RELEASE_DIR)/$$name"
	  echo "[release] building $$name -> $$target"
	  CGO_ENABLED=0 GOOS="$(TARGET_GOOS)" GOARCH="$(TARGET_GOARCH)" \
	    go build -trimpath -ldflags='-s -w' -o "$$target" "$$pkg"
	}
	build_bin control-plane ./cmd/control-plane
	build_bin cloud-plane ./cmd/cloud-plane
	build_bin node-agent ./cmd/node-agent
	cd "$(RELEASE_DIR)"
	sha256sum control-plane cloud-plane node-agent >SHA256SUMS
	echo "[release] done"

release: TARGET_GOOS := linux
release: TARGET_GOARCH := amd64
release: .release-binaries web-build

image: TARGET_GOOS := linux
image: TARGET_GOARCH := amd64
image: release
	@echo "[image] control-plane -> $(IMAGE)"
	test -f "$(IMAGE_CONFIG)"
	docker build \
	  --platform "$(IMAGE_PLATFORM)" \
	  --provenance=false \
	  -f deploy/container/control-plane.Dockerfile \
	  --build-arg CONTROL_PLANE_CONFIG="$(IMAGE_CONFIG)" \
	  -t "$(IMAGE)" \
	  .

# 生成 proto 代码。
# 这里只调用 buf generate，不在 Makefile 里手写 protoc 参数。
proto:
	@echo "[proto] buf generate"
	buf generate

.preflight: build
	"$(MINICTL)" check --config "$(CONFIG)"

deploy: .preflight
	"$(MINICTL)" deploy --config "$(CONFIG)"

update: build
	"$(MINICTL)" update --config "$(CONFIG)"

e2e: .preflight
	"$(MINICTL)" e2e --config "$(CONFIG)"

destroy: build
	"$(MINICTL)" destroy --config "$(CONFIG)"

# 清理本地构建产物。
# 只删除本项目的 dist 目录，不清理 Go cache 或其它全局缓存。
clean:
	@echo "[clean] remove dist"
	rm -rf "$(ROOT_DIR)/dist"
