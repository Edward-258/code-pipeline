#!/usr/bin/env bash
# M3 验收：durable 暂停（灵魂时刻）
# 剧本：两 stage，stage2 带 manual_exec → 跑到 pause → kill 引擎 Pod → 档案仍 pause → release → 续跑 passed
# 前置：引擎已部署且 ingress 入口可达（自签证书，curl -k）
# Usage: bash deploy/test-m3.sh   （可用 MP_BASE/MP_CTX 覆盖入口与 kube context）
set -euo pipefail

BASE="${MP_BASE:-https://pipeline.47.113.177.198.nip.io}"
CTX="${MP_CTX:-k3s-heyuan}"
WF="m3-demo"

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
assert_eq() { # desc actual expected
  if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want=$3 got=$2"; fi
}

task_json() { curl -sk "$BASE/api/tasks/$WF/$1"; }
field()     { echo "$1" | jq -r "$2"; }

echo "== 1) 建图：stage1 直跑，stage2 带 manual_exec =="
curl -sk -X DELETE "$BASE/api/workflows/$WF" >/dev/null || true
cat <<'EOF' | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
{"name":"m3-demo","stages":[
  {"name":"build","jobs":[{"name":"j1","type":"freestyle","spec":{"image":"busybox","scripts":["echo hi from stage1"]}}]},
  {"name":"deploy","parallel":false,"manual_exec":{"enabled":true},
   "jobs":[{"name":"j2","type":"freestyle","spec":{"image":"busybox","scripts":["echo deployed ok"]}}]}
]}
EOF

echo "== 2) run → 等 pause =="
TASK_ID=$(curl -sk -X POST "$BASE/api/workflows/$WF/run" | jq -r .task_id)
if [ "$TASK_ID" -gt 0 ] 2>/dev/null; then ok "task_id=$TASK_ID"; else bad "no task id"; exit 1; fi

S=""; S1=""; S2=""
for i in $(seq 1 30); do
  T=$(task_json $TASK_ID)
  S=$(field "$T" .status); S1=$(field "$T" .stages[0].status); S2=$(field "$T" .stages[1].status)
  [ "$S" = "pause" ] && break
  sleep 2
done
assert_eq "task status"  "$S"  "pause"
assert_eq "stage1"       "$S1" "passed"
assert_eq "stage2"       "$S2" "pause"

echo "== 3) kill 引擎 Pod（durable 考验：此刻世上只剩 Mongo 里那份 pause）=="
kubectl --context "$CTX" -n pipeline delete pod -l app=mini-pipeline >/dev/null
kubectl --context "$CTX" -n pipeline rollout status deploy/mini-pipeline --timeout=120s >/dev/null
sleep 2

T=$(task_json $TASK_ID)
assert_eq "引擎重启后 task 仍 pause"   "$(field "$T" .status)"           "pause"
assert_eq "引擎重启后 stage2 仍 pause" "$(field "$T" .stages[1].status)" "pause"
ST=$(field "$T" .start_time)
if [ -n "$ST" ] && [ "$ST" != "0" ]; then ok "start_time 保留 ($ST)"; else bad "start_time 丢失"; fi

echo "== 4) release → 续跑 =="
CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tasks/$WF/$TASK_ID/stages/deploy/release")
assert_eq "release http" "$CODE" "200"

for i in $(seq 1 30); do
  T=$(task_json $TASK_ID)
  [ "$(field "$T" .status)" = "passed" ] && break
  sleep 2
done
assert_eq "续跑后 task"   "$(field "$T" .status)"           "passed"
assert_eq "续跑后 stage2" "$(field "$T" .stages[1].status)" "passed"

echo "== 5) 反向：重复 release 应 409 =="
CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tasks/$WF/$TASK_ID/stages/deploy/release")
assert_eq "double release" "$CODE" "409"

echo
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ]
