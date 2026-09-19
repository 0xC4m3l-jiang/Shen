"""生成 L4 需要的 gRPC 桩，并把生成物变成**普通包**。

为什么需要这个脚本，而不是直接调 `grpc_tools.protoc`：

1. 生成的代码用**绝对导入**（`from telemetry.v1 import telemetry_pb2`），
   这要求把桩目录塞进 `sys.path`；
2. 而顶层名 `telemetry` 会与本层的 `analysis/telemetry.py`（遥测端口）**撞名** —— 一旦撞名，
   `import telemetry.v1` 会解析到那个模块并报 `'telemetry' is not a package`。

所以生成后做两件事：补 `__init__.py` 让它成为普通包；
把导入前缀改写成 `analysis.proto.telemetry.v1`。
这样 `import analysis.proto.telemetry.v1.telemetry_pb2_grpc` 稳定可用，且**不依赖 sys.path 技巧**。

用法（`make pygen` 会调用）：

    python analysis/tools/genproto.py
"""

from __future__ import annotations

import os
import pathlib
import re
import sys

REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
PROTO_ROOT = REPO_ROOT / "api"
OUT_DIR = REPO_ROOT / "analysis" / "proto"
PROTO_FILES = ("api/telemetry/v1/telemetry.proto",)

# 生成物里的绝对导入 → 包内绝对导入（保持"绝对"是为了可读性与工具兼容）
PREFIX = "analysis.proto."
IMPORT_RE = re.compile(r"^(from|import)\s+telemetry\.v1\b", re.MULTILINE)


def main() -> int:
    from grpc_tools import protoc  # 延迟导入：只在真正生成时才需要 grpcio-tools

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    # protoc 要求「-I 路径」是「文件路径」的前缀，绝对与相对混用会被拒；
    # 所以统一切到仓库根，用相对路径（与手工执行 `protoc -I api ...` 等价）。
    os.chdir(REPO_ROOT)
    # `-m grpc_tools.protoc` 会自动带上 bundled 的 well-known types；直接调 protoc.main() 不会，
    # 于是 `import "google/protobuf/timestamp.proto"` 会找不到 —— 这里显式补上。
    bundled_include = pathlib.Path(protoc.__file__).resolve().parent / "_proto"
    args = [
        "protoc",
        f"-I{bundled_include}",
        f"-I{PROTO_ROOT.relative_to(REPO_ROOT)}",
        f"--python_out={OUT_DIR.relative_to(REPO_ROOT)}",
        f"--grpc_python_out={OUT_DIR.relative_to(REPO_ROOT)}",
        *PROTO_FILES,
    ]
    code = protoc.main(args)
    if code != 0:
        print(f"protoc 失败（退出码 {code}）", file=sys.stderr)
        return code

    for directory in [OUT_DIR, *[p for p in OUT_DIR.rglob("*") if p.is_dir()]]:
        init = directory / "__init__.py"
        if not init.exists():
            init.write_text(
                '"""由 `make pygen` 生成，请勿手改（契约唯一事实源是 api/ 下的 .proto）。"""\n',
                encoding="utf-8",
            )

    rewritten = 0
    for path in OUT_DIR.rglob("*.py"):
        text = path.read_text(encoding="utf-8")
        updated = IMPORT_RE.sub(lambda m: f"{m.group(1)} {PREFIX}telemetry.v1", text)
        if updated != text:
            path.write_text(updated, encoding="utf-8")
            rewritten += 1

    print(f"✓ gRPC 桩已生成：analysis/proto（改写导入 {rewritten} 个文件）")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
