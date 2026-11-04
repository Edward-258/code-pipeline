#!/usr/bin/env bash
# 阶段二验收（T2.6）：CD 闭环 A6+A7
#   幕1 env CRUD：create / dup 409 / 命名 400 / 缺 ns 400 / list / delete / 404
#   幕2 deploy job 接线：图纸校验（缺 deploy_spec 400）+ 失败路径活体（env 指向不存在
#       ns → patch 报 not found——证明 env 解析与 patch 全链路，止步于预期边界）
#   幕3 archive 全链路（API 层）：真任务上传 tar.gz → 下载字节一致 / 缺失 404 / 超限 413
#   幕4 自举图纸在位：bootstrap 工作流 + env self（T2.5 ensure 的产物）
#   【需人工】MP_ROLE_LIVE=1：Role 升级后对引擎自身 Deployment 做同镜像 no-op patch
#       + wait_rollout 活体断言（不碰线上镜像版本，纯权限验证）
# 前置：本机引擎（§0.4 姿势）。Usage: MP_BASE=http://localhost:8080 bash deploy/test-mvp2.sh
set -euo pipefail

BASE="${MP_BASE:-http://localhost:8080}"
CTX="${MP_CTX:-k3s-heyuan}"

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
assert_eq() { if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want=$3 got=$2"; fi }
assert_has() { if echo "$2" | grep -q "$3"; then ok "$1"; else bad "$1: missing '$3' in: $(echo "$2" | head -c 200)"; fi }
code_of() { curl -sk -o /tmp/mvp2-resp.json -w '%{http_code}' "$@"; }
ensure_wf() {
  local body; body=$(cat)
  if ! name=$(echo "$body" | jq -er .name); then echo "❌ invalid JSON"; exit 1; fi
  curl -sk -X DELETE "$BASE/api/workflows/$name" >/dev/null || true
  echo "$body" | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
}
run_wf()    { curl -sk -X POST "$BASE/api/workflows/$1/run" | jq -r .task_id; }
task()      { curl -sk "$BASE/api/tasks/$1/$2"; }
field()     { echo "$1" | jq -r "$2"; }
wait_status() {
  local T=""
  for i in $(seq 1 "${4:-60}"); do
    T=$(task "$1" "$2")
    [ "$(field "$T" .status)" = "$3" ] && break
    sleep 2
  done
  echo "$T"
}

echo "== 预检：healthz =="
assert_eq "healthz ok" "$(curl -sk "$BASE/healthz" | jq -r .status)" "ok"

echo "== 幕1 env CRUD =="
assert_eq "create dev"   "$(code_of -X POST "$BASE/api/envs" -H 'Content-Type: application/json' -d '{"name":"mvp2-dev","namespace":"mvp2-dev-ns","desc":"验收环境"}')" "200"
assert_eq "dup 409"      "$(code_of -X POST "$BASE/api/envs" -H 'Content-Type: application/json' -d '{"name":"mvp2-dev","namespace":"x"}')" "409"
assert_eq "坏名字 400"    "$(code_of -X POST "$BASE/api/envs" -H 'Content-Type: application/json' -d '{"name":"Bad_Name","namespace":"x"}')" "400"
assert_eq "缺 ns 400"    "$(code_of -X POST "$BASE/api/envs" -H 'Content-Type: application/json' -d '{"name":"mvp2-x","namespace":""}')" "400"
assert_has "list 含 dev" "$(curl -sk "$BASE/api/envs")" "mvp2-dev-ns"
assert_eq "delete 200"   "$(code_of -X DELETE "$BASE/api/envs/mvp2-dev")" "200"
assert_eq "re-delete 404" "$(code_of -X DELETE "$BASE/api/envs/mvp2-dev")" "404"

echo "== 幕2 deploy job 接线 =="
assert_eq "缺 deploy_spec 400" "$(code_of -X POST "$BASE/api/workflows" -H 'Content-Type: application/json' -d '{"name":"mvp2-deploy-bad","stages":[{"name":"s","jobs":[{"name":"j","type":"deploy","spec":{"steps":[{"name":"x","type":"shell","script":"echo"}]}}]}]}')" "400"
ensure_wf <<'EOF'
{"name":"mvp2-deploy-miss","stages":[{"name":"ship","jobs":[
  {"name":"nginx","type":"deploy","deploy_spec":{
    "env":"mvp2-nowhere","deployment_name":"nginx","container_name":"web","image":"nginx:2.0","wait_rollout":false}}]}]}
EOF
curl -sk -X POST "$BASE/api/envs" -H 'Content-Type: application/json' -d '{"name":"mvp2-nowhere","namespace":"mvp2-no-such-ns"}' >/dev/null
TD=$(run_wf mvp2-deploy-miss)
TDF=$(wait_status mvp2-deploy-miss "$TD" failed 40)
assert_eq "deploy job 失败路径 failed" "$(field "$TDF" .status)" "failed"
assert_has "patch 尝试到达预期边界" "$(field "$TDF" '.stages[0].jobs[0].error')" "patch deployment"

echo "== 幕3 archive 全链路（API 层回环）=="
ensure_wf <<'EOF'
{"name":"mvp2-arch","stages":[{"name":"s","jobs":[
  {"name":"j","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"echo artifact-carrier"}]}}]}]}
EOF
TA=$(run_wf mvp2-arch)
TAF=$(wait_status mvp2-arch "$TA" passed 40)
assert_eq "载体任务 passed" "$(field "$TAF" .status)" "passed"

printf 'artifact-content-v1\n' > /tmp/mvp2-art.txt
tar czf /tmp/mvp2-art.tgz -C /tmp mvp2-art.txt
assert_eq "上传 200" "$(code_of -X POST "$BASE/api/tasks/mvp2-arch/$TA/artifact/out" -H 'Content-Type: application/gzip' --data-binary @/tmp/mvp2-art.tgz)" "200"
curl -sk "$BASE/api/tasks/mvp2-arch/$TA/artifact/out" -o /tmp/mvp2-art-back.tgz
if cmp -s /tmp/mvp2-art.tgz /tmp/mvp2-art-back.tgz; then ok "下载字节一致"; else bad "下载内容不一致"; fi
if tar tzf /tmp/mvp2-art-back.tgz | grep -q mvp2-art.txt; then ok "包内文件在位"; else bad "包内文件缺失"; fi
assert_eq "缺失 404" "$(code_of "$BASE/api/tasks/mvp2-arch/$TA/artifact/never-uploaded")" "404"
dd if=/dev/zero of=/tmp/mvp2-big.bin bs=1M count=33 2>/dev/null
assert_eq "超限 413" "$(code_of -X POST "$BASE/api/tasks/mvp2-arch/$TA/artifact/huge" -H 'Content-Type: application/octet-stream' --data-binary @/tmp/mvp2-big.bin)" "413"
rm -f /tmp/mvp2-art.txt /tmp/mvp2-art.tgz /tmp/mvp2-art-back.tgz /tmp/mvp2-big.bin

echo "== 幕4 自举图纸在位 =="
assert_has "bootstrap 工作流已落库" "$(curl -sk "$BASE/api/workflows/bootstrap")" '"build-ci"'
assert_has "env self 就绪" "$(curl -sk "$BASE/api/envs")" '"self"'

if [ "${MP_ROLE_LIVE:-0}" = "1" ]; then
  echo "== 【需人工】Role 升级后的活体 patch 断言 =="
  CUR_IMG=$(kubectl --context "$CTX" -n pipeline get deploy mini-pipeline -o jsonpath='{.spec.template.spec.containers[?(@.name=="engine")].image}')
  echo "  当前引擎镜像: $CUR_IMG（同镜像 no-op patch，不动线上版本）"
  ensure_wf <<EOF
{"name":"mvp2-deploy-live","stages":[{"name":"ship","jobs":[
  {"name":"noop","type":"deploy","deploy_spec":{
    "env":"self","deployment_name":"mini-pipeline","container_name":"engine","image":"$CUR_IMG","wait_rollout":true}}]}]}
EOF
  TL=$(run_wf mvp2-deploy-live)
  TLF=$(wait_status mvp2-deploy-live "$TL" passed 90)
  assert_eq "活体 no-op patch + rollout 通过" "$(field "$TLF" .status)" "passed"
else
  echo "== 【需人工】MP_ROLE_LIVE=1 启用活体 patch 断言（需先 helm upgrade 应用 T2.3 的 Role）=="
fi

echo ""
echo "=================================="
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ] && echo "阶段二验收（可活体部分）全绿 ✅" || echo "阶段二验收有红 ❌"
exit "$fail"
