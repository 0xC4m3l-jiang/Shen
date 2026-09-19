# L4 分析层（Python）—— 近线 worker 的运行镜像。
#
# 依赖取自 analysis/ 内的锁文件（版本固定 ⇒ 构建可复现）。
# 入口是 `python -m analysis.worker`：读核心遥测 → 态势去重 → 意图/链/策略 → 结论上报为事件。

FROM python:3.14-slim

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

# 先装运行期依赖（只改源码时这层不失效）
COPY analysis/requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

# 再装本层代码（可编辑安装：资源随包分发，AR-24）
COPY analysis/ ./
RUN pip install --no-cache-dir --no-deps setuptools && pip install --no-cache-dir --no-deps -e .

RUN useradd --system --uid 10001 shen && chown -R shen:shen /app
USER shen

ENTRYPOINT ["python", "-m", "analysis.worker"]
CMD ["--core", "127.0.0.1:9443", "--interval", "20"]
