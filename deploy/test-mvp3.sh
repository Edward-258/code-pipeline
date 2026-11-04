#!/usr/bin/env bash
# 阶段三验收（T3.5）：触发与协作 A8+A9+A10+A11
#   幕1 cron：手动点火（creator=cron:<name>）+ 真调度器到点触发（每分钟表达式，等待一拍后即删防刷屏）
#   幕2 webhook：签名对→触发 / 错签 401 / 分支不匹配不触发
#   幕3 审批：all 两人到齐才续跑 / any 一人即过
#   幕4 兼容 release：老图纸（无 approvers）走原 release 端点照样续跑
# 前置：本机引擎（§0.4 姿势）。Usage: MP_BASE=http://localhost:8080 bash deploy/test-mvp3.sh
set -euo pipefail

BASE="${MP_BASE:-http://localhost:8080}"

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
assert_eq() { if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want=$3 got=$2"; fi }
assert_has() { if echo "$2" | grep -q "$3"; then ok "$1"; else bad "$1: missing '$3' in: $(echo "$2" | head -c 200)"; fi }
code_of() { curl -sk -o /tmp/mvp3-resp.json -w '%{http_code}' "$@"; }
ensure_wf() {
  local body; body=$(cat)
  if ! name=$(echo "$body" | jq -er .name); then echo "❌ invalid JSON"; exit 1; fi
  curl -sk -X DELETE "$BASE/api/workflows/$name" >/dev/null || true
  echo "$body" | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
}
wait_status() { # wf tid want max_s
  local T=""
  for i in $(seq 1 "${4:-60}"); do
    T=$(curl -sk "$BASE/api/tasks/$1/$2")
    [ "$(echo "$T" | jq -r .status)" = "$3" ] && break
    sleep 2
  done
  echo "$T"
}
sig_of() { # body secret → GitHub 形态签名
  printf 'sha256=%s' "$(printf '%s' "$1" | openssl dgst -sha256 -hmac "$2" -r | cut -d' ' -f1)"
}

echo "== 预检：healthz =="
assert_eq "healthz ok" "$(curl -sk "$BASE/healthz" | jq -r .status)" "ok"

echo "== 幕1 cron：点火 + 真调度 =="
ensure_wf <<'EOF'
{"name":"mvp3-cron","stages":[{"name":"s","jobs":[
  {"name":"j","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"echo cron-fired"}]}}]}]}
EOF
assert_eq "cron 创建" "$(code_of -X POST "$BASE/api/crons" -H 'Content-Type: application/json' -d '{"name":"mvp3-minutely","workflow_name":"mvp3-cron","crontab":"* * * * *","enabled":true}')" "200"
assert_eq "坏表达式 400" "$(code_of -X POST "$BASE/api/crons" -H 'Content-Type: application/json' -d '{"name":"mvp3-bad","workflow_name":"mvp3-cron","crontab":"99 * * * *"}')" "400"
CODE=$(code_of -X POST "$BASE/api/crons/mvp3-minutely/fire")
CREATOR=$(cat /tmp/mvp3-resp.json | jq -r .creator)
assert_eq "手动点火 200" "$CODE" "200"
assert_eq "点火 creator 标记" "$CREATOR" "cron:mvp3-minutely"

echo "  （等真调度器到点触发，≤75s…）"
FIRED=0
for i in $(seq 1 38); do
  if curl -sk "$BASE/api/tasks/mvp3-cron" | jq -e '.[] | select(.creator=="cron:mvp3-minutely" and .task_id>1)' >/dev/null 2>&1; then
    FIRED=1; break
  fi
  sleep 2
done
assert_eq "真调度器到点触发" "$FIRED" "1"
assert_eq "清理 cron（防夜里刷屏）" "$(code_of -X DELETE "$BASE/api/crons/mvp3-minutely")" "200"

echo "== 幕2 webhook：签名与触发 =="
ensure_wf <<'EOF'
{"name":"mvp3-hook","webhooks":[{"repo":"Edward-258/code-pipeline","branch":"master","secret":"mvp3-secret"}],
 "stages":[{"name":"s","jobs":[{"name":"j","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"echo hooked"}]}}]}]}
EOF
BODY='{"ref":"refs/heads/master","repository":{"full_name":"Edward-258/code-pipeline"}}'
SIG=$(sig_of "$BODY" "mvp3-secret")
assert_eq "对签触发" "$(code_of -X POST "$BASE/api/webhook/mvp3-hook" -H "X-Hub-Signature-256: $SIG" -H "X-GitHub-Event: push" -H 'Content-Type: application/json' -d "$BODY")" "200"
assert_has "响应 triggered" "$(cat /tmp/mvp3-resp.json)" '"triggered":true'
assert_eq "错签 401" "$(code_of -X POST "$BASE/api/webhook/mvp3-hook" -H "X-Hub-Signature-256: sha256=deadbeef" -H "X-GitHub-Event: push" -d "$BODY")" "401"
BADBODY='{"ref":"refs/heads/dev","repository":{"full_name":"Edward-258/code-pipeline"}}'
BADSIG=$(sig_of "$BADBODY" "mvp3-secret")
CODE=$(code_of -X POST "$BASE/api/webhook/mvp3-hook" -H "X-Hub-Signature-256: $BADSIG" -H "X-GitHub-Event: push" -d "$BADBODY")
assert_eq "分支不匹配 200" "$CODE" "200"
assert_has "但不触发" "$(cat /tmp/mvp3-resp.json)" '"triggered":false'

echo "== 幕3 审批：all 与 any =="
ensure_wf <<'EOF'
{"name":"mvp3-approve-all","stages":[
  {"name":"gated","manual_exec":{"enabled":true,"policy":"all","approvers":["alice","bob"]},"jobs":[
    {"name":"j","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"echo approved"}]}}]}]}
EOF
TA=$(curl -sk -X POST "$BASE/api/workflows/mvp3-approve-all/run" | jq -r .task_id)
TAF=$(wait_status mvp3-approve-all "$TA" pause 20)
assert_eq "all 闸门 pause" "$(echo "$TAF" | jq -r .status)" "pause"
assert_eq "策略随快照进任务" "$(echo "$TAF" | jq -r '.stages[0].manual_exec.policy')" "all"
assert_eq "alice 批（未满足）" "$(code_of -X POST "$BASE/api/tasks/mvp3-approve-all/$TA/approve" -H 'Content-Type: application/json' -d '{"approver":"alice"}')" "200"
assert_has "satisfied=false" "$(cat /tmp/mvp3-resp.json)" '"satisfied":false'
assert_eq "仍在 pause" "$(echo "$(curl -sk "$BASE/api/tasks/mvp3-approve-all/$TA")" | jq -r .status)" "pause"
assert_eq "bob 批（满足）" "$(code_of -X POST "$BASE/api/tasks/mvp3-approve-all/$TA/approve" -H 'Content-Type: application/json' -d '{"approver":"bob"}')" "200"
assert_has "satisfied=true" "$(cat /tmp/mvp3-resp.json)" '"satisfied":true'
TAF2=$(wait_status mvp3-approve-all "$TA" passed 40)
assert_eq "all 续跑 passed" "$(echo "$TAF2" | jq -r .status)" "passed"

ensure_wf <<'EOF'
{"name":"mvp3-approve-any","stages":[
  {"name":"gated","manual_exec":{"enabled":true,"policy":"any","approvers":["carol","dave"]},"jobs":[
    {"name":"j","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"echo approved"}]}}]}]}
EOF
TB=$(curl -sk -X POST "$BASE/api/workflows/mvp3-approve-any/run" | jq -r .task_id)
wait_status mvp3-approve-any "$TB" pause 20 >/dev/null
assert_eq "any 一人即过" "$(code_of -X POST "$BASE/api/tasks/mvp3-approve-any/$TB/approve" -H 'Content-Type: application/json' -d '{"approver":"dave"}')" "200"
assert_has "satisfied=true" "$(cat /tmp/mvp3-resp.json)" '"satisfied":true'
TB2=$(wait_status mvp3-approve-any "$TB" passed 40)
assert_eq "any 续跑 passed" "$(echo "$TB2" | jq -r .status)" "passed"

echo "== 幕4 兼容 release：老图纸原端点 =="
ensure_wf <<'EOF'
{"name":"mvp3-release","stages":[
  {"name":"gated","manual_exec":{"enabled":true},"jobs":[
    {"name":"j","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"echo legacy"}]}}]}]}
EOF
TC=$(curl -sk -X POST "$BASE/api/workflows/mvp3-release/run" | jq -r .task_id)
wait_status mvp3-release "$TC" pause 20 >/dev/null
assert_eq "老图纸 release 200" "$(code_of -X POST "$BASE/api/tasks/mvp3-release/$TC/stages/gated/release")" "200"
TC2=$(wait_status mvp3-release "$TC" passed 40)
assert_eq "release 续跑 passed" "$(echo "$TC2" | jq -r .status)" "passed"

echo ""
echo "=================================="
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ] && echo "阶段三验收全绿 ✅" || echo "阶段三验收有红 ❌"
exit "$fail"
