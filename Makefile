GO ?= go

# shellcheck disable=SC1089,SC2276,SC2157   # 见下：本文件是 Makefile，不是 shell；这三条都是同一个误读的副产品
# 本文件是 **Makefile**，不是 shell 脚本。有工具会把整份文件当 shell 分析并在第一个 recipe 行报
# 「parsing stopped here」——那是工具误用。make 自身已能正常解析并执行本文件的全部目标
# （`make gate` / `make dev` 均在 CI 与本地实跑通过）。

# ── 门禁工具：版本固定，装在 scripts/bin/（本地缓存，不进仓库）────────────────────────
#
# 为什么固定版本：门禁必须可复现。今天过、明天因为工具升级而挂，等于门禁失效。
# 为什么装在本地：离线时门禁仍能跑；`go run pkg@version` 每次都依赖网络。
TOOLBIN         := $(CURDIR)/scripts/bin
STATICCHECK_PKG := honnef.co/go/tools/cmd/staticcheck@v0.8.1
ERRCHECK_PKG    := github.com/kisielk/errcheck@v1.20.0


# 契约文件在 Make 层求值一次，recipe 里就不必再嵌命令替换。
PROTOS := $(shell find common/api -name '*.proto')

.PHONY: help generate build test vet fmt fmt-check staticcheck errcheck lint \
        archcheck trace leakcheck licensecheck license-ledger tools gate check run coverage clean \
        check-config replay smoke dev ai-check commit clean-check done fp-capture fp-diff bench caddy-surface console demo \
        pyenv pyfmt-check pylint pytest pygen check-pydeps analysis analysis-llm \
        docker-build up down docker-ps docker-logs docker-log check-ignore \
        start status app-smoke traffic verify

# check-ignore 必须用 --no-index：默认行为下 git 认为已入库文件不受忽略规则影响，
# 于是检查会永远通过 —— 那是**假的防线**（本仓库真的踩过：见 docs/log.md）。
check-ignore: ## 自检：已入库文件不得被 .gitignore 规则命中（防「源码被静默排除」）
	@bad="`git ls-files | git check-ignore --no-index --stdin`"; \
	if [ -n "$$bad" ]; then \
		echo "已入库文件却被 .gitignore 忽略（源码可能被静默排除）："; echo "$$bad"; exit 1; \
	fi
	@echo "✓ 忽略清单未误伤任何已入库文件"

start: ## 起全套并等就绪（= scripts/shen.sh up；只需 Docker）
	@scripts/shen.sh up

status: ## 看容器状态 + 控制台概览
	@scripts/shen.sh status

doctor: ## 接入自检（INT-17 五项：body 可读 / TLS 方式 / 会话粘性 / 实境与幻境 / 是否在路径上）
	@scripts/shen.sh doctor $(ARGS)

app-smoke: ## 快速链路验证：经引擎造三条流量并回显判定
	@scripts/shen.sh smoke

traffic: ## 发伪造流量并从观测面核对判定（完整验证；场景见 scripts/traffic/scenarios.json）
	@scripts/shen.sh traffic $(ARGS)

verify: ## 仓库级验证：make gate + make dev
	@scripts/shen.sh check

# ── Docker：一键起全套（本机只需 Docker，不用装 Go / Node / Python）──────────
COMPOSE := docker compose -f deploy/docker/compose.yaml

docker-build: ## 构建全部镜像（core / proxy / console / analysis / business）
	$(COMPOSE) build

up: ## 一键起全套（Docker）：核心 + 代理 + 控制台 + L4 + 演示业务站
	$(COMPOSE) up -d --build
	@echo
	@echo "控制台  http://127.0.0.1:$${SHEN_CONSOLE_PORT:-19444}/   业务入口  http://127.0.0.1:$${SHEN_HTTP_PORT:-18080}/"

down: ## 停掉全套并删容器（镜像保留）
	$(COMPOSE) down

docker-ps: ## 看全套容器状态
	$(COMPOSE) ps

docker-logs: ## 跟看日志（会一直挂着，Ctrl-C 退出；S=core 只看某个服务）
	$(COMPOSE) logs -f $(S)

# ⚠️ 给脚本/自动化用**不跟随**版本：`docker logs -f` 不会自己返回，
#    在非交互场景（含 AI 助手）里会把命令挂住 —— 本仓库真的踩过。取最近 50 行后立即退出。
docker-log: ## 只看日志尾部（不跟随；S=core 可选，N=行数默认 50）
	$(COMPOSE) logs --tail=$${N:-50} $(S)

help: ## 显示本帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

# ── 生成 ─────────────────────────────────────────────────────────────────────
generate: ## 由 common/api/*.proto 生成 Go 代码（契约的唯一事实源，禁止手写客户端）
	protoc -I common/api \
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

# ── L4（Python）工具链与门禁 ────────────────────────────────────────────────
# 依据 docs/design/language.md TB-15：CI 必须含 Python 的 ruff 检查。
#
# **本层的环境与配置全部在 `analysis/` 内**（pyproject.toml / requirements*.txt / .venv）——
# Python 是 L4 的实现选择（TB-2 / TB-20），它的私有工具不该占用仓库根。
# 所以下面这些目标一律在 `analysis/` 里跑（pyproject 自动生效），不污染系统 Python。
ANALYSIS := analysis
PY      := $(ANALYSIS)/.venv/bin/python
PY_VENV := $(ANALYSIS)/.venv

pygen: ## 生成 L4 的 Python gRPC 桩（契约唯一事实源仍是 common/api/ 下的 .proto，ST-6）
	$(require_pyenv)
	@$(PY) $(ANALYSIS)/tools/genproto.py

analysis: ## 跑一轮 L4 近线分析（读核心遥测 → 上报结论事件；需核心已在跑）
	$(require_pyenv)
	@$(PY) -m analysis.worker --core $${SHEN_CORE_ADDR:-127.0.0.1:9443} --once

analysis-llm: ## 同 analysis，但启用模型路径（--llm；需 SHEN_AI_KEY，未设则回落确定性）
	$(require_pyenv)
	@$(PY) -m analysis.worker --core $${SHEN_CORE_ADDR:-127.0.0.1:9443} --once --llm

pyenv: ## 建/更新 L4 的 analysis/.venv 并安装锁定依赖（首次或改锁文件后跑）
	@test -d $(PY_VENV) || python3 -m venv $(PY_VENV)
	@$(PY_VENV)/bin/pip install -q --upgrade pip
	@$(PY_VENV)/bin/pip install -q -r $(ANALYSIS)/requirements-dev.txt -r $(ANALYSIS)/requirements.txt
	@cd $(ANALYSIS) && .venv/bin/pip install -q -e .
	@$(CURDIR)/scripts/gate/check-pydeps.sh
	@echo "L4 环境就绪（$(PY_VENV)）：`$(PY) --version`、ruff `$(PY_VENV)/bin/ruff --version | cut -d' ' -f2`"

define require_pyenv
	@test -x $(PY) || { echo "缺 Python 环境：先跑 make pyenv（TB-15 要求 Python 过 ruff）"; exit 1; }
endef

pyfmt-check: ## Python 代码风格（ruff format --check）：L4 + scripts/
	$(require_pyenv)
	@cd $(ANALYSIS) && .venv/bin/ruff format --check . ../scripts
	@echo "✓ Python 格式（ruff format）"

pylint: ## Python 静态检查（ruff check）：L4 + scripts/（含 TB-14 的裸 except 禁令）
	$(require_pyenv)
	@cd $(ANALYSIS) && .venv/bin/ruff check . ../scripts
	@echo "✓ Python 静态检查（ruff）"

pytest: ## L4 单测（pytest）
	$(require_pyenv)
	@cd $(ANALYSIS) && .venv/bin/pytest
	@echo "✓ L4 单测（pytest）"

check-pydeps: ## 锁文件 ↔ venv 一致性（依据 TB-16；不一致即失败，指向 make pyenv）
	@$(CURDIR)/scripts/gate/check-pydeps.sh

lint: fmt-check vet staticcheck errcheck pyfmt-check check-pydeps pylint check-ignore archcheck trace leakcheck licensecheck ## 全部静态、结构与追溯检查

# ── 版本控制：把「提交」变成一轮收尾的一部分（不是可选项）────────────────────
#
# 为什么放进流程：没有提交就没有回退点。上一轮 `scripts/tracecheck` 被误删后无从恢复，
# 只能从会话记录里考古 —— 有版本控制的话那只是一条 `git checkout` 的事。
commit: ## 提交本轮改动（必须给 MSG="<一句话主题>"）
	@if [ -z "$(MSG)" ]; then echo '用法：make commit MSG="<一句话主题>"'; echo "MSG 是必需的：提交信息要让半年后的人看懂这轮干了什么。"; exit 1; fi
	@if [ -z "`git status --porcelain`" ]; then echo "没有可提交的改动（工作区已干净）。"; exit 1; fi
	git add -A
	git commit -q -m "$(MSG)" -m "验证：make gate 通过（关键输出见 docs/log.md 对应条目）"
	@echo "已提交：" && git --no-pager log --oneline -1

clean-check: ## 校验工作区干净（收尾的最后一道闸：改动必须已提交）
	@if [ -n "`git status --porcelain`" ]; then echo "工作区不干净 —— 这一轮的改动还没提交："; git status --short; echo '跑 make done MSG="…"（门禁 → 提交 → 校验）或 make commit MSG="…"。'; exit 1; fi
	@echo "工作区干净：本轮改动都已提交。"

done: gate commit clean-check ## 一轮的收尾：门禁 → 提交 → 校验工作区干净
	@echo
	@echo "本轮收尾完成：门禁绿 · 已提交 · 工作区干净。"

# ── 实验工具：TLS 指纹采集与对比（E2）────────────────────────────────────────
# 为什么单独给目标：E2 是 P0 实验（结论可能推翻欺骗命题），必须能一条命令复现。
fp-capture: ## TLS 指纹采集（E2）：make fp-capture ADDR=host:443 [SNI=name] [OUT=x.json]
	@if [ -z "$(ADDR)" ]; then echo '用法：make fp-capture ADDR=host:443 [SNI=name] [OUT=x.json]'; exit 1; fi
	$(GO) run ./scripts/fingerprint -mode capture -addr "$(ADDR)" $(if $(SNI),-sni "$(SNI)",) $(if $(OUT),-out "$(OUT)",)

fp-diff: ## TLS 指纹对比（E2）：make fp-diff A=real.json B=ours.json
	@if [ -z "$(A)" ] || [ -z "$(B)" ]; then echo '用法：make fp-diff A=real.json B=ours.json'; exit 1; fi
	$(GO) run ./scripts/fingerprint -mode diff -a "$(A)" -b "$(B)"

# ── 观测台（人工测试用）──────────────────────────────────────────────────────
console: ## 只起观测控制台（核心需已在跑；地址取 SHEN_CORE_ADDR）
	$(GO) run ./modules/console/cmd/console

demo: ## 一键起「能看」的本地环境：核心 + 假业务 + 代理 + 控制台（人工测试入口）
	@scripts/demo/run.sh

# ── Caddy 耦合面（升级时先看它）──────────────────────────────────────────────
# 为什么做成目标而不是写在文档里：文档会腐烂，这里的结果**永远来自代码**。
# 升级流程：make caddy-surface → 对着 Caddy 的 changelog 核这些 API → make gate（兼容锁在 tests 里）
caddy-surface: ## 列出我们对 Caddy 的 API 依赖（升级 Caddy 前先看它）
	@echo "我们对 Caddy 的依赖面（自动从代码提取）："
	@echo
	@echo "  · 引用的包："
	@grep -rhoE 'github.com/caddyserver/caddy/v2[a-z/]*' --include='*.go' modules/deception/ common/core/ | sort -u | sed 's/^/      /'
	@echo
	@echo "  · 用到的导出符号："
	@grep -rhoE '\b(caddy|caddyhttp|caddytls|caddyconfig|reverseproxy)\.[A-Z][A-Za-z]*' --include='*.go' modules/deception/ common/core/ | sort -u | sed 's/^/      /'
	@echo
	@echo "  · 以字符串引用的模块 ID（改了就启动失败或静默失效）："
	@grep -rhoE '"http\.(handlers|error_handlers)\.[a-z_]+"' --include='*.go' modules/deception/ common/core/ | sort -u | sed 's/^/      /'

# ── 基准：AR-29 的空载下界（直连 vs 经引擎）────────────────────────────────
# 它只给下界；AR-29 的 P99 ≤ 5ms 要在接入演练里按真实载荷实测（实验 E3）。
bench: ## 基准：业务路径额外延迟的空载下界（AR-29 的输入之一）
	$(GO) test ./modules/deception/proxy/ -run '^$$' -bench BenchmarkAddedLatency -benchtime 500x -count=1

# ── 测试 ─────────────────────────────────────────────────────────────────────
test: pytest ## 全部单测，含数据竞争检测（依据 TB-15）
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

tools: ## 预装门禁工具到 scripts/bin/（离线环境先跑这个）
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
	SHEN_CONFIG=$(SHEN_CONFIG) $(GO) run ./common/core/cmd/core

check-config: SHEN_CONFIG ?= deploy/config/config.example.yaml
check-config: ## 配置干跑：装载 + 校验 + 打印策略摘要（不开端口）
	SHEN_CONFIG=$(SHEN_CONFIG) $(GO) run ./common/core/cmd/core -check-config

replay: ## 规则回放：打印「样本观测 → 分数 → 命中信号」（判定响应不回显分值，只能在这里看）
	$(GO) test -count=1 -run TestRuleReplay -v ./common/core/cmd/core

smoke: ## 在线冒烟：对已在跑的核心发判定请求（地址取 SHEN_DEV_ADDR）
	$(GO) run ./scripts/devcheck -addr "$${SHEN_DEV_ADDR:-127.0.0.1:19443}"

dev: ## 一键开发验证：配置干跑 → 起核心 → 在线冒烟 → 规则回放 → 关核心
	@scripts/dev/smoke.sh

ai-check: ## AI 欺骗内容注入端到端验收（六项：关闭态字节一致 / 打开态注入 / AR-30 / 关卡 / 秒级关闭 / DAG 注入跳）
	@python3 scripts/dev/ai-inject-check.py

clean: ## 清理构建缓存
	$(GO) clean ./...
