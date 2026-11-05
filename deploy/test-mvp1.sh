#!/usr/bin/env bash
# 阶段一验收（T1.10）：执行基建补课 A1+A2+A3+A4+A5+A13
#   幕1 队列：双 run 入队按序派发（同 wf 串行 + 全局上限见 MAX_RUNNING_TASKS）
#   幕2 取消：cancel running → 202 → 一拍内档案 cancelled + K8s Job 消失（+ 二次取消 409）
#   幕3 单 job 重试：MP_ATTEMPT 翻转剧本，retry=1 → passed + jt.retry==1 + 只剩末轮日志
#   幕4 outputs 跨 job：上游 mp_output → 下游 {{.job.key}} 渲染 + global_context 点分键
#   幕5 克隆：clone → 新号自动跑 → passed，creator=clone:<src>
#   幕6 整任务重试：failed 任务 retry → 失败节点重跑（end_time 变化证明）、预算归零
#   幕7 ignore 策略：软失败记红不刹车（stage 内兄弟 + 后续 stage 照跑，task 如实 failed）
#   幕8 超时可配：timeout_minutes=1 撞 sleep 90 → "timeout after 1m0s"
# 前置：本机引擎（§0.4 姿势）——port-forward mongo + MONGO_URI/K8S_CONTEXT go run .（ingress 上是旧镜像，别打）
# Usage: MP_BASE=http://localhost:8080 bash deploy/test-mvp1.sh
set -euo pipefail

BASE="${MP_BASE:-http://localhost:8080}"
CTX="${MP_CTX:-k3s-heyuan}"

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
assert_eq() { if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want=$3 got=$2"; fi }
assert_has() { if echo "$2" | grep -q "$3"; then ok "$1"; else bad "$1: missing '$3' in: $(echo "$2" | head -c 200)"; fi }
ensure_wf() { # JSON 走 stdin；先删后建保证幂等（复杂 body 用 heredoc 单引号防 bash 吃 $）
  local body; body=$(cat)
  if ! name=$(echo "$body" | jq -er .name); then
    echo "❌ ensure_wf: invalid JSON body:"; echo "$body" | head -5; exit 1
  fi
  curl -sk -X DELETE "$BASE/api/workflows/$name" >/dev/null || true
  echo "$body" | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
}
run_wf()    { curl -sk -X POST "$BASE/api/workflows/$1/run" | jq -r .task_id; }
task()      { curl -sk "$BASE/api/tasks/$1/$2"; }
field()     { echo "$1" | jq -r "$2"; }
wait_status() { # wf tid want_status max_s —— 终态/目标态轮询（每个 wf 自己的 counter，禁跨 wf 借号 §9.12）
  local T=""
  for i in $(seq 1 "${4:-60}"); do
    T=$(task "$1" "$2")
    [ "$(field "$T" .status)" = "$3" ] && break
    sleep 2
  done
  echo "$T"
}

echo "== 预检：healthz =="
HZ=$(curl -sk "$BASE/healthz")
assert_eq "healthz ok" "$(echo "$HZ" | jq -r .status)" "ok"

echo "== 幕1 队列：双 run 入队按序派发 =="
ensure_wf <<'EOF'
{"name":"mvp1-queue","stages":[{"name":"s","jobs":[
  {"name":"j","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"sleep 4; echo done"}]}}]}]}
EOF
T1=$(run_wf mvp1-queue); T2=$(run_wf mvp1-queue)
# 派发器 1s 一拍：本机引擎下 GET 可能晚于首拍——queued|running 都证明"run 即返回不阻塞"
# （2026-11-06 活体：快机器上 queued 竞速必输，断言放宽；同 wf 串行由下两断言承担）
ST1=$(field "$(task mvp1-queue "$T1")" .status)
case "$ST1" in queued|running) ok "run1 立即入队（$ST1）";; *) bad "run1 立即入队: got=$ST1";; esac
ST2=$(field "$(task mvp1-queue "$T2")" .status)
case "$ST2" in queued|running) ok "run2 立即入队（$ST2）";; *) bad "run2 立即入队: got=$ST2";; esac
for i in $(seq 1 15); do [ "$(field "$(task mvp1-queue "$T1")" .status)" = "running" ] && break; sleep 1; done
assert_eq "t1 派发 running"  "$(field "$(task mvp1-queue "$T1")" .status)" "running"
assert_eq "t2 同 wf 串行排队" "$(field "$(task mvp1-queue "$T2")" .status)" "queued"
T1F=$(wait_status mvp1-queue "$T1" passed 40)
T2F=$(wait_status mvp1-queue "$T2" passed 40)
assert_eq "t1 passed" "$(field "$T1F" .status)" "passed"
assert_eq "t2 接力 passed" "$(field "$T2F" .status)" "passed"

echo "== 幕2 取消：running → 202 → 一拍内 cancelled + Job 消失 =="
ensure_wf <<'EOF'
{"name":"mvp1-cancel","stages":[{"name":"s","jobs":[
  {"name":"sleeper","type":"freestyle","spec":{"steps":[{"name":"nap","type":"shell","script":"echo napping; sleep 120"}]}}]}]}
EOF
TC=$(run_wf mvp1-cancel)
for i in $(seq 1 15); do [ "$(field "$(task mvp1-cancel "$TC")" .status)" = "running" ] && break; sleep 1; done
CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X DELETE "$BASE/api/tasks/mvp1-cancel/$TC")
assert_eq "cancel 返回 202" "$CODE" "202"
TCF=$(wait_status mvp1-cancel "$TC" cancelled 15)
assert_eq "档案 cancelled" "$(field "$TCF" .status)" "cancelled"
assert_eq "树内 stage cancelled" "$(field "$TCF" '.stages[0].status')" "cancelled"
JN=$(field "$TCF" '.stages[0].jobs[0].k8s_job_name')
for i in $(seq 1 10); do
  kubectl --context "$CTX" -n pipeline get job "$JN" >/dev/null 2>&1 || break
  sleep 1
done
if kubectl --context "$CTX" -n pipeline get job "$JN" >/dev/null 2>&1; then bad "K8s Job 应已删除 ($JN)"; else ok "K8s Job 已消失"; fi
CODE2=$(curl -sk -o /dev/null -w '%{http_code}' -X DELETE "$BASE/api/tasks/mvp1-cancel/$TC")
assert_eq "二次取消 409" "$CODE2" "409"

echo "== 幕3 单 job 重试：MP_ATTEMPT 翻转剧本 =="
ensure_wf <<'EOF'
{"name":"mvp1-retry","stages":[{"name":"s","jobs":[
  {"name":"flaky","type":"freestyle","spec":{"retry":1,"steps":[
    {"name":"probe","type":"shell","script":"[ \"$MP_ATTEMPT\" = \"1\" ] && { echo attempt-1-fails-on-purpose; exit 1; }\necho \"attempt $MP_ATTEMPT ok\"\nmp_output \"ATTEMPT=$MP_ATTEMPT\""}]}}]}]}
EOF
TR=$(run_wf mvp1-retry)
TRF=$(wait_status mvp1-retry "$TR" passed 60)
assert_eq "重试后 passed" "$(field "$TRF" .status)" "passed"
assert_eq "重试计数 1"    "$(field "$TRF" '.stages[0].jobs[0].retry')" "1"
assert_has "末轮日志（化石已清）" "$(field "$TRF" '.stages[0].jobs[0].steps[0].log')" "attempt 2 ok"
assert_eq "末轮输出"       "$(field "$TRF" '.stages[0].jobs[0].outputs.ATTEMPT')" "2"

echo "== 幕4 outputs 跨 job 渲染 =="
ensure_wf <<'EOF'
{"name":"mvp1-outputs","global_context":{"REGION":"cn-heyuan"},"stages":[
  {"name":"produce","jobs":[{"name":"build","type":"freestyle","spec":{"steps":[
    {"name":"emit","type":"shell","script":"mp_output 'MSG=hello-from-build'"}]}}]},
  {"name":"deliver","jobs":[{"name":"consume","type":"freestyle","spec":{"steps":[
    {"name":"use","type":"shell","script":"echo GOT={{.build.MSG}} env={{.global.REGION}}"}]}}]}]}
EOF
TO=$(run_wf mvp1-outputs)
TOF=$(wait_status mvp1-outputs "$TO" passed 60)
assert_eq "串联 passed" "$(field "$TOF" .status)" "passed"
assert_has "下游渲染上游输出" "$(field "$TOF" '.stages[1].jobs[0].steps[0].log')" "GOT=hello-from-build env=cn-heyuan"
assert_eq "global_context 图纸级" "$(field "$TOF" '.global_context.REGION')" "cn-heyuan"
assert_eq "global_context 收割键" "$(field "$TOF" '.global_context["build.MSG"]')" "hello-from-build"

echo "== 幕5 克隆：快照 → 新号 → 自动跑 =="
CC=$(curl -sk -X POST "$BASE/api/tasks/mvp1-outputs/$TO/clone")
CN=$(field "$CC" .id); CT=$(field "$CC" .task_id)
assert_has "creator 出身标记" "$CC" "clone:mvp1-outputs-$TO"
SCC=$(field "$CC" .status)
case "$SCC" in queued|running) ok "新 task 立即入队（$SCC）";; *) bad "新 task 立即入队: got=$SCC";; esac
CCF=$(wait_status mvp1-outputs "$CT" passed 60)
assert_eq "克隆任务 passed ($CN)" "$(field "$CCF" .status)" "passed"
assert_has "克隆任务 outputs 重新收割" "$(field "$CCF" '.global_context["build.MSG"]')" "hello-from-build"

echo "== 幕6 整任务重试：失败节点重跑、预算归零 =="
ensure_wf <<'EOF'
{"name":"mvp1-retryfail","stages":[{"name":"s","jobs":[
  {"name":"hopeless","type":"freestyle","spec":{"steps":[
    {"name":"die","type":"shell","script":"echo no-hope; exit 3"}]}}]}]}
EOF
TF=$(run_wf mvp1-retryfail)
TFF=$(wait_status mvp1-retryfail "$TF" failed 40)
assert_eq "首跑 failed" "$(field "$TFF" .status)" "failed"
E1=$(field "$TFF" '.stages[0].jobs[0].steps[0].end_time')
CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X POST "$BASE/api/tasks/mvp1-retryfail/$TF/retry")
assert_eq "retry 返回 200" "$CODE" "200"
TFF2=$(wait_status mvp1-retryfail "$TF" failed 60)
E2=$(field "$TFF2" '.stages[0].jobs[0].steps[0].end_time')
assert_eq "重跑后仍 failed" "$(field "$TFF2" .status)" "failed"
if [ -n "$E1" ] && [ -n "$E2" ] && [ "$E1" != "$E2" ]; then ok "失败节点确实重跑 ($E1→$E2)"; else bad "end_time 未变化，疑似没重跑"; fi
assert_eq "重试预算归零" "$(field "$TFF2" '.stages[0].jobs[0].retry')" "0"

echo "== 幕7 ignore 策略：记红不刹车 =="
ensure_wf <<'EOF'
{"name":"mvp1-ignore","stages":[
  {"name":"s1","jobs":[
    {"name":"soft_fail","type":"freestyle","spec":{"error_policy":"ignore","steps":[
      {"name":"die","type":"shell","script":"echo soft-fail; exit 5"}]}},
    {"name":"after_in_stage","type":"freestyle","spec":{"steps":[
      {"name":"go","type":"shell","script":"echo still-ran"}]}}]},
  {"name":"s2","jobs":[
    {"name":"next_stage","type":"freestyle","spec":{"steps":[
      {"name":"go2","type":"shell","script":"echo next-stage-ran"}]}}]}]}
EOF
TI=$(run_wf mvp1-ignore)
TIF=$(wait_status mvp1-ignore "$TI" failed 60)
assert_eq "task 如实 failed（上报不撒谎）" "$(field "$TIF" .status)" "failed"
assert_eq "软失败 job 红"      "$(field "$TIF" '.stages[0].jobs[0].status')" "failed"
assert_eq "stage 内兄弟照跑"   "$(field "$TIF" '.stages[0].jobs[1].status')" "passed"
assert_eq "后续 stage 照跑"    "$(field "$TIF" '.stages[1].status')" "passed"

echo "== 幕8 超时可配 =="
ensure_wf <<'EOF'
{"name":"mvp1-timeout","stages":[{"name":"s","jobs":[
  {"name":"slow","type":"freestyle","spec":{"timeout_minutes":1,"steps":[
    {"name":"nap","type":"shell","script":"sleep 90; echo never"}]}}]}]}
EOF
TT=$(run_wf mvp1-timeout)
TTF=$(wait_status mvp1-timeout "$TT" failed 150)
assert_eq "超时任务 failed" "$(field "$TTF" .status)" "failed"
assert_has "超时错误信息"  "$(field "$TTF" '.stages[0].jobs[0].error')" "timeout after 1m0s"

echo ""
echo "=================================="
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ] && echo "阶段一验收全绿 ✅" || echo "阶段一验收有红 ❌"
exit "$fail"
