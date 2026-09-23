# Go 服务的**统一**构建：core / proxy / console / web（Web 诱饵后端）共用（用构建参数 SERVICE 指定入口）。
#
# 为什么一个文件：三个服务是同一份 Go 模块的三个入口，构建步骤完全一致；
# 拆成三份只会让「依赖升级」变成三处修改。
#
# 用法（compose 已接好，一般不用手敲）：
#   docker build -f deploy/docker/go.Dockerfile --build-arg SERVICE=./common/core/cmd/core -t shen-core .
#
# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
ARG SERVICE
WORKDIR /src

# 依赖已 **vendor 入库**（vendor/ 与 go.mod 一起提交），所以：
#   ① 构建**不需要网络**、也不需要 Go 模块代理（本机访问不了 proxy.golang.org，靠 vendor 才起得来）；
#   ② 故意不写 `go mod download` —— 有 vendor 时它反而会去联网，把构建卡死。
# 先拷依赖清单与 vendor：只改源码时这一层仍命中缓存，后续构建快得多。
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY common/api/ ./common/api/
COPY common/core/ ./common/core/
COPY modules/deception/ ./modules/deception/
COPY modules/console/ ./modules/console/
# 蜜罐层（②）也有 Go 代码（合成 Web 场景包 `modules/honeypot/web/`）—— 它同样按这个统一构建打包。
COPY modules/honeypot/ ./modules/honeypot/
COPY scripts/ ./scripts/
# CGO_ENABLED=0：静态二进制，运行镜像不需要 libc。
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app "${SERVICE}"

FROM alpine:3.20
# 只为了证书：适配器与核心若要出网（ACME / 上游 HTTPS）需要根证书包。
RUN apk add --no-cache ca-certificates
COPY --from=build /out/app /usr/local/bin/shen
# 非 root 运行：三个服务都不需要写文件。
USER nobody
ENTRYPOINT ["/usr/local/bin/shen"]
