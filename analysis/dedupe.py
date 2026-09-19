"""态势去重（`AR-14`）。

触发 L4 分析**必须**经过态势去重（`AR-14`）。

禁止随事件写入量线性触发：否则 LLM 调用量会随事件量线性增长。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class SituationDedupe:
    """按 `(来源, 会话, 手法)` 在时间窗内去重；窗口外的再次出现算新态势。"""

    window_seconds: float = 60.0
    capacity: int = 4096
    _seen: dict[str, float] = field(default_factory=dict)
    admitted: int = 0
    suppressed: int = 0

    def key(self, *, source: str, session_id: str, method: str, path: str) -> str:
        return f"{source}|{session_id}|{method}|{path}"

    def admit(self, key: str, *, at: float) -> bool:
        """返回 True 表示**应当**触发 L4（该态势在窗口内首次出现）。"""
        if self.capacity <= 0:
            raise ValueError("capacity 必须为正（AR-14）")
        last = self._seen.get(key)
        if last is not None and at - last < self.window_seconds:
            self.suppressed += 1
            return False
        if len(self._seen) >= self.capacity:
            oldest = min(self._seen, key=lambda k: self._seen[k])
            self._seen.pop(oldest, None)
        self._seen[key] = at
        self.admitted += 1
        return True

    @property
    def ratio(self) -> float:
        total = self.admitted + self.suppressed
        return 0.0 if total == 0 else self.admitted / total
