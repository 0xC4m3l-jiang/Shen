# 管控台前端镜像（多阶段）。构建上下文是仓库根。
#
#   target=dev      Vite 开发服务器（compose.dev.yaml 使用；源码由绑定挂载提供，支持热更新）
#   target=runtime  nginx 提供构建好的静态资源，并同源反代 /api/ → console-api（默认 / 生产）
#
# 用法：docker build -f deploy/docker/console-ui.Dockerfile --target runtime -t shen/console-ui .
# syntax=docker/dockerfile:1

# ── 依赖层：只拷清单与锁文件，改源码不失效；npm ci 严格按锁文件安装（可复现）──────────────
FROM node:22-alpine AS deps
WORKDIR /ui
ENV npm_config_fund=false npm_config_audit=false npm_config_update_notifier=false
COPY modules/console/ui/package.json modules/console/ui/package-lock.json ./
RUN npm ci

# ── 开发：Vite（监听 9444，与生产同一个宿主端口；/api 代理到 127.0.0.1:9445）──────────────
FROM deps AS dev
COPY --chown=node:node modules/console/ui/ ./
# Vite 要往 node_modules/.vite 写预构建缓存：交给非 root 的 node 用户。
RUN chown -R node:node /ui
ENV SHEN_VITE_PORT=9444 \
    SHEN_CONSOLE_API_URL=http://127.0.0.1:9445
USER node
EXPOSE 9444
CMD ["npx", "vite", "--host", "0.0.0.0"]

# ── 构建：类型检查 + 打包（类型错误会让镜像构建失败，而不是带着错误上线）──────────────────
FROM deps AS build
COPY modules/console/ui/ ./
RUN npm run build

# ── 运行：非 root 的 nginx（uid 101，监听 9444 这种非特权端口）───────────────────────────
FROM nginxinc/nginx-unprivileged:1.27-alpine AS runtime
COPY deploy/docker/nginx/console.conf /etc/nginx/conf.d/default.conf
COPY deploy/docker/nginx/security-headers.conf /etc/nginx/snippets/security-headers.conf
COPY --from=build /ui/dist /usr/share/nginx/html
EXPOSE 9444
