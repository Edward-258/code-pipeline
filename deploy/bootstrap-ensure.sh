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

# 2) bootstrap 工作流（先删后建，图纸改了重跑本脚本即生效）
curl -sk -X DELETE "$BASE/api/workflows/bootstrap" >/dev/null || true
code=$(curl -sk -o /tmp/wf-resp.json -w '%{http_code}' -X POST "$BASE/api/workflows" \
  -H 'Content-Type: application/json' -d @"$DIR/bootstrap-workflow.json")
[ "$code" = "200" ] || { echo "❌ ensure workflow bootstrap failed ($code): $(cat /tmp/wf-resp.json)"; exit 1; }
echo "✅ bootstrap 图纸已落库"

echo ""
echo "执行前置清单（全部【需人工】）:"
echo "  ① worker 节点 daemon 一次性 docker login ACR 公网端点（凭证 = chart values acr.username/password）："
echo "     ssh -i ~/.ssh/heyuan81.pem root@<workerIP> 'docker login crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com -u <user>'"
echo "  ② chart Role 扩展已活体生效（T2.3 role.yaml 的 helm upgrade，含 apps 组 patch）"
echo "  ③ 之后：curl -sk -X POST $BASE/api/workflows/bootstrap/run"
echo "  ⚠️ selfdeploy 的 wait_rollout=false 是自陷保护：patch 即成功，不等 rollout（防新 Pod 绞杀正在跑的自己）"
