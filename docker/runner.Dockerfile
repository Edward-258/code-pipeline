# pipeline-runner 0.2 —— 大满贯单镜像（2026-11-06 用户拍板：自动档通吃，免"现场选镜像"）。
# 哲学分叉记录在案：Zadig 是"一组系统镜像（xenial/bionic/focal-base，base 也不带 go）
# + jobexecutor 二进制 initContainer 注入"；我们是"单 runner 全能"——0.1.x 的"刻意瘦"
# 在 bootstrap 上撞了墙（build job 被迫显式指定 golang:1.27-alpine），学习版转向自动档通吃。
# 工具链从官方 golang 镜像整目录搬运（版本与 bootstrap 工作流对齐），GOPROXY 烘焙进镜像。
# 分发：ACR codepipeline-learning/pipeline-runner（deploy/build-runner.sh 本机直推公网
# 端点，集群 kubelet 经 VPC 端点拉取）。
FROM golang:1.27-alpine AS toolchain

FROM alpine:3.20
RUN sed -i 's#dl-cdn.alpinelinux.org#mirrors.aliyun.com#g' /etc/apk/repositories \
    && apk add --no-cache git docker-cli curl ca-certificates make jq
COPY --from=toolchain /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/go/bin:$PATH \
    GOPATH=/go \
    GOPROXY=https://goproxy.cn,direct
WORKDIR /workspace
