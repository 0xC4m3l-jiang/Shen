# Go 服务的**统一**构建：core / proxy / console-api / honeypot-web 共用（用构建参数 SERVICE 指定入口）。
#
# 为什么一个文件：它们是同一份 Go 模块的多个入口，构建步骤完全一致；
# 拆成多份只会让「依赖升级」变成多处修改。
#
# 用法（compose 已接好，一般不用手敲；仓库根执行）：
#   docker build -f deploy/docker/go.Dockerfile --build-arg SERVICE=./common/core/cmd/core -t shen/core .
#
# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
ARG SERVICE
WORKDIR /src

# 依赖已 **vendor 入库**（vendor/ 与 go.mod 一起提交），所以：
#   ① 构建**不需要网络**、也不需要 Go 模块代理；
#   ② 故意不写 `go mod download` —— 有 vendor 时它反而会去联网，把构建卡死。
# 先拷依赖清单与 vendor：只改源码时这一层仍命中缓存，后续构建快得多。
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY common/api/ ./common/api/
COPY common/core/ ./common/core/
COPY modules/deception/ ./modules/deception/
# 管控台 API（含内嵌的 ip2region 归属地库）；前端工程 ui/ 由 console-ui.Dockerfile 单独构建，这里排除无关文件由 .dockerignore 负责。
COPY modules/console/ ./modules/console/
COPY modules/honeypot/ ./modules/honeypot/
COPY scripts/ ./scripts/
# CGO_ENABLED=0：静态二进制，运行镜像不需要 libc。
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/app "${SERVICE}"

FROM alpine:3.20
# ca-certificates：适配器与核心若要出网（ACME / 上游 HTTPS）需要根证书包。
# busybox wget（alpine 自带）供 compose 健康检查使用。
RUN apk add --no-cache ca-certificates \
    && mkdir -p /data && chown nobody:nobody /data && chmod 0700 /data
COPY --from=build /out/app /usr/local/bin/shen
# 非 root 运行。只有管控台 API 需要写文件：写到 /data（compose 挂载命名卷 console-data；
# 首次挂载空卷时 Docker 会沿用这里的属主 nobody，因此无需额外 chown）。其余服务不写盘。
USER nobody
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/shen"]
