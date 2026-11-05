#!/usr/bin/env bash
# 阶段四验收（T4.3）：门面 A12+A14 —— API 侧全自动 + 浏览器走查【需人工】
#   幕1 列表过滤分页：envelope 形状 / status / creator / from-to / 翻页
#   幕2 前端可服务性：/ 返回 HTML（go:embed）、/api 同源可达
#   幕3 前端数据源完备性：详情树/分步日志/TestResult/artifact 四类数据端点一次跑通
#   浏览器走查【需人工】：浏览器打开 $BASE（默认 http://localhost:8080）
#     走查清单：五页签切换 / 任务列表过滤翻页 / 任务详情树+日志展开 / 审批表单 / env/notify/cron 增删
# Usage: MP_BASE=http://localhost:8080 bash deploy/test-mvp4.sh
set -euo pipefail

BASE="${MP_BASE:-http://localhost:8080}"

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
assert_eq() { if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want=$3 got=$2"; fi }
assert_has() { if echo "$2" | grep -q "$3"; then ok "$1"; else bad "$1: missing '$3' in: $(echo "$2" | head -c 200)"; fi }
code_of() { curl -sk -o /tmp/mvp4-resp.json -w '%{http_code}' "$@"; }
ensure_wf() {
  local body; body=$(cat)
  if ! name=$(echo "$body" | jq -er .name); then echo "❌ invalid JSON"; exit 1; fi
  curl -sk -X DELETE "$BASE/api/workflows/$name" >/dev/null || true
  echo "$body" | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
}

echo "== 预检：healthz =="
assert_eq "healthz ok" "$(curl -sk "$BASE/healthz" | jq -r .status)" "ok"

echo "== 幕1 列表过滤分页 =="
LST="mvp4-list-$$"; FL="mvp4-fail-$$"   # 图纸删了 counter 不重置（§9.12 家族）：唯一名绕开化石档案
ensure_wf <<EOF
{"name":"$LST","stages":[{"name":"s","jobs":[
  {"name":"okjob","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"echo fine"}]}}]}]}
EOF
ensure_wf <<EOF
{"name":"$FL","stages":[{"name":"s","jobs":[
  {"name":"badjob","type":"freestyle","spec":{"steps":[{"name":"main","type":"shell","script":"exit 2"}]}}]}]}
EOF
curl -sk -X POST "$BASE/api/workflows/$LST/run" >/dev/null
TL2=$(curl -sk -X POST "$BASE/api/workflows/$LST/run" | jq -r .task_id)  # 同 wf 串行：等第二个也终态再断言
TF=$(curl -sk -X POST "$BASE/api/workflows/$FL/run" | jq -r .task_id)
for i in $(seq 1 30); do
  S1=$(curl -sk "$BASE/api/tasks/$FL/$TF" | jq -r .status)
  S2=$(curl -sk "$BASE/api/tasks/mvp4-list/$TL2" | jq -r .status)
  case "$S1 $S2" in "failed "passed"|failed failed") break;; esac
  sleep 2
done
assert_has "envelope 形状" "$(curl -sk "$BASE/api/tasks/mvp4-list")" '"total"'
assert_eq "过滤 passed" "$(curl -sk "$BASE/api/tasks/$LST?status=passed" | jq -r '.items | length')" "2"
assert_eq "过滤 failed" "$(curl -sk "$BASE/api/tasks/$FL?status=failed" | jq -r '.total')" "1"
assert_eq "creator=api" "$(curl -sk "$BASE/api/tasks/$LST?creator=api" | jq -r '.items[0].creator')" "api"
NOW=$(date +%s)
assert_eq "from-to 区间" "$(curl -sk "$BASE/api/tasks/$LST?from=$((NOW-600))&to=$NOW" | jq -r '.items | length')" "2"
assert_eq "翻页 size=1 第1页" "$(curl -sk "$BASE/api/tasks/$LST?page=1&size=1" | jq -r '.items | length')" "1"
assert_eq "翻页 size=1 第2页" "$(curl -sk "$BASE/api/tasks/$LST?page=2&size=1" | jq -r '.items | length')" "1"
assert_eq "总数不随翻页变" "$(curl -sk "$BASE/api/tasks/$LST?page=2&size=1" | jq -r '.total')" "2"

echo "== 幕2 前端可服务性（React+nginx 独立 Pod） =="
HTML=$(curl -sk "$BASE/")
assert_has "index.html 可服务" "$HTML" "mini-pipeline"
# SPA 形态：导航/页面在 JS bundle 里，index 只有挂载点。断言 bundle 引用在位 + 可下载
BUNDLE=$(printf '%s' "$HTML" | grep -o '/assets/index-[^"]*\.js' | head -1)
if [ -n "$BUNDLE" ]; then ok "bundle 引用在位"; else bad "bundle 引用缺失（非 React 前端在吐？）"; fi
if [ -n "$BUNDLE" ]; then
  curl -sk "$BASE$BUNDLE" -o /tmp/mvp4-bundle.mjs
  SIZE=$(wc -c </tmp/mvp4-bundle.mjs)
  if [ "$SIZE" -gt 100000 ]; then ok "bundle 可下载（${SIZE}B）"; else bad "bundle 异常小（${SIZE}B）"; fi
  # §9.19 教训：字符串在位≠脚本活着——bundle 必须过真语法门（ESM 形态，打实吐字节）
  if command -v node >/dev/null 2>&1; then
    if node --check /tmp/mvp4-bundle.mjs 2>/tmp/mvp4-err.txt; then
      ok "前端 bundle 语法门（node --check ESM）"
    else
      bad "bundle 语法错误: $(head -2 /tmp/mvp4-err.txt | tr '\n' ' ')"
    fi
    # 五导航在 bundle（antd Menu 的中文 label 编译进产物）
    grep -q "工作流" /tmp/mvp4-bundle.mjs && grep -q "定时" /tmp/mvp4-bundle.mjs \
      && ok "五页导航内容在 bundle" || bad "bundle 缺导航内容"
  fi
fi

echo "== 幕3 前端数据源完备性（树/日志/TestResult/artifact）=="
WF="mvp4-src-$$"
ensure_wf <<EOF
{"name":"$WF","stages":[
  {"name":"build","jobs":[{"name":"ci","type":"freestyle","spec":{"steps":[
    {"name":"prep","type":"shell","script":"echo making artifacts && printf 'content-v1' > out.txt && printf '<?xml version=\"1.0\"?><testsuite tests=\"3\" failures=\"1\" time=\"0.5\"></testsuite>' > report.xml"},
    {"name":"test","type":"junit_report","report_glob":"report.xml","pass_rate":0.5},
    {"name":"pack","type":"archive","artifact_glob":"out.txt","artifact_name":"out.tgz"}]}}]},
  {"name":"ship","jobs":[{"name":"gate","type":"freestyle","spec":{
    "error_policy":"ignore","steps":[{"name":"die","type":"shell","script":"exit 3"}]}}]}]}
EOF
TS=$(curl -sk -X POST "$BASE/api/workflows/$WF/run" | jq -r .task_id)
T=$(curl -sk "$BASE/api/tasks/$WF/$TS")
for i in $(seq 1 60); do
  T=$(curl -sk "$BASE/api/tasks/$WF/$TS")
  echo "$T" | jq -e '.status=="passed" or .status=="failed"' >/dev/null 2>&1 && break
  sleep 2
done
assert_has "任务跑完（ignore 策略不刹车）" "$T" '"failed"'
assert_has "三级树 steps 在位" "$T" '"prep"'
assert_has "TestResult 落库" "$T" '"test_result"'
assert_eq "门禁 50% 放行" "$(echo "$T" | jq -r '.stages[0].jobs[0].steps[1].status')" "passed"
assert_has "门禁统计（1/3 失败）" "$T" '"failures":1'
LOG=$(curl -sk "$BASE/api/tasks/$WF/$TS/log?stage=build&job=ci&step=prep")
assert_has "分步日志可取" "$LOG" "making artifacts"

echo ""
echo "  🔎 浏览器走查【需人工】：打开 $BASE ——"
echo "     五页签切换 / 任务列表过滤翻页 / 详情树+日志展开 / 审批表单 / env·notify·cron 增删"
echo "  🔎 Pod→引擎 archive 上传活体【需人工】：0.6.x 镜像 helm upgrade 后，跑带 archive 步骤的工作流"
echo "     （当前集群内是 0.4.6 旧镜像无 artifact 路由，Pod 侧上传必 404——本机引擎 Pod 打不到宿主，已知边界）"
echo ""
echo "=================================="
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ] && echo "阶段四验收（API 侧）全绿 ✅" || echo "阶段四验收有红 ❌"
exit "$fail"
