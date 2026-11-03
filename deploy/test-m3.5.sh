#!/usr/bin/env bash
# M3.5 验收：启动宣判（running 的孤儿档案宣判死亡，pause 的稳态不碰）
# 剧本A：sleep 300 的任务跑到 running → kill 引擎 Pod → 新引擎宣判 cancelled + 泄漏 Job 被清
# 剧本B：带闸门的任务跑到 pause   → kill 引擎 Pod → 仍 pause，release 续跑照常成立
# 前置：引擎已部署且 ingress 入口可达（自签证书，curl -k）
# Usage: bash deploy/test-m3.5.sh   （可用 MP_BASE/MP_CTX 覆盖入口与 kube context）
set -euo pipefail

BASE="${MP_BASE:-https://pipeline.47.113.177.198.nip.io}"
CTX="${MP_CTX:-k3s-heyuan}"

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
assert_eq() { # desc actual expected
  if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want=$3 got=$2"; fi
}

task_json() { curl -sk "$BASE/api/tasks/$1/$2"; }
field()     { echo "$1" | jq -r "$2"; }

kill_engine_and_wait() {
  kubectl --context "$CTX" -n pipeline delete pod -l app=mini-pipeline >/dev/null
  kubectl --context "$CTX" -n pipeline rollout status deploy/mini-pipeline --timeout=120s >/dev/null
  sleep 3 # 新 Pod 起来即已跑过 Recover（宣判发生在监听端口之前），稍等 API 就绪
}

# ============ 剧本A：running 死 → 宣判 ============
WF="m35-running"
echo "== A1) 建图：单 job sleep 300（够我们从容 kill 引擎） =="
curl -sk -X DELETE "$BASE/api/workflows/$WF" >/dev/null || true
cat <<'EOF' | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
{"name":"m35-running","stages":[
  {"name":"hold","jobs":[{"name":"sleepy","type":"freestyle","spec":{"image":"busybox","scripts":["echo sleeping; sleep 300; echo done"]}}]}
]}
EOF

echo "== A2) run → 等 running，记下 K8s Job 名 =="
TID=$(curl -sk -X POST "$BASE/api/workflows/$WF/run" | jq -r .task_id)
[ "$TID" -gt 0 ] 2>/dev/null && ok "task_id=$TID" || { bad "no task id"; exit 1; }

JNAME=""
for i in $(seq 1 30); do
  T=$(task_json $WF $TID)
  [ "$(field "$T" .status)" = "running" ] && JNAME=$(field "$T" '.stages[0].jobs[0].k8s_job_name') && break
  sleep 2
done
assert_eq "task running" "$(field "$T" .status)" "running"
[ -n "$JNAME" ] && [ "$JNAME" != "null" ] && ok "k8s job = $JNAME" || bad "no k8s job name"

echo "== A3) kill 引擎 Pod（此刻 sleepy 的 Job 成了无主执行物） =="
kill_engine_and_wait

echo "== A4) 新引擎宣判：档案 cancelled、泄漏 Job 已清 =="
for i in $(seq 1 15); do
  T=$(task_json $WF $TID)
  [ "$(field "$T" .status)" = "cancelled" ] && break
  sleep 2
done
assert_eq "task 宣判"      "$(field "$T" .status)"                    "cancelled"
assert_eq "stage 宣判"     "$(field "$T" .stages[0].status)"          "cancelled"
assert_eq "job 宣判"       "$(field "$T" .stages[0].jobs[0].status)"  "cancelled"
ERR=$(field "$T" .error)
case "$ERR" in *restart*) ok "error 注明 restart ($ERR)" ;; *) bad "error 不含 restart: $ERR" ;; esac

for i in $(seq 1 10); do # Job 删除是 Background 级联，稍等
  kubectl --context "$CTX" -n pipeline get job "$JNAME" >/dev/null 2>&1 || break
  sleep 2
done
if kubectl --context "$CTX" -n pipeline get job "$JNAME" >/dev/null 2>&1; then
  bad "泄漏 Job 未清理: $JNAME"
else
  ok "泄漏 Job 已清理 ($JNAME)"
fi

# ============ 剧本B：pause 死 → 稳态不碰 ============
WF2="m35-gate"
echo "== B1) 建图：stage1 直跑 + stage2 闸门 =="
curl -sk -X DELETE "$BASE/api/workflows/$WF2" >/dev/null || true
cat <<'EOF' | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
{"name":"m35-gate","stages":[
  {"name":"build","jobs":[{"name":"j1","type":"freestyle","spec":{"image":"busybox","scripts":["echo hi"]}}]},
  {"name":"deploy","parallel":false,"manual_exec":{"enabled":true},
   "jobs":[{"name":"j2","type":"freestyle","spec":{"image":"busybox","scripts":["echo deployed"]}}]}
]}
EOF

TID2=$(curl -sk -X POST "$BASE/api/workflows/$WF2/run" | jq -r .task_id)
for i in $(seq 1 30); do
  T2=$(task_json $WF2 $TID2)
  [ "$(field "$T2" .status)" = "pause" ] && break
  sleep 2
done
assert_eq "剧本B 前置: task pause" "$(field "$T2" .status)" "pause"

echo "== B2) kill 引擎 Pod → 宣判扫描必须放过 pause =="
kill_engine_and_wait
T2=$(task_json $WF2 $TID2)
assert_eq "pause 任务存活"   "$(field "$T2" .status)"                  "pause"
assert_eq "stage2 仍 pause"  "$(field "$T2" .stages[1].status)"       "pause"

echo "== B3) release 续跑照常成立（M3 灵魂未被 M3.5 误伤） =="
CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tasks/$WF2/$TID2/stages/deploy/release")
assert_eq "release http" "$CODE" "200"
for i in $(seq 1 30); do
  T2=$(task_json $WF2 $TID2)
  [ "$(field "$T2" .status)" = "passed" ] && break
  sleep 2
done
assert_eq "续跑后 task" "$(field "$T2" .status)" "passed"

echo
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ]
