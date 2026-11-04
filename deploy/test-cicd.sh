#!/usr/bin/env bash
# 核心 CI/CD 验收：步骤化 + 真构建 + junit 门禁
# 幕0 旧图纸兼容（Scripts → 隐式 main）
# 幕1 密封构建：shell 造 Dockerfile → docker_build 构建+推 registry:2 → 临时 Job 拉起该镜像闭环
#    （默认跳过——需节点 daemon.json 放行 <nodeIP>:30500，补丁 deploy/fix-daemon-insecure.sh 待批；
#      构建产物若转 ACR 则此幕改写。MP_ACT1=1 启用）
# 幕2 junit 门禁：红（1/3 失败 → job failed）与绿（全过 → passed + TestResult 落库）
# 幕3 失败路径：步骤失败即停（exit 透传）→ 后续步骤 skipped → stage2 刹车 skipped
# 幕4 git 步骤实战 + ACR runner 链路：克隆公开仓库；git 步骤强制引擎选 pipeline-runner
#    （busybox 无 git，成功即证明 kubelet 经 VPC 端点+acr-credential 拉到 ACR 私有镜像）
# 附加：分步日志端点（终态回档案）
# 前置：pipeline-runner 已推 ACR（deploy/build-runner.sh，chart values runner.image 同步）
# Usage: bash deploy/test-cicd.sh   （可用 MP_BASE/MP_CTX/REG/MP_ACT1 覆盖）
set -euo pipefail

BASE="${MP_BASE:-https://pipeline.47.113.177.198.nip.io}"
CTX="${MP_CTX:-k3s-heyuan}"
REG="${REG:-http://registry.47.113.177.198.nip.io}"

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
assert_eq() { if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want=$3 got=$2"; fi }
assert_has() { if echo "$2" | grep -q "$3"; then ok "$1"; else bad "$1: missing '$3' in: $(echo "$2" | head -c 200)"; fi }
ensure_wf() { # JSON 走 stdin，名字从里面取
  local body; body=$(cat)
  # §9.7 护栏：手拼 body 多/少一个括号要让脚本当场炸亮，不能变成静默的错断言
  if ! name=$(echo "$body" | jq -er .name); then
    echo "❌ ensure_wf: invalid JSON body:"; echo "$body" | head -5; exit 1
  fi
  curl -sk -X DELETE "$BASE/api/workflows/$name" >/dev/null || true
  echo "$body" | curl -sk -X POST "$BASE/api/workflows" -d @- >/dev/null
}
run_wf()    { curl -sk -X POST "$BASE/api/workflows/$1/run" | jq -r .task_id; }
task()      { curl -sk "$BASE/api/tasks/$1/$2"; }
field()     { echo "$1" | jq -r "$2"; }
wait_status() { # wf tid want_status max_s
  local T=""
  for i in $(seq 1 "${4:-60}"); do
    T=$(task "$1" "$2")
    [ "$(field "$T" .status)" = "$3" ] && break
    sleep 2
  done
  echo "$T"
}

echo "== 幕0 旧图纸兼容：Scripts → 隐式 step main =="
ensure_wf <<'EOF'
{"name":"cicd-legacy","stages":[{"name":"s","jobs":[
  {"name":"j","type":"freestyle","spec":{"image":"busybox","scripts":["echo old-style ok"]}}]}]}
EOF
T=$(wait_status cicd-legacy "$(run_wf cicd-legacy)" passed 30)
assert_eq "task passed" "$(field "$T" .status)" "passed"
assert_eq "隐式步骤名" "$(field "$T" '.stages[0].jobs[0].steps[0].name')" "main"
assert_eq "步骤状态"   "$(field "$T" '.stages[0].jobs[0].steps[0].status')" "passed"
assert_has "步骤日志落库" "$(field "$T" '.stages[0].jobs[0].steps[0].log')" "old-style ok"

echo "== 幕1 密封构建：shell 造 Dockerfile → docker_build → 临时 Job 拉起产物 =="
if [ "${MP_ACT1:-0}" != "1" ]; then
  echo "  ⏭️ 跳过（MP_ACT1=1 启用；需节点 daemon.json 放行 <nodeIP>:30500，补丁待批/产物去留待定）"
else
ensure_wf <<'EOF'
{"name":"cicd-build","stages":[{"name":"build","jobs":[
  {"name":"maker","type":"freestyle","spec":{"steps":[
    {"name":"prepare","type":"shell","script":"printf 'FROM busybox\\nRUN echo built-by-mini-pipeline > /whoami\\n' > Dockerfile && cat Dockerfile"},
    {"name":"image","type":"docker_build","image":"mini-demo","tag":"0.1.0"}]}}]}]}
EOF
TID=$(run_wf cicd-build)
T=$(wait_status cicd-build "$TID" passed 90)
assert_eq "task passed"        "$(field "$T" .status)" "passed"
assert_eq "步骤1 prepare"      "$(field "$T" '.stages[0].jobs[0].steps[0].status')" "passed"
assert_eq "步骤2 docker_build" "$(field "$T" '.stages[0].jobs[0].steps[1].status')" "passed"
S1=$(field "$T" '.stages[0].jobs[0].steps[0].start_time'); E1=$(field "$T" '.stages[0].jobs[0].steps[0].end_time')
if [ -n "$S1" ] && [ "$S1" != "0" ] && [ "$E1" != "0" ]; then ok "步骤起止时间 ($S1→$E1)"; else bad "步骤起止缺失"; fi
assert_has "prepare 日志含 Dockerfile" "$(field "$T" '.stages[0].jobs[0].steps[0].log')" "FROM busybox"

TAGS=$(curl -s "$REG/v2/mini-demo/tags/list")
assert_has "镜像进 registry:2" "$TAGS" "0.1.0"

kubectl --context "$CTX" -n pipeline delete job mini-demo-verify >/dev/null 2>&1 || true
kubectl --context "$CTX" -n pipeline create job mini-demo-verify --image=localhost:5000/mini-demo:0.1.0 -- /bin/cat /whoami >/dev/null
for i in $(seq 1 30); do
  kubectl --context "$CTX" -n pipeline wait --for=condition=complete job/mini-demo-verify --timeout=0 >/dev/null 2>&1 && break
  sleep 2
done
if kubectl --context "$CTX" -n pipeline wait --for=condition=complete job/mini-demo-verify --timeout=0 >/dev/null 2>&1; then
  OUT=$(kubectl --context "$CTX" -n pipeline logs job/mini-demo-verify)
  assert_has "产物镜像可拉起(localhost:5000 链路闭环)" "$OUT" "built-by-mini-pipeline"
else
  bad "mini-demo-verify job 未完成"
fi
kubectl --context "$CTX" -n pipeline delete job mini-demo-verify >/dev/null 2>&1 || true
fi

echo "== 幕2 junit 门禁：红（66.7% < 100%）与绿（100%） =="
ensure_wf <<'EOF'
{"name":"cicd-test","stages":[{"name":"test","jobs":[
  {"name":"runner","type":"freestyle","spec":{"steps":[
    {"name":"gen","type":"shell","script":"printf '<?xml version=\"1.0\"?><testsuite name=\"t\" tests=\"3\" failures=\"1\" errors=\"0\" skipped=\"0\" time=\"0.02\"></testsuite>' > T.xml && cat T.xml"},
    {"name":"junit","type":"junit_report","report_glob":"*.xml"}]}}]}]}
EOF
T=$(wait_status cicd-test "$(run_wf cicd-test)" failed 60)
assert_eq "task failed（门禁翻红）"   "$(field "$T" .status)" "failed"
assert_eq "步骤 gen passed"          "$(field "$T" '.stages[0].jobs[0].steps[0].status')" "passed"
assert_eq "步骤 junit failed"        "$(field "$T" '.stages[0].jobs[0].steps[1].status')" "failed"
assert_has "门禁错误注明 66.7%"      "$(field "$T" '.stages[0].jobs[0].steps[1].error')" "66.7%"
assert_eq "TestResult.tests"         "$(field "$T" '.stages[0].jobs[0].steps[1].test_result.tests')" "3"
assert_eq "TestResult.failures"      "$(field "$T" '.stages[0].jobs[0].steps[1].test_result.failures')" "1"

ensure_wf <<'EOF'
{"name":"cicd-test-green","stages":[{"name":"test","jobs":[
  {"name":"runner","type":"freestyle","spec":{"steps":[
    {"name":"gen","type":"shell","script":"printf '<?xml version=\"1.0\"?><testsuite name=\"t\" tests=\"3\" failures=\"0\" errors=\"0\" skipped=\"0\" time=\"0.02\"></testsuite>' > T.xml"},
    {"name":"junit","type":"junit_report","report_glob":"*.xml"}]}}]}]}
EOF
T=$(wait_status cicd-test-green "$(run_wf cicd-test-green)" passed 60)
assert_eq "绿版 task passed" "$(field "$T" .status)" "passed"
assert_eq "绿版 TestResult"  "$(field "$T" '.stages[0].jobs[0].steps[1].test_result.tests')" "3"

echo "== 幕3 失败路径：exit 透传 + 失败即停 + stage 刹车 =="
ensure_wf <<'EOF'
{"name":"cicd-fail","stages":[
  {"name":"build","jobs":[{"name":"j","type":"freestyle","spec":{"steps":[
    {"name":"boom","type":"shell","script":"echo gonna-die >&2; exit 3"},
    {"name":"never","type":"shell","script":"echo SHOULD-NOT-RUN"}]}}]},
  {"name":"deploy","jobs":[{"name":"d","type":"freestyle","spec":{"image":"busybox","scripts":["echo deployed"]}}]}]}
EOF
FTID=$(run_wf cicd-fail)
T=$(wait_status cicd-fail "$FTID" failed 60)
assert_eq "task failed"          "$(field "$T" .status)" "failed"
assert_eq "boom failed"          "$(field "$T" '.stages[0].jobs[0].steps[0].status')" "failed"
assert_eq "boom 退出码=3"        "$(field "$T" '.stages[0].jobs[0].steps[0].error')" "exit=3"
assert_eq "never skipped"        "$(field "$T" '.stages[0].jobs[0].steps[1].status')" "skipped"
assert_eq "stage1 failed"        "$(field "$T" '.stages[0].status')" "failed"
assert_eq "stage2 刹车 skipped"  "$(field "$T" '.stages[1].status')" "skipped"

echo "== 幕4 git 步骤实战 + ACR runner 拉取链路 =="
ensure_wf <<'EOF'
{"name":"cicd-git","stages":[{"name":"s1","jobs":[
  {"name":"clone","type":"freestyle","spec":{"steps":[
    {"name":"fetch","type":"git","repo":"https://github.com/octocat/Hello-World.git","branch":"master"},
    {"name":"show","type":"shell","script":"ls; head -3 README"}]}}]}]}
EOF
TID=$(run_wf cicd-git)
# 趁 Job 活着抓 spec：镜像必须是 ACR VPC 路径 + 自挂 acr-credential（Deployment 的 secret 不继承给 Job）
sleep 3
J=$(kubectl --context "$CTX" -n pipeline get jobs -o name 2>/dev/null | grep "mp-${TID}-" | head -1 || true)
if [ -n "$J" ]; then
  SPEC=$(kubectl --context "$CTX" -n pipeline get "$J" -o jsonpath='{.spec.template.spec.containers[0].image}{"  pullSecret="}{.spec.template.spec.imagePullSecrets[0].name}')
  assert_has "Job 用 ACR pipeline-runner" "$SPEC" "pipeline-runner:0.1.0"
  assert_has "Job 自挂 acr-credential"    "$SPEC" "acr-credential"
else
  ok "(Job 已跑完没抓到活体——步骤成功即证明 ACR 拉取)"
fi
T=$(wait_status cicd-git "$TID" passed 90)
assert_eq "task passed"      "$(field "$T" .status)" "passed"
assert_eq "fetch passed"     "$(field "$T" '.stages[0].jobs[0].steps[0].status')" "passed"
assert_has "git clone 日志"  "$(field "$T" '.stages[0].jobs[0].steps[0].log')" "Cloning"
assert_has "show 读到 README" "$(field "$T" '.stages[0].jobs[0].steps[1].log')" "Hello World!"

echo "== 附加：分步日志端点（终态回档案） =="
# ⚠️ 必须用 cicd-fail 自己的 FTID：两 workflow 的 counter 各自独立递增，
# 历史上拿幕4 cicd-git 的 task_id 来查导致一直读到旧引擎时代的化石档案（假红灯三连）
LOGS=$(curl -sk "$BASE/api/tasks/cicd-fail/$FTID/log?stage=build&job=j")
assert_has "boom 步骤日志（stderr 混流）" "$LOGS" "gonna-die"
assert_has "never 步骤收口" "$LOGS" "step never"

echo
echo "PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ]
