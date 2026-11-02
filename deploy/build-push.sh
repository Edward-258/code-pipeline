#!/usr/bin/env bash
# Build + push loop for the engine image（构建发生在集群节点上，本机不需要 docker）。
# WSL 版：源码取自本仓库，替代 Windows 时代的 build-push.ps1（那个打包的是 E: 盘旧副本，勿再用）。
# Usage: deploy/build-push.sh <tag>          e.g. deploy/build-push.sh 0.2.1
#        环境变量可覆盖: MP_KEY（ssh key）/ MP_NODE（worker 公网 IP）
set -euo pipefail

Tag="${1:?usage: $0 <tag>}"
Key="${MP_KEY:-$HOME/.ssh/heyuan81.pem}"
Node="${MP_NODE:-8.155.128.91}" # worker-1：节点上有完整 docker，且 127.0.0.1:30500 即 registry:2

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
tarball="$(mktemp /tmp/mp-src.XXXXXX.tgz)"
trap 'rm -f "$tarball"' EXIT

# 1) 打包源码（不带 .git）
tar -czf "$tarball" -C "$repo_root" go.mod go.sum main.go api config models repo engine docker

# 2) 节点上 build + push（registry 存储会剥离 host 前缀，kubelet 侧镜像名即 localhost:5000/…）
scp -i "$Key" "$tarball" "root@${Node}:/opt/mp-src.tgz"
ssh -i "$Key" "root@${Node}" "rm -rf /opt/mp-build && mkdir -p /opt/mp-build && tar xzf /opt/mp-src.tgz -C /opt/mp-build && cd /opt/mp-build && docker build -f docker/Dockerfile -t 127.0.0.1:30500/mini-pipeline:${Tag} . && docker push 127.0.0.1:30500/mini-pipeline:${Tag} && (docker rmi 127.0.0.1:30500/mini-pipeline:${Tag} >/dev/null 2>&1 || true)"

echo
echo "pushed: 127.0.0.1:30500/mini-pipeline:${Tag}  (= localhost:5000/mini-pipeline:${Tag})"
echo "next:   helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=${Tag} --set tags.engine=true --wait"
