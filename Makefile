GO ?= go

# ── 门禁工具：版本固定，装在 .bin/（本地缓存，不进仓库）────────────────────────
#
# 为什么固定版本：门禁必须可复现。今天过、明天因为工具升级而挂，等于门禁失效。
# 为什么装在本地：离线时门禁仍能跑；`go run pkg@version` 每次都依赖网络。
TOOLBIN         := $(CURDIR)/.bin
STATICCHECK_PKG := honnef.co/go/tools/cmd/staticcheck@v0.8.1
ERRCHECK_PKG    := github.com/kisielk/errcheck@v1.20.0


# 契约文件在 Make 层求值一次，recipe 里就不必再嵌命令替换。
PROTOS := $(shell find api -name '*.proto')

.PHONY: help generate build test vet fmt fmt-check staticcheck errcheck lint \
        archcheck trace leakcheck licensecheck license-ledger tools gate check run coverage clean \
        check-config replay smoke dev commit clean-check done

help: ## 显示本帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

# ── 生成 ─────────────────────────────────────────────────────────────────────
generate: ## 由 api/*.proto 生成 Go 代码（契约的唯一事实源，禁止手写客户端）
	protoc -I api \
		--go_out=. --go_opt=module=shen \
		--go-grpc_out=. --go-grpc_opt=module=shen \
		$(PROTOS)

# ── 构建 ─────────────────────────────────────────────────────────────────────
build: ## 编译全部
	$(GO) build ./...

# ── 静态检查与结构检查：注释里标明每条对应的文档出处 ─────────────────────────
fmt-check: ## 格式化检查，不改文件（依据 TB-15）
	@scripts/gate/check-fmt.sh

vet: ## go vet（依据 TB-15）
	$(GO) vet ./...

staticcheck: ## staticcheck，比 vet 更深（依据 TB-15；需先跑 make tools）
	@if [ ! -x $(TOOLBIN)/staticcheck ]; then echo "staticcheck 未安装。先跑 make tools。"; echo "禁止跳过本检查 —— 静默跳过等于假绿。"; exit 1; fi
	$(TOOLBIN)/staticcheck ./...

errcheck: ## 未处理的错误返回值（依据 TB-14；需先跑 make tools）
	@if [ ! -x $(TOOLBIN)/errcheck ]; then echo "errcheck 未安装。先跑 make tools。"; echo "禁止跳过本检查 —— 静默跳过等于假绿。"; exit 1; fi
	$(TOOLBIN)/errcheck ./...

archcheck: ## 架构与依赖方向：ST-1…ST-4 顶层目录与跨平面 import、MD-18…MD-20 store 唯一 I/O 出口与模块清单一致、TB-20/21/24 语言层数与 CGO
	$(GO) run ./scripts/archcheck

trace: ## 追溯检查：模块文档↔代码↔单测（MD-2/17/22）、规则 ID 引用存在性（D-3/D-8）、变更包与变更日志（DEV-1/2）
	$(GO) run ./scripts/tracecheck

leakcheck: ## 泄漏检查：字符串字面量 ↔ OH-1 禁用清单、响应头 ↔ OH-5（决策与分数不回传）、豁免逐条登记（OH-4）
	$(GO) run ./scripts/check-leak

licensecheck: ## 依赖许可审计，拦 AGPL / SSPL / BUSL 等（依据 TB-16）
	$(GO) run ./scripts/licensecheck

lint: fmt-check vet staticcheck errcheck archcheck trace leakcheck licensecheck ## 全部静态、结构与追溯检查

# ── 版本控制：把「提交」变成一轮收尾的一部分（不是可选项）────────────────────
#
# 为什么放进流程：没有提交就没有回退点。上一轮 `scripts/tracecheck` 被误删后无从恢复，
# 只能从会话记录里考古 —— 有版本控制的话那只是一条 `git checkout` 的事。
commit: ## 提交本轮改动（必须给 MSG="<一句话主题>"）
	@if [ -z "$(MSG)" ]; then echo '用法：make commit MSG="<一句话主题>"'; echo "MSG 是必需的：提交信息要让半年后的人看懂这轮干了什么。"; exit 1; fi
	@if [ -z "$$(git status --porcelain)" ]; then echo "没有可提交的改动（工作区已干净）。"; exit 1; fi
	git add -A
	git commit -q -m "$(MSG)" -m "验证：make gate 通过（关键输出见 docs/log.md 对应条目）"
	@echo "已提交：" && git --no-pager log --oneline -1

clean-check: ## 校验工作区干净（收尾的最后一道闸：改动必须已提交）
	@if [ -n "$$(git status --porcelain)" ]; then echo "工作区不干净 —— 这一轮的改动还没提交："; git status --short; echo '跑 make done MSG="…"（门禁 → 提交 → 校验）或 make commit MSG="…"。'; exit 1; fi
	@echo "工作区干净：本轮改动都已提交。"

done: gate commit clean-check ## 一轮的收尾：门禁 → 提交 → 校验工作区干净
	@echo
	@echo "本轮收尾完成：门禁绿 · 已提交 · 工作区干净。"

# ── 测试 ─────────────────────────────────────────────────────────────────────
test: ## 全部单测，含数据竞争检测（依据 TB-15）
	$(GO) test -race ./...

coverage: ## 单测覆盖率
	$(GO) test -cover ./...

# ── 门禁 ─────────────────────────────────────────────────────────────────────
gate: lint test ## CI 门禁：格式化 + 静态检查 + 架构检查 + 追溯检查 + 泄漏检查 + 许可审计 + 单测（含 -race）
	@echo
	@echo "门禁通过。"

check: build fmt-check vet archcheck trace leakcheck ## 快速内循环检查（不需要下载任何工具）
	@echo
	@echo "快速检查通过。"

tools: ## 预装门禁工具到 .bin/（离线环境先跑这个）
	@mkdir -p $(TOOLBIN)
	GOBIN=$(TOOLBIN) $(GO) install $(STATICCHECK_PKG)
	GOBIN=$(TOOLBIN) $(GO) install $(ERRCHECK_PKG)
	@echo "门禁工具已装到 $(TOOLBIN)/"

# ── 产物与台账 ───────────────────────────────────────────────────────────────
license-ledger: ## 重新生成依赖许可台账（依据 TB-16）
	$(GO) run ./scripts/licensecheck -ledger > docs/spec/dependencies.md
	@echo "已写入 docs/spec/dependencies.md"

# ── 运行与开发验证 ───────────────────────────────────────────────────────────
fmt: ## 格式化（会改写文件）
	$(GO) fmt ./...

run: SHEN_CONFIG ?= deploy/config/config.example.yaml
run: ## 本地起核心（影子模式；配置取 SHEN_CONFIG）
	SHEN_CONFIG=$(SHEN_CONFIG) $(GO) run ./core/cmd/core

check-config: SHEN_CONFIG ?= deploy/config/config.example.yaml
check-config: ## 配置干跑：装载 + 校验 + 打印策略摘要（不开端口）
	SHEN_CONFIG=$(SHEN_CONFIG) $(GO) run ./core/cmd/core -check-config

replay: ## 规则回放：打印「样本观测 → 分数 → 命中信号」（判定响应不回显分值，只能在这里看）
	$(GO) test -count=1 -run TestRuleReplay -v ./core/cmd/core

smoke: ## 在线冒烟：对已在跑的核心发判定请求（地址取 SHEN_DEV_ADDR）
	$(GO) run ./scripts/devcheck -addr "$${SHEN_DEV_ADDR:-127.0.0.1:19443}"

dev: ## 一键开发验证：配置干跑 → 起核心 → 在线冒烟 → 规则回放 → 关核心
	@scripts/dev/smoke.sh

clean: ## 清理构建缓存
	$(GO) clean ./...
