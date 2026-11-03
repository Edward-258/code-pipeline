# pipeline-runner —— 步骤执行器的宿体（对照 Zadig reaper 全家桶的反面：只装步骤需要的工具）。
# 刻意不带 go/jdk 等编译器：编译/测试交给 docker_build 多阶段或任意用户镜像，
# runner 保持瘦（学习版哲学：工具按需进镜像，不做万能底座）。
# 分发（2026-11-04 起）：ACR codepipeline-learning/pipeline-runner（deploy/build-runner.sh
# 本机直推公网端点，集群 kubelet 经 VPC 端点拉取）。
FROM alpine:3.20
RUN sed -i 's#dl-cdn.alpinelinux.org#mirrors.aliyun.com#g' /etc/apk/repositories \
    && apk add --no-cache git docker-cli ca-certificates
WORKDIR /workspace
