# 前端镜像：多阶段构建 —— node 编译 → nginx 运行（产物镜像零 node 运行时）
# 反代配置走 nginx 官方镜像的 envsubst 机制：
#   /etc/nginx/templates/*.template 启动时按环境变量渲染进 /etc/nginx/conf.d/
#   → 镜像自包含可配置，chart 侧只需传 MP_ENGINE_UPSTREAM 一个 env，零 ConfigMap
# ⚠️ 本机构建前基镜像需经 mirror 拉（§9.13：docker.io SNI 被掐），build-web.sh 已内置
FROM node:20-alpine AS build
WORKDIR /app
# 先拷清单装依赖（layer cache：源码变动不重装 node_modules）
COPY package.json package-lock.json ./
RUN npm ci --registry=https://registry.npmmirror.com --fetch-retries=5 --maxsockets=3
COPY . .
RUN npm run build

FROM nginx:alpine
ENV MP_ENGINE_UPSTREAM=mini-pipeline.pipeline.svc.cluster.local:8080
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
# resolv.conf → resolver 注入钩子（防启动期静态解析 emerg，见脚本头注释）
COPY docker-entrypoint-resolver.sh /docker-entrypoint.d/99-resolver.sh
RUN chmod +x /docker-entrypoint.d/99-resolver.sh
EXPOSE 80
