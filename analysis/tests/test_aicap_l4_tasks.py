"""L4 三个 `kind`（`intent` / `chain` / `strategy`）走唯一出口 + 护栏 + worker 接线。

覆盖四件事：

1. **登记与启动期断言**（`AR-33`）：三个 kind 在注册表里，缺提示词模板就起不来；
2. **闭集拒绝**（`AR-15`）：越界类别被拒而不是回落成某个默认类别；
3. **护栏真的承重**（`AR-22` / `AR-33`）：泄露类与风格不一致都被拒；
4. **worker 的「模型优先、失败回落」**（`NI-1` / `AR-12`）：模型失败不影响整轮，`AR-12` 仍强制。

**全部不打网络**：模型用注入的替身（`ScriptedClient`）。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from pathlib import Path

import pytest

from analysis.aicap import service
from analysis.aicap.guardrail import prompts as guardrail_prompts
from analysis.aicap.tasks import _registry
from analysis.aicap.tasks.chain import CHAIN_TASK
from analysis.aicap.tasks.intent import INTENT_TASK
from analysis.aicap.tasks.strategy import STRATEGY_TASK, StrategyBoundError
from analysis.llm.client import Unavailable
from analysis.llm.schemas import CHAIN_KIND, INTENT_KIND, STRATEGY_KIND
from analysis.telemetry import InMemoryTelemetry, WireEvent
from analysis.worker import MODEL_GENERATOR, RULES_GENERATOR, run_once

PROMPT_DIR = Path(guardrail_prompts.DEFAULT_DIR)

# ── 模型替身：公开成员只有 `complete`（`AR-32` 的成员名检查会对它生效）──────────────


@dataclass
class ScriptedClient:
    """按提示词给固定应答的替身；`fail` 非空时每次调用都抛它。"""

    reply: str | Callable[[str], str] = "{}"
    fail: Exception | None = None
    prompts: list[str] | None = None

    def complete(self, prompt: str, *, session_id: str, timeout: float) -> str:
        del session_id, timeout
        if self.prompts is not None:
            self.prompts.append(prompt)
        if self.fail is not None:
            raise self.fail
        return self.reply(prompt) if callable(self.reply) else self.reply


def by_kind(replies: Mapping[str, str]) -> Callable[[str], str]:
    """按提示词数据区里的 `kind` 选应答（数据区是排序后的 JSON，键值稳定）。"""

    def respond(prompt: str) -> str:
        for kind, text in replies.items():
            if f'"kind": "{kind}"' in prompt:
                return text
        raise Unavailable(f"替身没为这段提示词准备应答（已有 {sorted(replies)}）")

    return respond


INTENT_OK = json.dumps(
    {
        "category": "reconnaissance",
        "confidence": 0.5,
        "evidence_ids": ["e-1"],
        "rationale": "命中侦察类规则（reconnaissance）",
    }
)


def strategy_ok(gray: int = 5, block: float = 0.6) -> str:
    return json.dumps(
        {
            "decoy_selection": ["fake-gitlab"],
            "gray_pct": gray,
            "threshold_suggestions": {"route_mirage": 0.3, "block": block},
            "rationale": "灰度 5%，阈值 route_mirage=0.3 / block=0.6",
        }
    )


def chain_ok(evidence: list[str] | None = None) -> str:
    return json.dumps(
        {
            "stages": [
                {"name": "reconnaissance", "evidence_ids": evidence or ["e-1"], "confidence": 0.4}
            ],
            "broken_decoy_signals": [],
            "rationale": "阶段 reconnaissance 有证据，其余阶段缺失",
        }
    )


def spec(kind: str, payload: Mapping[str, object] | None = None) -> service.TaskSpec:
    return service.TaskSpec(
        kind=kind, session_id="s-1", deadline_s=5.0, payload=dict(payload or {"observations": []})
    )


# ── ① 登记与启动期断言（AR-33 / AR-24）────────────────────────────────────────


def test_registry_lists_all_four_kinds() -> None:
    assert _registry.kinds() == ("chain", "content", "intent", "strategy")
    for task in (INTENT_TASK, CHAIN_TASK, STRATEGY_TASK):
        task.assert_declared()  # 声明不全 ⇒ 这里就炸，而不是运行期才发现
        assert task.requires_model is True, "三个 L4 任务的产出依赖模型（--llm 才走）"


def test_startup_assert_loads_four_templates() -> None:
    assert service.startup_assert() == ("chain", "content", "intent", "strategy")


def test_startup_assert_fails_when_a_new_template_is_missing(tmp_path: Path) -> None:
    """缺 intent 模板 ⇒ **启动失败**（不是运行到一半才发现，`AR-24`）。"""
    (tmp_path / "content.md").write_text(
        (PROMPT_DIR / "content.md").read_text(encoding="utf-8"), encoding="utf-8"
    )
    with pytest.raises(guardrail_prompts.PromptError):
        _registry.assert_startup(prompt_dir=tmp_path)


def test_every_kind_points_at_its_own_template() -> None:
    """护栏档案引用的模板必须真的加载得到 —— 逐个 kind 核（不是只看启动断言的整体通过）。"""
    loaded = guardrail_prompts.load()
    for kind, task in (
        (INTENT_KIND, INTENT_TASK),
        (CHAIN_KIND, CHAIN_TASK),
        (STRATEGY_KIND, STRATEGY_TASK),
    ):
        assert task.guardrail_profile.prompt in loaded, kind


# ── ② 闭集拒绝（AR-15）───────────────────────────────────────────────────────


def test_intent_out_of_set_category_is_rejected_not_defaulted() -> None:
    """越界类别**必须拒绝**：曾经它被静默回落成 `reconnaissance`（那是默认值）。"""
    reply = json.dumps(
        {
            "category": "第六类",
            "confidence": 0.5,
            "evidence_ids": ["e-1"],
            "rationale": "侦察类（reconnaissance）",
        }
    )
    envelope = service.generate(spec(INTENT_KIND), client=ScriptedClient(reply))
    assert not envelope.accepted
    assert "闭集" in str(envelope.rejected_reason), envelope.rejected_reason
    assert envelope.data == {}, "被拒时不得回退成某个默认类别（AR-15）"


def test_intent_accepts_an_in_set_category() -> None:
    envelope = service.generate(spec(INTENT_KIND), client=ScriptedClient(INTENT_OK))
    assert envelope.accepted, envelope.rejected_reason
    assert envelope.data["category"] == "reconnaissance"


# ── ③ 护栏真的承重（AR-22 / AR-33）───────────────────────────────────────────


def test_guardrail_rejects_leaked_private_address() -> None:
    """泄露类：理由里回显了内网地址 ⇒ 拒绝（`AR-22`）。"""
    reply = json.dumps(
        {
            "category": "reconnaissance",
            "confidence": 0.5,
            "evidence_ids": ["e-1"],
            "rationale": "观察到内网 10.1.2.3 的侦察行为（reconnaissance）",
        }
    )
    envelope = service.generate(spec(INTENT_KIND), client=ScriptedClient(reply))
    assert not envelope.accepted
    assert "泄露" in str(envelope.rejected_reason) or "leak" in str(envelope.rejected_reason)


def test_guardrail_rejects_off_profile_rationale() -> None:
    """风格一致性：理由里一个画像术语都没有 ⇒ 判为「不像该画像」（`AR-33`）。"""
    reply = json.dumps(
        {
            "category": "reconnaissance",
            "confidence": 0.5,
            "evidence_ids": ["e-1"],
            "rationale": "看起来有人在扫描我们的服务",
        }
    )
    envelope = service.generate(spec(INTENT_KIND), client=ScriptedClient(reply))
    assert not envelope.accepted
    assert "画像" in str(envelope.rejected_reason)


def test_all_three_templates_ban_echoing_raw_attacker_fields() -> None:
    """三条禁令必须**写在提示词里**（机器识别这三类字段的能力今天不存在，见变更包 §7）。

    为什么还要断言文本：这条禁令一旦在模板里被删掉，模型层就完全没人拦了，
    而后置护栏只认黑名单三类 —— 删掉它不会有任何别的检查变红。
    """
    for name in (INTENT_KIND, CHAIN_KIND, STRATEGY_KIND):
        text = (PROMPT_DIR / f"{name}.md").read_text(encoding="utf-8")
        assert "回显原始 URI 查询串" in text, name


def test_strategy_bounds_are_enforced_on_the_model_path() -> None:
    """`INT-11` 的边界对模型路径同样生效 —— 越界即拒绝建产物，**不夹紧、不改写**。"""
    with pytest.raises(StrategyBoundError):
        service.generate(
            spec(STRATEGY_KIND, {"available_decoys": ["fake-gitlab"], "observations": []}),
            client=ScriptedClient(strategy_ok(gray=100)),
        )
    with pytest.raises(StrategyBoundError):
        service.generate(
            spec(STRATEGY_KIND, {"available_decoys": ["fake-gitlab"], "observations": []}),
            client=ScriptedClient(strategy_ok(block=0.1)),
        )
    ok = service.generate(
        spec(STRATEGY_KIND, {"available_decoys": ["fake-gitlab"], "observations": []}),
        client=ScriptedClient(strategy_ok()),
    )
    assert ok.accepted, ok.rejected_reason


# ── ④ worker：模型优先、失败回落（NI-1 / AR-12）──────────────────────────────


def decision(event_id: str, path: str, *, backend: str = "fake-gitlab") -> WireEvent:
    payload = {
        "event_id": event_id,
        "at": "2026-09-19T10:00:00+08:00",
        "source": "203.0.113.9",
        "session_id": "s-1",
        "method": "GET",
        "path": path,
        "user_agent": "sqlmap/1.7",
        "action": "route_origin",
        "signals": ["path-probe"],
        "severity": "none",
        "backend": backend,
    }
    return WireEvent(
        event_id=event_id,
        event_type="decision",
        session_id="s-1",
        payload=json.dumps(payload).encode("utf-8"),
        created_at=payload["at"],
    )


def test_worker_without_client_is_unchanged_and_not_a_fallback() -> None:
    """`client=None`（默认）= 确定性三步，且**不算回落**（`--llm` 关闭时的回归基线）。"""
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    run = run_once(port, now="2026-09-19T10:00:30+08:00")
    assert run.rejected == 0 and run.model_rejected == {}
    assert run.intent_generator == run.chain_generator == run.strategy_generator == RULES_GENERATOR
    assert all(item["generator"] == RULES_GENERATOR for item in run.conclusions)


def test_worker_uses_model_when_the_model_path_works() -> None:
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    client = ScriptedClient(
        by_kind({INTENT_KIND: INTENT_OK, CHAIN_KIND: chain_ok(), STRATEGY_KIND: strategy_ok()})
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00", client=client)
    assert run.rejected == 0, run.model_rejected
    assert run.intent_generator == MODEL_GENERATOR
    assert run.chain_generator == MODEL_GENERATOR
    assert run.strategy_generator == MODEL_GENERATOR
    assert [item["generator"] for item in run.conclusions] == [MODEL_GENERATOR] * 2
    assert all(item["model_rejected"] is None for item in run.conclusions)
    assert run.errors == [], "模型成功时不该有任何错误记录"


def test_worker_falls_back_on_every_step_but_still_concludes() -> None:
    """模型三条路全失败（缺 key 的等价物）⇒ 三步回落确定性版，整轮**仍然成功**（`NI-1`）。"""
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    client = ScriptedClient(fail=Unavailable("分析用模型未配置"))
    run = run_once(port, now="2026-09-19T10:00:30+08:00", client=client)
    assert run.rejected == 3, f"三步都应回落：{run.model_rejected}"
    assert set(run.model_rejected) == {INTENT_KIND, CHAIN_KIND, STRATEGY_KIND}
    assert all("Unavailable" in reason for reason in run.model_rejected.values())
    assert run.errors == [], "回落是设计内的降级，不是「本轮没做成」"
    assert run.reported == 2 and len(run.conclusions) == 2
    assert all(item["generator"] == RULES_GENERATOR for item in run.conclusions)
    assert all(item["model_rejected"] for item in run.conclusions), "结论里必须能看出模型为什么没成"


def test_worker_voids_model_intent_when_evidence_does_not_exist() -> None:
    """`AR-12` 对**意图**模型路径同样强制（独立评审 P1 发现）。

    为什么容易漏：结论里有两处证据引用 —— 顶层的「本轮参与分析的事件」（永远真）与
    `data` 里「模型引用的证据」（模型自由填）。只校验前者，就等于让结论携带**编造的证据 ID**，
    而读结论的人（控制台 / 审计）无法分辨。
    """
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    fake = json.dumps(
        {
            "category": "reconnaissance",
            "confidence": 0.5,
            "evidence_ids": ["根本没这条证据"],
            "rationale": "侦察类（reconnaissance）",
        }
    )
    client = ScriptedClient(
        by_kind({INTENT_KIND: fake, CHAIN_KIND: chain_ok(), STRATEGY_KIND: strategy_ok()})
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00", client=client)
    assert run.intent_generator == RULES_GENERATOR, "引用不存在的证据必须作废并回落"
    assert "AR-12" in run.model_rejected[INTENT_KIND], run.model_rejected
    assert run.intent is not None and run.intent.accepted
    assert run.intent.data["evidence_ids"] == ["e-1"], "回落后的证据引用必须是真的"
    assert run.chain_generator == MODEL_GENERATOR, "不得连带拖垮其他步"


def test_worker_accepts_model_intent_when_evidence_exists() -> None:
    """正对照：引用**真实存在**的证据时必须放行 —— 否则上一例可能只是「什么都拦」。"""
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    client = ScriptedClient(
        by_kind({INTENT_KIND: INTENT_OK, CHAIN_KIND: chain_ok(), STRATEGY_KIND: strategy_ok()})
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00", client=client)
    assert run.intent_generator == MODEL_GENERATOR, run.model_rejected
    assert run.intent is not None and run.intent.data["evidence_ids"] == ["e-1"]


def test_worker_voids_model_chain_when_evidence_does_not_exist() -> None:
    """`AR-12` 在模型路径上仍强制：引用了不存在的证据 ⇒ 整条链作废、回落确定性版。"""
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    client = ScriptedClient(
        by_kind(
            {
                INTENT_KIND: INTENT_OK,
                CHAIN_KIND: chain_ok(["根本没这条证据"]),
                STRATEGY_KIND: strategy_ok(),
            }
        )
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00", client=client)
    assert run.chain_generator == RULES_GENERATOR, "链必须作废并回落确定性版"
    assert CHAIN_KIND in run.model_rejected
    assert "AR-12" in run.model_rejected[CHAIN_KIND], run.model_rejected
    assert run.chain is not None and run.chain.stages, "回落后的链仍应成形"
    assert run.intent_generator == MODEL_GENERATOR, "链作废不得连带拖垮其他步"


def test_worker_falls_back_on_bound_violation_from_the_model() -> None:
    """模型给出越界的策略数值 ⇒ 该步回落（`INT-11` 不因「模型说的」而放宽）。"""
    port = InMemoryTelemetry([decision("e-1", "/.git/config")])
    client = ScriptedClient(
        by_kind(
            {
                INTENT_KIND: INTENT_OK,
                CHAIN_KIND: chain_ok(),
                STRATEGY_KIND: strategy_ok(gray=100),
            }
        )
    )
    run = run_once(port, now="2026-09-19T10:00:30+08:00", client=client)
    assert run.strategy_generator == RULES_GENERATOR
    assert "INT-11" in run.model_rejected[STRATEGY_KIND]
    assert run.intent_generator == MODEL_GENERATOR and run.chain_generator == MODEL_GENERATOR
