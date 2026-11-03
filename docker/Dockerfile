# mini-pipeline 引擎镜像（多阶段，对照 Zadig docker/*.Dockerfile 的模式：主仓库只出镜像）
# 2026-11-04 起本机 Docker Desktop 直构直推 ACR（deploy/build-push.sh）
FROM golang:1.26-alpine AS build
# 模块代理走 goproxy.cn
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -o /out/mini-pipeline .

FROM alpine:3.20
COPY --from=build /out/mini-pipeline /usr/local/bin/mini-pipeline
EXPOSE 8080
ENTRYPOINT ["mini-pipeline"]
