#!/usr/bin/env bash
# 自举图纸落库（T2.5）：建 env self（引擎自身 namespace，deploy job 的自举靶标）+
# 建 bootstrap 工作流。⭐ 只落库，不触发执行——执行是【需人工】动作（见尾部清单）。
# Usage: MP_BASE=http://localhost:8080 bash deploy/bootstrap-ensure.sh
set -euo pipefail

BASE="${MP_BASE:-http://localhost:8080}"
DIR="$(cd "$(dirname "$0")" && pwd)"

# 1) env self：deploy job 解析 "self" → 引擎所在 namespace（Role 也在这个 ns，权限边界一致）
code=$(curl -sk -o /tmp/env-resp.json -w '%{http_code}' -X POST "$BASE/api/envs" \
  -H 'Content-Type: application/json' \
  -d '{"name":"self","namespace":"pipeline","desc":"引擎自身所在 namespace（自举部署靶标）"}')
[ "$code" = "200" ] || [ "$code" = "409" ] || { echo "❌ ensure env self failed ($code): $(cat /tmp/env-resp.json)"; exit 1; }
echo "✅ env self 就绪 ($code)"

# 1.5) registry acr：docker_build 的推送目标（2026-11-07 registry 管理化——Pod 内自行
# docker login，不再依赖"哪个 worker 手工 login 过"；凭证 = chart values acr.*，公网推送端点）
code=$(curl -sk -o /tmp/reg-resp.json -w '%{http_code}' -X POST "$BASE/api/registries" \
  -H 'Content-Type: application/json' \
  -d '{"name":"acr","address":"crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com","username":"nick9188231182","password":"Zhongshikun@123","desc":"ACR 个人版（自举镜像推送，凭证与 chart values acr.* 同源）"}')
[ "$code" = "200" ] || [ "$code" = "409" ] || { echo "❌ ensure registry acr failed ($code): $(cat /tmp/reg-resp.json)"; exit 1; }
echo "✅ registry acr 就绪 ($code)"

# 2) bootstrap 工作流（先删后建，图纸改了重跑本脚本即生效）
curl -sk -X DELETE "$BASE/api/workflows/bootstrap" >/dev/null || true
code=$(curl -sk -o /tmp/wf-resp.json -w '%{http_code}' -X POST "$BASE/api/workflows" \
  -H 'Content-Type: application/json' -d @"$DIR/bootstrap-workflow.json")
[ "$code" = "200" ] || { echo "❌ ensure workflow bootstrap failed ($code): $(cat /tmp/wf-resp.json)"; exit 1; }
echo "✅ bootstrap 图纸已落库"

echo ""
echo "执行前置清单（全部【需人工】）:"
echo "  ① （2026-11-07 起可跳过）worker 节点 docker login ACR——bootstrap 的 docker_build 已走"
echo "     registry 引用（Pod 内 DOCKER_CONFIG=/tmp/.docker 自行 login）；节点 login 仅兜底旧全名直推图纸"
echo "  ② chart Role 扩展已活体生效（T2.3 role.yaml 的 helm upgrade，含 apps 组 patch）"
echo "  ③ 之后：curl -sk -X POST $BASE/api/workflows/bootstrap/run"
echo "  ⚠️ selfdeploy 的 wait_rollout=false 是自陷保护：patch 即成功，不等 rollout（防新 Pod 绞杀正在跑的自己）"
