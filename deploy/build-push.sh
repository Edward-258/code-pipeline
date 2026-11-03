#!/usr/bin/env bash
# 引擎镜像构建+推送（2026-11-04 起本机 Docker Desktop 直构直推 ACR，ssh-worker 链路退役）。
# ⚠️ --provenance=false 是 ACR 硬要求：ACR 拒收 BuildKit 默认的 provenance/attestation 清单
#    （报 unknown manifest class for application/vnd.oci.empty.v1+json）。
# 前提：本机已对 ACR 公网端点 docker login 过一次（凭证存 ~/.docker/config.json）。
# Usage: deploy/build-push.sh <tag>          e.g. deploy/build-push.sh 0.4.5
set -euo pipefail

Tag="${1:?usage: $0 <tag>}"
ACR="crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com" # ACR 公网端点（推送侧）
NS="codepipeline-learning"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"

docker build --provenance=false -f "$repo_root/docker/Dockerfile" -t "${ACR}/${NS}/mini-pipeline:${Tag}" "$repo_root"
docker push "${ACR}/${NS}/mini-pipeline:${Tag}"

echo
echo "pushed: ${ACR}/${NS}/mini-pipeline:${Tag}"
echo "next:   helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=${Tag} --set tags.engine=true --wait"
