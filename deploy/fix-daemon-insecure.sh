#!/usr/bin/env bash
# 给两台 worker 的 docker daemon 补 insecure-registries（M5 docker_build 步骤的前提）。
#
# 为什么必须补：runner Pod 通过宿主 docker.sock 遥控节点 daemon 推镜像，
# push 端点是 <宿主IP>:30500（VPC IP 非 127/8）——daemon 对非 localhost 的 registry
# 默认走 HTTPS，而 registry:2 是纯 HTTP，会被 "server gave HTTP response to HTTPS client" 卡死。
# 127.0.0.1:30500 不用补（Docker 对 127/8 默认放行），但 worker-2 回环 NodePort 不通（决断 #3），
# 所以必须走 nodeIP 端点 → 必须显式放行。
#
# 影响面：只动节点上独立的 docker daemon（k3s 用的是 containerd，互不相干）；
# restart docker 只会打断正在跑的 docker build。原文件备份为 daemon.json.bak.<epoch>。
set -euo pipefail

IPS=(8.155.128.91 8.155.128.81)   # worker-1 / worker-2
INSECURE='["172.29.237.249:30500","172.29.237.250:30500","172.29.237.251:30500"]'

for ip in "${IPS[@]}"; do
  echo "== $ip =="
  ssh -i ~/.ssh/heyuan81.pem "root@$ip" "cp /etc/docker/daemon.json /etc/docker/daemon.json.bak.\$(date +%s) \
    && jq --argjson v '$INSECURE' '.[\"insecure-registries\"] = \$v' /etc/docker/daemon.json > /tmp/dj.json \
    && mv /tmp/dj.json /etc/docker/daemon.json \
    && systemctl restart docker \
    && sleep 2 && docker info 2>/dev/null | grep -A5 'Insecure Registries'"
done
