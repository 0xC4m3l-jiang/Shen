"""离线生成器：`python -m analysis.aicap --out <file> [...]`。

它做三件事，且**顺序不可换**：

1. **启动期断言**（`service.startup_assert`）：任务声明、护栏档案、提示词资源 ——
   任一不合格即退出（`AR-33` / `AR-24`）；
2. **逐条生成**：每个（资源 × 变体）走一次 `service.generate()` ——
   两道护栏都在里面，没过的**不进内容库**；
3. **产出清单**：把内容库聚合成核心可装载的 JSON
   （[`../../../docs/spec/ai-contract.md`](../../../docs/spec/ai-contract.md) §3）。

退出码：`0` = 至少产出一条内容并写出清单；`1` = **一条都没过护栏**（清单不值得写，此时说明
护栏配置或输入有问题）；`2` = 启动期断言失败（配置/资源坏了，根本没开始生成）。

用法：

    python -m analysis.aicap --out deploy/content/manifest.json \\
        --profile site-a --resources /,/api/users --variants 8 --version 1
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import argparse
import sys
from collections.abc import Sequence
from datetime import UTC, datetime
from pathlib import Path

from . import service
from .content import ContentStore, build_manifest, write_manifest
from .tasks._registry import kinds
from .tasks.content import GENERATOR


def _now_iso() -> str:
    return datetime.now(UTC).isoformat()


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="analysis.aicap", description="欺骗内容离线生成器")
    parser.add_argument("--out", required=True, help="清单输出路径（JSON）")
    parser.add_argument("--kind", default="content", help="任务种类（必须已登记）")
    parser.add_argument("--profile", default="site-a", help="文档画像标识")
    parser.add_argument(
        "--resources",
        default="/",
        help="资源路径列表（逗号分隔）；资源是请求路径的精确值",
    )
    parser.add_argument(
        "--variants", type=int, default=8, help="变体数 N（必须与 ai.content.variants 一致）"
    )
    parser.add_argument("--version", type=int, default=1, help="内容版本；轮换时递增")
    parser.add_argument("--now", default="", help="生成时刻（RFC 3339；留空用当前时间）")
    parser.add_argument(
        "--identifiers",
        default="",
        help="部署方注入的真实业务标识（逗号分隔；命中即被护栏拒绝，AR-22）",
    )
    parser.add_argument("--quiet", action="store_true", help="只打印汇总行")
    args = parser.parse_args(argv)

    try:
        service.startup_assert()
    except (AssertionError, RuntimeError) as exc:
        print(f"aicap: 启动期断言失败：{exc}", file=sys.stderr)
        return 2

    resources = [item.strip() for item in args.resources.split(",") if item.strip()]
    if not resources:
        print("aicap: --resources 不能为空", file=sys.stderr)
        return 2
    if args.variants < 1:
        print("aicap: --variants 必须 ≥ 1", file=sys.stderr)
        return 2
    if args.kind not in kinds():
        print(f"aicap: 未登记的任务种类 {args.kind!r}；已登记：{list(kinds())}", file=sys.stderr)
        return 2

    stamp = args.now or _now_iso()
    identifiers = tuple(item.strip() for item in args.identifiers.split(",") if item.strip())
    store = ContentStore()
    # `ContentStore` 显式实现 `Sink`（`aicap/ports.py`）：只有过了护栏的产物才会走到这里（`AR-33`）
    sink: service.Sink = store
    rejected: list[str] = []

    for resource in resources:
        for variant in range(args.variants):
            envelope = service.generate(
                service.TaskSpec(
                    kind=args.kind,
                    session_id=f"generate:{args.profile}:{resource}:{variant}",
                    deadline_s=30.0,
                    payload={
                        "resource": resource,
                        "variant": variant,
                        "version": args.version,
                        "profile_id": args.profile,
                    },
                ),
                sink=sink,
                identifiers=identifiers,
                generated_at=stamp,
            )
            if not envelope.accepted:
                rejected.append(f"{resource}#{variant}: {envelope.rejected_reason}")
            elif not args.quiet:
                print(f"  ✓ {resource}#{variant} {service.message_of(envelope)}")

    if not len(store):
        print("aicap: 没有任何内容通过护栏 —— 不写清单（护栏或输入有问题）", file=sys.stderr)
        for reason in rejected:
            print(f"  ✗ {reason}", file=sys.stderr)
        return 1

    manifest = build_manifest(
        store.entries(),
        version=args.version,
        variants=args.variants,
        generated_at=stamp,
        generator=GENERATOR,
    )
    path = Path(args.out)
    size = write_manifest(path, manifest)
    entries = len(manifest["entries"])
    print(
        f"aicap: 生成 {len(store)} 条 / 护栏拒绝 {len(rejected)} 条 · "
        f"清单 v{args.version} variants={args.variants} entries={entries} "
        f"bytes={size} → {path}"
    )
    for reason in rejected:
        print(f"  ✗ {reason}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
