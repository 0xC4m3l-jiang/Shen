# 演示用的「假业务站」—— 只用标准库，用来验证「经引擎访问业务」这条链路。
#
# ⚠️ 它**不是**产品的一部分：真实部署时把 `SHEN_PROXY_UPSTREAM` 指向你自己的服务即可。

FROM python:3.14-slim

WORKDIR /app
COPY scripts/demo/business.py /app/business.py

USER nobody
CMD ["python", "/app/business.py", "19080"]
