"""从**真实业务样本**里提取服务画像（`SHEN-M5-02`）。

为什么需要它：差异化不能靠人工为每个服务手写画像 —— 接入服务一多就不可维护。
本模块从采样到的页面与接口响应里抽出六类特征，产出可审阅、可版本化的画像 JSON。

| 类别 | 从哪里取 | 落到画像的哪个字段 |
| --- | --- | --- |
| 业务域名词 | 页面里的菜单/标题/按钮/表头 | `domain_terms` |
| 实体与字段 | JSON 响应的键（按父键归组成实体） | `entity_fields` |
| 状态枚举 | 值形如 `pending` / `in_review` 的短词 | `status_values` |
| 角色与权限 | 键名含 role/permission 的值 | `roles` |
| 技术栈线索 · 语言风格 | 命名风格（snake/camel）、中英混排 | 记进 `source`（供人工审阅） |

**红线**：提取结果**不得**包含真实敏感数据。凡形似密钥、邮箱、IP、长十六进制、
长数字串的 token 一律丢弃 —— 画像会进配置仓库、进日志、进审计，漏一次就等于泄露一次。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import argparse
import html
import json
import re
import sys
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path

from .profiles import GENERIC_FIELDS, PROFILE_DIR, ServiceProfile, valid_profile_id

# 上限：画像要能被人读完，也要能进配置仓库。
MAX_TERMS = 40
MAX_FIELDS_PER_ENTITY = 12
MAX_ENTITIES = 24
MAX_STATUS_VALUES = 16
MAX_ROLES = 12

# 词长边界：太短的是噪声（a/of），太长的是哈希或句子残片。
MIN_WORD = 3
MAX_WORD = 32

# 敏感形态：命中即丢（宁缺勿滥 —— 画像会进仓库与日志）。
_SENSITIVE_PATTERNS = (
    re.compile(r"^sk-[A-Za-z0-9]{8,}"),  # API key
    re.compile(r"^[0-9a-fA-F]{16,}$"),  # 长十六进制（哈希/密钥）
    re.compile(r"^[A-Za-z0-9+/]{20,}={0,2}$"),  # base64 片段
    re.compile(r"^[^@\s]+@[^@\s]+\.[A-Za-z]{2,}$"),  # 邮箱
    re.compile(r"^\d{1,3}(\.\d{1,3}){3}$"),  # IPv4
    re.compile(r"^\d{11,}$"),  # 手机号 / 长编号
    re.compile(r"://"),  # URL
)

# 状态枚举的样子：全小写 ASCII 词，允许下划线（pending / in_review）。
_STATUS_RE = re.compile(r"^[a-z][a-z_]{2,23}$")

# 角色键：命中这些键名的值才当角色（避免把普通字符串当角色）。
_ROLE_KEYS = ("role", "roles", "permission", "permissions", "scope", "scopes")

# HTML 里值得取文本的标签（菜单、标题、表头、按钮、标签）。
_HTML_TEXT_RE = re.compile(
    r"<(nav|h1|h2|h3|label|th|button|option|a)\b[^>]*>(.*?)</\1\s*>",
    re.IGNORECASE | re.DOTALL,
)
_TAG_RE = re.compile(r"<[^>]+>")
_WORD_RE = re.compile(rf"[A-Za-z][A-Za-z_\-]{{{MIN_WORD - 1},{MAX_WORD - 1}}}")
_CJK_RE = re.compile(r"[\u4e00-\u9fff]{2,8}")


@dataclass(frozen=True)
class Sample:
    """一条采样：`kind` 是 `html` 或 `json`。"""

    kind: str
    body: str


def _looks_sensitive(token: str) -> bool:
    return any(p.search(token) for p in _SENSITIVE_PATTERNS)


def _clean_words(text: str) -> list[str]:
    """把一段文本切成候选词（英文词 + 中文词块），并丢掉敏感形态。"""
    out: list[str] = []
    for m in _WORD_RE.finditer(text):
        w = m.group(0)
        if not _looks_sensitive(w):
            out.append(w)
    for m in _CJK_RE.finditer(text):
        w = m.group(0)
        if not _looks_sensitive(w):
            out.append(w)
    return out


def _terms_from_html(body: str) -> list[str]:
    """取页面里「用户真看得见」的词：菜单/标题/表头/按钮/链接文本。"""
    out: list[str] = []
    for _, inner in _HTML_TEXT_RE.findall(body):
        text = html.unescape(_TAG_RE.sub(" ", inner))
        out.extend(_clean_words(text))
    return out


def _walk_json(
    node: object,
    entity: str,
    fields: dict[str, list[str]],
    out: dict[str, list[str]],
) -> None:
    """递归走 JSON：键进「实体字段」，短小写字符串进状态/角色候选。"""
    if isinstance(node, Mapping):
        for raw_key, value in node.items():
            key = str(raw_key)
            low = key.lower()
            known = {f.lower() for f in fields.get(entity, [])}
            if (
                not _looks_sensitive(key)
                and 1 < len(key) <= MAX_WORD
                and low not in known
                and len(fields.setdefault(entity, [])) < MAX_FIELDS_PER_ENTITY
            ):
                fields[entity].append(key)
            if key.lower() in _ROLE_KEYS:
                for v in _scalar_values(value):
                    if isinstance(v, str) and _STATUS_RE.match(v) and not _looks_sensitive(v):
                        out.setdefault("roles", []).append(v)
                continue
            if key.lower() in ("status", "state", "visibility", "stage", "phase", "moderation"):
                for v in _scalar_values(value):
                    if isinstance(v, str) and _STATUS_RE.match(v) and not _looks_sensitive(v):
                        out.setdefault("status_values", []).append(v)
            # 数组的第一项当作该实体的样例对象（`{"orders": [{...}]}` ⇒ 实体 orders）
            child = value
            if isinstance(child, list) and child:
                child = child[0]
            if isinstance(child, Mapping):
                _walk_json(child, key, fields, out)


def _scalar_values(node: object) -> list[object]:
    if isinstance(node, list):
        return [x for x in node if not isinstance(x, (Mapping, list))]
    if isinstance(node, Mapping):
        return []
    return [node]


def _dedupe_cap(items: Iterable[str], cap: int) -> tuple[str, ...]:
    seen: set[str] = set()
    out: list[str] = []
    for raw in items:
        v = raw.strip()
        if not v:
            continue
        key = v.lower()
        if key in seen:
            continue
        seen.add(key)
        out.append(v)
    return tuple(out[:cap])


def extract_profile(
    samples: Iterable[Sample], *, profile_id: str, source_note: str = ""
) -> ServiceProfile:
    """从样本提取画像（确定性：同一批样本两次调用得到**完全相同**的结果）。

    只做「抽取 + 过滤 + 排序」，**不猜业务语义**：抽不到就留空字段（由生成侧兜底），
    绝不按路径名瞎编业务含义。
    """
    pid = (profile_id or "").strip()
    if not pid:
        raise ValueError("profile_id 不得为空")

    terms: list[str] = []
    fields: dict[str, list[str]] = {}
    extra: dict[str, list[str]] = {}

    for s in samples:
        kind = (s.kind or "").strip().lower()
        if kind == "html":
            terms.extend(_terms_from_html(s.body))
        elif kind == "json":
            try:
                doc = json.loads(s.body)
            except json.JSONDecodeError as exc:
                raise ValueError(f"样本不是合法 JSON：{exc}") from exc
            entity = "root"
            _walk_json(doc, entity, fields, extra)
        else:
            raise ValueError(f"未知样本类型 {s.kind!r}（只支持 html / json）")

    # 实体字段里没有的键，也算业务词（菜单名与字段名往往同源）。
    for names in fields.values():
        terms.extend(names)

    domain_terms = _dedupe_cap(terms, MAX_TERMS)
    if not domain_terms:
        raise ValueError("样本里没抽出任何业务词 —— 换一份更完整的样本，或改用人工画像")

    entity_fields: dict[str, tuple[str, ...]] = {}
    for entity in sorted(fields)[:MAX_ENTITIES]:
        entity_fields[entity] = _dedupe_cap(fields[entity], MAX_FIELDS_PER_ENTITY)
    if not entity_fields:
        entity_fields["*"] = GENERIC_FIELDS

    return ServiceProfile(
        profile_id=pid,
        domain_terms=domain_terms,
        entity_fields=entity_fields,
        status_values=_dedupe_cap(extra.get("status_values", []), MAX_STATUS_VALUES),
        roles=_dedupe_cap(extra.get("roles", []), MAX_ROLES),
        source="extracted" + (f":{source_note}" if source_note else ""),
    )


def to_document(profile: ServiceProfile) -> dict[str, object]:
    """把画像转成可落盘的 JSON（带 `schema_version`，供人工审阅与版本化）。"""
    return {
        "schema_version": 1,
        "profile_id": profile.profile_id,
        "domain_terms": list(profile.domain_terms),
        "entity_fields": {k: list(v) for k, v in profile.entity_fields.items()},
        "status_values": list(profile.status_values),
        "roles": list(profile.roles),
        "source": profile.source,
    }


def save_profile(profile: ServiceProfile, path: Path | None = None) -> Path:
    """写进画像目录（默认 `resources/profiles/<id>.json`）；返回落盘路径。

    写在画像目录里 ⇒ 下一轮 `load_profile(id)` 直接生效，无需改代码。
    """
    if path is None and not valid_profile_id(profile.profile_id):
        # 画像 id 会被拼进路径：读侧已经校验，写侧同样要校验（否则等于把任意写交给调用方）。
        raise ValueError(f"画像 id 非法，不能落盘：{profile.profile_id!r}")
    target = path or (PROFILE_DIR / f"{profile.profile_id}.json")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(
        json.dumps(to_document(profile), ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    return target


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="从业务样本提取服务画像（SHEN-M5-02）")
    parser.add_argument("--profile", required=True, help="画像 id（落盘文件名）")
    parser.add_argument("--html", action="append", default=[], help="采样页面文件（可多次）")
    parser.add_argument(
        "--json",
        dest="json_files",
        action="append",
        default=[],
        help="采样响应文件（可多次）",
    )
    parser.add_argument("--out", help="落盘路径（默认写进资源目录）")
    parser.add_argument("--print", dest="print_only", action="store_true", help="只打印，不落盘")
    args = parser.parse_args(argv)

    samples: list[Sample] = []
    for path in args.html:
        samples.append(Sample(kind="html", body=Path(path).read_text(encoding="utf-8")))
    for path in args.json_files:
        samples.append(Sample(kind="json", body=Path(path).read_text(encoding="utf-8")))
    if not samples:
        print("至少给一个 --html 或 --json 样本", file=sys.stderr)
        return 2

    try:
        profile = extract_profile(samples, profile_id=args.profile)
    except ValueError as exc:
        print(f"提取失败：{exc}", file=sys.stderr)
        return 1

    doc = to_document(profile)
    if args.print_only:
        print(json.dumps(doc, ensure_ascii=False, indent=2))
        return 0
    target = save_profile(profile, Path(args.out) if args.out else None)
    counts = (
        f"术语 {len(profile.domain_terms)} · 实体 {len(profile.entity_fields)} · "
        f"状态 {len(profile.status_values)} · 角色 {len(profile.roles)}"
    )
    print(f"画像 {profile.profile_id}：{counts} → {target}")
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
