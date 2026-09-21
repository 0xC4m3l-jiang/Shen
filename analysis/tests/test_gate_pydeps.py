"""门禁脚本 `scripts/gate/check-pydeps.sh`：锁文件 ↔ venv 一致性检查本身要能被验证。

为什么用 fixture 目录而不是改真 venv：真 venv 是共享环境，改它会让别的检查一起失真；
而这道检查的价值恰恰是「**环境与锁文件不一致时它必须红**」——
那就必须能在一个**故意不一致**的沙盒里跑它。

做法：把脚本原样拷进一个临时仓库骨架（脚本从自身位置推算仓库根），
再给一个「假的 python」——它只负责打印发行版清单，不碰真环境。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import shutil
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "gate" / "check-pydeps.sh"


def _sandbox(tmp_path: Path, *, lock: str, installed: str, dev_lock: str = "") -> Path:
    """搭一个最小仓库骨架：`scripts/gate/` + `analysis/requirements*.txt` + 假 venv。"""
    (tmp_path / "scripts" / "gate").mkdir(parents=True)
    target = tmp_path / "scripts" / "gate" / "check-pydeps.sh"
    shutil.copy2(SCRIPT, target)

    analysis = tmp_path / "analysis"
    analysis.mkdir()
    (analysis / "requirements.txt").write_text(lock, encoding="utf-8")
    (analysis / "requirements-dev.txt").write_text(dev_lock, encoding="utf-8")

    fake_python = analysis / ".venv" / "bin" / "python"
    fake_python.parent.mkdir(parents=True)
    fake_python.write_text(f"#!/bin/sh\ncat <<'EOF'\n{installed}\nEOF\n", encoding="utf-8")
    fake_python.chmod(0o755)
    return target


def _run(script: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [str(script)],
        capture_output=True,
        text=True,
        check=False,
    )


def test_consistent_lock_passes(tmp_path: Path) -> None:
    script = _sandbox(
        tmp_path,
        lock="grpcio==1.84.0\nprotobuf==7.36.2\n",
        dev_lock="ruff==0.16.8\n",
        installed="grpcio=1.84.0\nprotobuf=7.36.2\nruff=0.16.8",
    )
    result = _run(script)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "一致" in result.stdout


def test_version_mismatch_fails(tmp_path: Path) -> None:
    """锁文件说 7.36.2、环境里是 7.35.1 ⇒ 必须失败并指向 make pyenv（本仓库真的撞到过）。"""
    script = _sandbox(
        tmp_path,
        lock="protobuf==7.36.2\n",
        installed="protobuf=7.35.1",
    )
    result = _run(script)
    assert result.returncode != 0
    assert "7.36.2" in result.stdout and "7.35.1" in result.stdout
    assert "make pyenv" in result.stdout


def test_missing_install_fails(tmp_path: Path) -> None:
    script = _sandbox(tmp_path, lock="pyyaml==6.0.3\n", installed="protobuf=7.36.2")
    result = _run(script)
    assert result.returncode != 0
    assert "没有装" in result.stdout


def test_non_pinned_line_fails(tmp_path: Path) -> None:
    """范围约束（`>=`）不是锁 —— 锁必须可复算，否则审的不是同一份东西。"""
    script = _sandbox(tmp_path, lock="protobuf>=7.36\n", installed="protobuf=7.36.2")
    result = _run(script)
    assert result.returncode != 0
    assert "非严格定版" in result.stdout


def test_name_normalization_matches_pip(tmp_path: Path) -> None:
    """`PyYAML` 与 `pyyaml`、`grpcio_tools` 与 `grpcio-tools` 是同一个发行版（PEP 503）。"""
    script = _sandbox(
        tmp_path,
        lock="PyYAML==6.0.3\ngrpcio_tools==1.84.0\n",
        installed="pyyaml=6.0.3\ngrpcio-tools=1.84.0",
    )
    result = _run(script)
    assert result.returncode == 0, result.stdout + result.stderr


def test_missing_venv_fails_loudly(tmp_path: Path) -> None:
    """缺 venv 必须**失败**，不能静默跳过（静默跳过等于假绿）。"""
    (tmp_path / "scripts" / "gate").mkdir(parents=True)
    target = tmp_path / "scripts" / "gate" / "check-pydeps.sh"
    shutil.copy2(SCRIPT, target)
    (tmp_path / "analysis").mkdir()
    (tmp_path / "analysis" / "requirements.txt").write_text("protobuf==7.36.2\n", encoding="utf-8")
    result = _run(target)
    assert result.returncode != 0
    assert "make pyenv" in result.stdout


def test_real_repository_passes() -> None:
    """真实仓库此刻是绿的 —— 这条把「检查本身坏了」与「环境真的不一致」区分开。"""
    if not (REPO_ROOT / "analysis" / ".venv" / "bin" / "python").exists():
        pytest.skip("没有 analysis/.venv（先跑 make pyenv）—— 本机没建环境时跳过，不算通过")
    result = _run(SCRIPT)
    assert result.returncode == 0, result.stdout + result.stderr
