#!/usr/bin/env bash
# 构建 pipeline-runner 并推入 ACR（本机 Docker 直构直推，runner 变更时重跑）。
# 与引擎镜像同实例同 namespace（codepipeline-learning）：推送走公网端点，
# 集群 kubelet 拉取走 VPC 端点（同仓同名，见 chart values runner.image）。
# ⚠️ --provenance=false 是 ACR 硬要求（同 build-push.sh：ACR 拒收 BuildKit provenance 清单）。
# 前提：本机已对 ACR 公网端点 docker login 过一次（凭证存 ~/.docker/config.json）。
# Usage: deploy/build-runner.sh [tag]   # 默认 0.1.0，须与 chart values runner.image 的 tag 一致
set -euo pipefail

TAG="${1:-0.1.0}"
ACR="crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com" # 公网端点（推送侧）
IMAGE="${ACR}/codepipeline-learning/pipeline-runner:${TAG}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"

docker build --provenance=false -f "$repo_root/docker/runner.Dockerfile" -t "${IMAGE}" "$repo_root"
docker push "${IMAGE}"
echo "pushed: ${IMAGE}（kubelet 拉取走 VPC 端点，仓库路径不变；push 自动建仓）"
