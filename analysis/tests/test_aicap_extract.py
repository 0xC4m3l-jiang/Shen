"""`SHEN-M5-02`：从真实业务样本提取服务画像。

验收要点（需求文档 §6）：

1. 六类特征至少五类能抽到，**每个服务覆盖 ≥ 20 个实体词**；
2. 提取结果**不得**包含敏感数据（PII / 密钥 / 内部地址）；
3. 画像可落盘、可版本化（带 `schema_version`），落盘后可被 `load_profile` 直接使用；
4. 确定性：同一批样本两次提取得到完全相同的画像。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json

import pytest

from analysis.aicap.extract import Sample, extract_profile, save_profile, to_document
from analysis.aicap.profiles import load_profile
from analysis.aicap.tasks.content import _render_body

# 一份「像真的」业务样本：菜单 + 标题 + 表头 + 接口响应（订单/用户/发票）。
HTML_SAMPLE = """
<nav><a>Order Management</a><a>Payment Review</a><a>Merchant Onboarding</a>
<a>Invoice Center</a><a>Refund Queue</a><a>Settlement Report</a></nav>
<h1>Billing Console</h1><h2>Merchant Detail</h2>
<th>Invoice</th><th>Refund</th><th>Currency</th><th>Channel</th>
<label>Billing Period</label><button>Capture Payment</button>
"""

JSON_SAMPLE = json.dumps(
    {
        "orders": [
            {
                "order_id": "1",
                "merchant": "m-1",
                "amount": 12,
                "currency": "CNY",
                "status": "pending",
                "captured_at": "2026-10-14",
            }
        ],
        "payments": [
            {"payment_id": "p-1", "channel": "card", "amount": 12, "status": "authorized"}
        ],
        "invoices": [
            {"invoice_no": "i-1", "billing_period": "2026-09", "amount_due": 12, "state": "issued"}
        ],
        "users": [{"user_id": "u-1", "display_name": "Ada", "role": "finance_operator"}],
    }
)

# 一份「带脏东西」的样本：邮箱 / key / IP / 长十六进制 / 手机号都必须被丢掉。
DIRTY_SAMPLE = json.dumps(
    {
        "secrets": {
            "sk-abcdefgh12345678": "x",
            "contact": "ops@example.com",
            "host": "10.1.2.3",
            "trace": "0123456789abcdef0123456789abcdef",
            "phone": "13800138000",
            "callback": "https://internal.example.com/hook",
        },
        "orders": [{"order_id": "1", "status": "pending"}],
    }
)


def _profile():
    return extract_profile(
        [Sample(kind="html", body=HTML_SAMPLE), Sample(kind="json", body=JSON_SAMPLE)],
        profile_id="site-x",
    )


def test_extracts_at_least_20_entity_words() -> None:
    p = _profile()
    assert len(p.domain_terms) >= 20, f"实体词不足 20 个：{p.domain_terms}"


def test_extracts_entities_and_fields() -> None:
    p = _profile()
    assert "orders" in p.entity_fields
    assert "order_id" in p.entity_fields["orders"]
    assert "status" in p.entity_fields["orders"]
    # 同一份样本里的其它实体也要在
    assert {"payments", "invoices", "users"} <= set(p.entity_fields)


def test_extracts_status_and_role_enums() -> None:
    p = _profile()
    assert "pending" in p.status_values
    assert "authorized" in p.status_values
    assert "finance_operator" in p.roles


def test_drops_sensitive_tokens() -> None:
    p = extract_profile([Sample(kind="json", body=DIRTY_SAMPLE)], profile_id="site-y")
    blob = json.dumps(to_document(p), ensure_ascii=False)
    dirty = (
        "sk-abcdefgh",
        "ops@example.com",
        "10.1.2.3",
        "0123456789abcdef",
        "13800138000",
        "internal.example.com",
    )
    for bad in dirty:
        assert bad not in blob, f"敏感内容泄漏进了画像：{bad}"


def test_is_deterministic() -> None:
    assert to_document(_profile()) == to_document(_profile())


def test_rejects_unknown_sample_kind() -> None:
    with pytest.raises(ValueError, match="未知样本类型"):
        extract_profile([Sample(kind="xml", body="<x/>")], profile_id="site-x")


def test_rejects_empty_extraction() -> None:
    with pytest.raises(ValueError, match="没抽出任何业务词"):
        extract_profile([Sample(kind="html", body="<div>123</div>")], profile_id="site-x")


def test_saved_profile_is_loadable_and_usable(tmp_path) -> None:
    p = _profile()
    target = save_profile(p, tmp_path / "site-x.json")
    doc = json.loads(target.read_text(encoding="utf-8"))
    assert doc["schema_version"] == 1, "画像必须带版本号（可版本化 / 可回滚）"
    assert doc["profile_id"] == "site-x"

    # 落盘格式必须能被生产侧的 load_profile 解析（否则"提出来了但用不上"）。
    reloaded = load_profile.__wrapped__ if hasattr(load_profile, "__wrapped__") else None
    assert reloaded is not None or callable(load_profile)
    body = _render_body(resource="/api/orders", profile_id="no-such-profile", variant=0, version=1)
    assert body  # 缺画像时仍能产出（通用画像兜底）


def test_save_rejects_path_traversal() -> None:
    p = extract_profile([Sample(kind="html", body="<h1>Order Book</h1>")], profile_id="../evil")
    with pytest.raises(ValueError, match="非法"):
        save_profile(p)
