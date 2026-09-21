#!/usr/bin/env python3
"""把控制台 DAG 的**原始 JSON** 渲染成 Mermaid 图（报告与人工检查共用）。

为什么要单独一个渲染器，而不是在报告里手绘图：

1. **手绘的图会漂**——它看着像证据，其实是插图；代码一改，图还停在旧样子。
   这里渲染的每个节点、每条边、每个计数都来自 `ai-inject-check.py --dag-out` 落盘的 JSON，
   别人可以自己重跑、自己重画、也可以直接对着 JSON 核数字。
2. **两幅图回答两个不同的问题**：
   - **拓扑图**（`topology.json`）回答「这一轮流量**整体**走了哪几条路、各多少条」；
   - **单请求链路图**（`graphs.json`）回答「**这一条请求**每一步发生了什么、为什么」。

用法：

    python3 scripts/dev/render-dag.py --dag-dir /tmp/dag-final            # 全部阶段
    python3 scripts/dev/render-dag.py --dag-dir /tmp/dag-final --stage 2-inject

只用标准库。输出是 Markdown（可直接贴进报告）。
"""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Mapping, Sequence
from pathlib import Path

# 边按种类换线型：一眼能看出「这条边是改道、拦截还是普通放行」
EDGE_ARROW = {"mirage": "-.->", "block": "==>", "normal": "-->"}


def load_json(path: Path) -> object:
    """读一个 JSON 文件（**不限定顶层类型**：topology 是对象、graphs 是数组）。"""
    if not path.exists():
        raise SystemExit(f"缺文件：{path}（先用 --dag-out 跑一次 ai-inject-check.py）")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        # 坏文件要报「哪个文件坏」，而不是抛一串 traceback（这是给人跑的工具）
        raise SystemExit(f"读不了 {path}：{exc}") from exc


def load(path: Path) -> Mapping[str, object]:
    """读一个**顶层是对象**的 JSON（topology.json）。"""
    data = load_json(path)
    if not isinstance(data, Mapping):
        raise SystemExit(f"形状不合预期（顶层不是对象）：{path}")
    return data


def node_id(raw: str) -> str:
    """Mermaid 的节点 id 不能带 `:` 与 `.`（会当语法）——统一换成 `_`。"""
    return "".join(ch if ch.isalnum() or ch == "_" else "_" for ch in str(raw))


def esc(text: object) -> str:
    """Mermaid 标签里的引号会截断字符串；`<br/>` 是我们自己插的换行，保留。"""
    return str(text).replace('"', "'").replace("\n", " ")


def render_topology(data: Mapping[str, object]) -> str:
    nodes = data.get("nodes") or []
    edges = data.get("edges") or []
    if not isinstance(nodes, list) or not isinstance(edges, list):
        raise SystemExit("topology.json 的 nodes/edges 不是列表")
    lines = ["```mermaid", "flowchart LR"]
    for item in nodes:
        if not isinstance(item, Mapping):
            continue
        ident = node_id(str(item.get("id", "")))
        label = esc(item.get("label", item.get("id", "")))
        count = item.get("count", 0)
        lines.append(f'  {ident}["{label}<br/>{count} 次"]')
    for item in edges:
        if not isinstance(item, Mapping):
            continue
        arrow = EDGE_ARROW.get(str(item.get("kind", "normal")), "-->")
        src, dst = node_id(str(item.get("from", ""))), node_id(str(item.get("to", "")))
        kind = item.get("kind", "normal")
        lines.append(f'  {src} {arrow}|"{item.get("count", 0)} 次 · {esc(kind)}"| {dst}')
    lines.append("```")
    totals = data.get("totals") or {}
    if isinstance(totals, Mapping) and totals:
        pairs = " · ".join(f"{k}={v}" for k, v in sorted(totals.items()))
        lines.append(f"\n> 合计：{pairs}（`shadow={data.get('shadow')}`）")
    return "\n".join(lines)


def pick_chain(chains: Sequence[object]) -> Mapping[str, object] | None:
    """挑一条「最能说明本阶段」的请求：注入 > 改道 > 拦截 > 其他。"""
    ranked: list[tuple[int, Mapping[str, object]]] = []
    for item in chains:
        if not isinstance(item, Mapping):
            continue
        rank = 0
        if item.get("inject") == "applied":
            rank = 4
        elif item.get("executed") == "mirage":
            rank = 3
        elif item.get("executed") == "block":
            rank = 2
        elif item.get("action") == "route_origin":
            rank = 1
        ranked.append((rank, item))
    if not ranked:
        return None
    ranked.sort(key=lambda pair: pair[0], reverse=True)
    return ranked[0][1]


def render_chain(item: Mapping[str, object]) -> str:
    chain = item.get("chain") or []
    if not isinstance(chain, list) or not chain:
        raise SystemExit("graphs.json 里这条请求没有 chain")
    lines = ["```mermaid", "flowchart TD"]
    for idx, node in enumerate(chain):
        if not isinstance(node, Mapping):
            continue
        label = esc(node.get("label", ""))
        value = esc(node.get("value", ""))
        lines.append(f'  n{idx}["{label}<br/><small>{value}</small>"]')
        if idx:
            lines.append(f"  n{idx - 1} --> n{idx}")
    # 注入那一跳单独上色：报告里要能一眼指出「AI 内容是在这里进去的」
    inject_idx = next(
        (i for i, n in enumerate(chain) if isinstance(n, Mapping) and n.get("id") == "inject"),
        None,
    )
    if inject_idx is not None:
        lines.append(f"  class n{inject_idx} inject")
        lines.append("  classDef inject fill:#ffe8b3,stroke:#b8860b,stroke-width:2px")
    lines.append("```")
    head = (
        f"\n> 代表请求：`{item.get('method')} {item.get('path')}` · "
        f"action=**{item.get('action')}** · executed=**{item.get('executed')}** · "
        f"inject=**{item.get('inject')}** · score={item.get('score')} · "
        f"backend={item.get('backend') or '—'} · content_id={item.get('content_id') or '—'}"
    )
    rows = ["\n| 步骤 | 这一跳是什么 | 请求 | 响应 | 为什么 |", "| --- | --- | --- | --- | --- |"]
    for idx, node in enumerate(chain):
        if not isinstance(node, Mapping):
            continue
        rows.append(
            f"| {idx + 1} | {esc(node.get('label', ''))} | {esc(node.get('request', ''))} "
            f"| {esc(node.get('response', ''))} | {esc(node.get('why', ''))} |"
        )
    return head + "\n" + "\n".join(lines) + "\n" + "\n".join(rows)


def render_stage(stage_dir: Path) -> str:
    out = [f"### 阶段：`{stage_dir.name}`\n", "**① 拓扑（这一阶段流量整体走了哪几条路）**\n"]
    out.append(render_topology(load(stage_dir / "topology.json")))
    chains = load_json(stage_dir / "graphs.json")
    if not isinstance(chains, list):
        raise SystemExit(f"graphs.json 顶层不是列表：{stage_dir}")
    picked = pick_chain(chains)
    out.append("\n**② 单请求链路（每一步发生了什么）**\n")
    if picked is None:
        out.append("> 这一段没有请求链路（空阶段）")
    else:
        out.append(render_chain(picked))
    return "\n".join(out)


def main(argv: Sequence[str]) -> int:
    parser = argparse.ArgumentParser(prog="render-dag", description="DAG JSON → Mermaid")
    parser.add_argument("--dag-dir", required=True, help="ai-inject-check.py --dag-out 的目录")
    parser.add_argument("--stage", default="", help="只渲染某一个阶段目录（默认全部）")
    args = parser.parse_args(argv[1:])

    root = Path(args.dag_dir)
    stages = [root / args.stage] if args.stage else sorted(p for p in root.iterdir() if p.is_dir())
    if not stages:
        raise SystemExit(f"目录里没有阶段子目录：{root}")
    print("# DAG 图示（由真实 JSON 渲染）\n")
    for stage in stages:
        print(render_stage(stage))
        print()
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
