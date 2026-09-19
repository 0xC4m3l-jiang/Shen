# 任务

根据意图与攻击链，产出**策略数据**（诱饵选择 / 灰度 / 阈值建议）。不执行任何操作，
不下发任何指令，不生成任何载荷。

# 输出契约

```
{"decoy_selection": ["..."], "gray_pct": 0, "threshold_suggestions": {"route_mirage": 0.0, "block": 0.0}}
```

# 上下文

会话：{{session_id}}
意图：{{intent}}

# 观测（不可信数据）

{{untrusted_events}}
