"""遥测面端口（L4 的**唯一**外部 I/O）。

契约来自 `api/telemetry/v1/telemetry.proto`：
- 读：`ListEvents`（拉最近事件）；
- 写：`Report`（上报**结论事件**，幂等键 `event_id`）。

L4 **不写**普通存储（`MD-20`：`store` 是核心唯一的 I/O 出口）；它只把结论作为**事件**上报。
gRPC 适配器**延迟导入** `grpc`，这样单测与离线分析不需要 grpc 也能跑。
"""

# pyright: reportMissingImports=false, reportMissingModuleSource=false
# 本仓库静态检查器的导入解析不可靠（绝对导入与包内相对导入都误报）；
# 运行时权威判据是 pytest（缺导入会在运行时真炸）+ `pip install -e .` 安装为包。

from __future__ import annotations

import json
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from typing import Any, Protocol

CONCLUSION_EVENT_TYPE = "analysis"
"""结论事件类型：控制台按它单独成块（人工测试时可一眼看到 L4 干了什么）。"""


@dataclass(frozen=True)
class WireEvent:
    """遥测事件的线上形状（与 `.proto` 一一对应）。"""

    event_id: str
    event_type: str
    session_id: str = ""
    actor_id: str = ""
    payload: bytes = b""
    created_at: str = ""

    def json_payload(self) -> dict[str, Any]:
        if not self.payload:
            return {}
        try:
            decoded = json.loads(self.payload.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            return {"_undecodable": True}
        return decoded if isinstance(decoded, dict) else {"_value": decoded}


class TelemetryPort(Protocol):
    """L4 需要的全部 I/O 能力（消费方定义，便于用替身测试 —— `MD-22`）。"""

    def list_events(
        self, *, limit: int = 200, since: str | None = None, event_type: str = ""
    ) -> Sequence[WireEvent]: ...

    def report(self, event: WireEvent) -> tuple[int, int]:
        """返回 `(accepted, duplicated)`（`AR-11` 幂等）。"""
        ...


def _load_well_known_types() -> None:
    """先把 **well-known types** 导入进来，再导入生成的桩。

    新版 protobuf 的生成物是在运行期解析依赖的：`telemetry.proto` 里
    `import "google/protobuf/timestamp.proto"`；若不先把这个模块导进来，`AddSerializedFile` 会报
    `Depends on file 'google/protobuf/timestamp.proto', but it has not been loaded`
    （容器里就真踩到了：宿主机的环境恰好先加载过，属于撞巧）。
    """
    import google.protobuf.timestamp_pb2  # noqa: F401  —— 导入即注册，必须早于生成物


class TelemetryUnavailable(RuntimeError):
    """遥测面不可达 —— L4 **降级为不分析**（近线，不在业务路径上，`NI-1`）。"""


class GrpcTelemetryClient:
    """gRPC 适配器：唯一碰网络的实现。"""

    def __init__(self, target: str, *, timeout: float = 5.0) -> None:
        try:
            import grpc
        except ModuleNotFoundError as exc:  # 显式失败，不静默降级（AR-15）
            raise TelemetryUnavailable(f"缺少 grpc（跑 make pyenv 安装锁定依赖）：{exc}") from exc
        _load_well_known_types()
        from analysis.proto.telemetry.v1 import telemetry_pb2_grpc

        self._grpc = grpc
        self._target = target
        self._timeout = timeout
        # loopback 明文：与核心侧「明文监听只允许本机」的断言对齐（assertPlaintextListenIsLocal）
        channel = grpc.insecure_channel(target)
        self._stub = telemetry_pb2_grpc.DeceptionTelemetryStub(channel)

    def list_events(
        self, *, limit: int = 200, since: str | None = None, event_type: str = ""
    ) -> Sequence[WireEvent]:
        from analysis.proto.telemetry.v1 import telemetry_pb2

        request = telemetry_pb2.ListEventsRequest(limit=limit, event_type=event_type)
        if since:
            request.since.FromJsonString(since)
        try:
            response = self._stub.ListEvents(request, timeout=self._timeout)
        except self._grpc.RpcError as exc:
            raise TelemetryUnavailable(f"ListEvents 失败：{exc.code()} {exc.details()}") from exc
        return [_from_proto(item) for item in response.events]

    def report(self, event: WireEvent) -> tuple[int, int]:
        try:
            ack = self._stub.Report(_to_proto(event), timeout=self._timeout)
        except self._grpc.RpcError as exc:
            raise TelemetryUnavailable(f"Report 失败：{exc.code()} {exc.details()}") from exc
        return ack.accepted, ack.duplicated


def _from_proto(item: Any) -> WireEvent:
    return WireEvent(
        event_id=item.event_id,
        event_type=item.event_type,
        session_id=item.session_id,
        actor_id=item.actor_id,
        payload=bytes(item.payload),
        created_at=item.created_at.ToJsonString() if item.HasField("created_at") else "",
    )


def _to_proto(event: WireEvent) -> Any:
    from analysis.proto.telemetry.v1 import telemetry_pb2

    item = telemetry_pb2.TelemetryEvent(
        event_id=event.event_id,
        event_type=event.event_type,
        session_id=event.session_id,
        actor_id=event.actor_id,
        payload=event.payload,
    )
    if event.created_at:
        item.created_at.FromJsonString(event.created_at)
    return item


class InMemoryTelemetry:
    """测试与离线替身：同一套语义，无网络（`MD-22`）。"""

    def __init__(self, events: Iterable[WireEvent] = ()) -> None:
        self._events: list[WireEvent] = list(events)
        self.reported: list[WireEvent] = []

    def list_events(
        self, *, limit: int = 200, since: str | None = None, event_type: str = ""
    ) -> Sequence[WireEvent]:
        picked = [e for e in self._events if not event_type or e.event_type == event_type]
        if since:
            picked = [e for e in picked if e.created_at >= since]
        return picked[-limit:]

    def report(self, event: WireEvent) -> tuple[int, int]:
        if any(existing.event_id == event.event_id for existing in self._events):
            return 0, 1
        self._events.append(event)
        self.reported.append(event)
        return 1, 0
