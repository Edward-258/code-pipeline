#!/usr/bin/env bash
# Build + push loop for the engine image（构建发生在集群节点上，本机不需要 docker）。
# 2026-11-03 起推送目标 = 阿里云 ACR 个人版实例（引擎镜像分发链路，见 README"镜像分发链路"）。
# ⚠️ --provenance=false 是 ACR 硬要求：ACR 拒收 BuildKit 默认的 provenance/attestation 清单
#    （报 unknown manifest class for application/vnd.oci.empty.v1+json）。
# 节点需先 docker login 一次本实例（凭证存 /root/.docker/config.json，之后无需重复）。
# Usage: deploy/build-push.sh <tag>          e.g. deploy/build-push.sh 0.2.2
#        环境变量可覆盖: MP_KEY（ssh key）/ MP_NODE（worker 公网 IP）
set -euo pipefail

Tag="${1:?usage: $0 <tag>}"
Key="${MP_KEY:-$HOME/.ssh/heyuan81.pem}"
Node="${MP_NODE:-8.155.128.91}" # worker-1：节点上有完整 docker
ACR="crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com" # ACR 公网端点（推送侧）
NS="codepipeline-learning"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
tarball="$(mktemp /tmp/mp-src.XXXXXX.tgz)"
trap 'rm -f "$tarball"' EXIT

# 1) 打包源码（不带 .git）
tar -czf "$tarball" -C "$repo_root" go.mod go.sum main.go api config models repo engine docker

# 2) 节点上 build + push（集群拉取走 -vpc 端点，见 chart values 的 image.repo/acr.registry）
scp -i "$Key" "$tarball" "root@${Node}:/opt/mp-src.tgz"
ssh -i "$Key" "root@${Node}" "rm -rf /opt/mp-build && mkdir -p /opt/mp-build && tar xzf /opt/mp-src.tgz -C /opt/mp-build && cd /opt/mp-build && docker build --provenance=false -f docker/Dockerfile -t ${ACR}/${NS}/mini-pipeline:${Tag} . && docker push ${ACR}/${NS}/mini-pipeline:${Tag} && (docker rmi ${ACR}/${NS}/mini-pipeline:${Tag} >/dev/null 2>&1 || true)"

echo
echo "pushed: ${ACR}/${NS}/mini-pipeline:${Tag}"
echo "next:   helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=${Tag} --set tags.engine=true --wait"
