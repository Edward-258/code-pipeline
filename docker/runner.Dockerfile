# mini-runner —— 步骤执行器的宿体（对照 Zadig reaper 全家桶的反面：只装步骤需要的工具）。
# 刻意不带 go/jdk 等编译器：编译/测试交给 docker_build 多阶段或任意用户镜像，
# runner 保持瘦（学习版哲学：工具按需进镜像，不做万能底座）。
# 分发：集群内 registry:2（127.0.0.1:30500 push），kubelet 走 localhost:5000 mirror 拉取（决断 #3），
# 不进 ACR——引擎分发与流水线产物两条链路互不干扰（决断 #12）。
FROM alpine:3.20
RUN sed -i 's#dl-cdn.alpinelinux.org#mirrors.aliyun.com#g' /etc/apk/repositories \
    && apk add --no-cache git docker-cli ca-certificates
WORKDIR /workspace
