# `doctor` —— 接入自检（`INT-17` 五项）

| 项 | 值 |
| --- | --- |
| 阶段 | 1（接入前必须过） |
| 依据 | `INT-17` · `INT-22`（读不到 body 禁止启用误导处置）· `ST-10` · `ADR-0019` |

## 用法

```sh
scripts/shen.sh doctor          # = make doctor
python3 scripts/doctor/doctor.py --entry http://host:8080 --console http://host:9444 --origin http://真实业务:9000 --json
```

五项：① body 是否可读 ② TLS 是终结还是透传 ③ 会话粘性 ④ 实境与幻境可区分 ⑤ 引擎是否真的在请求路径上。

结果有四种状态：**通过** · **失败** · **约束**（验到了但限制你能做什么）· **无法判定**（验不了，写明原因与关法）。
**不给假绿**：④ 在没有登记可用幻境后端时就是"无法判定"，不会假通过。

完整说明与实测例子：[`../../docs/integrate/doctor.md`](../../docs/integrate/doctor.md)。
只用标准库（python3），无第三方依赖。
