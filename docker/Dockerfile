# mini-pipeline 引擎镜像（多阶段，对照 Zadig docker/*.Dockerfile 的模式：主仓库只出镜像）
# 注意：本机 Docker Desktop 通常没开，实际构建在集群节点上做（见 AGENTS.md 常用命令）
FROM golang:1.26-alpine AS build
# 节点在国内网络，模块代理走 goproxy.cn
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
