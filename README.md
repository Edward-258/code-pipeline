# mini-pipeline（Zadig 微型复现）

以 Zadig 为蓝本的微型发布工作流引擎：**图纸/实例分离 + stage 状态机 + durable 暂停 + Job=K8s Job**。
单体 Go 服务（gin + mongo-driver + client-go），引擎本体跑在 k3s 里（helm chart 部署），API 与前端经 ingress-nginx 暴露。

> 学习性质的小范围复现：砍掉 RBAC/身份系统/多集群，代码宁直接不过度设计。
> 演示环境 `https://pipeline.47.113.177.198.nip.io` 跑在临时学习集群上（2026-11-16 到期），随时可能消失。

## 已实现的能力

### 工作流模型

- **图纸/实例分离**：`workflow` 集合存图纸，run 时把整棵定义快照进 `workflow_task`——之后改图纸不影响在跑/已跑的实例
- **三级树**：stage → job → step；任务 API 返回整棵状态树，每步自带 status/起止时间/日志/TestResult
- **状态机**：stage 内 job 可并行可串行；聚合**最差者胜**（cancelled > failed > pause > running > passed > created > skipped）；失败刹车——串行剩余 job 置 skipped、后续 stage 全 skipped，并行=全员跑完再聚合

### 执行引擎

- **Job = K8s Job**：主控只创建/轮询（2s）/收割/清理，不执行脚本；顺序铁律 Run → SaveInfo → Clean（先收割再销毁）
- **durable 暂停**（灵魂）：闸门 stage = pause 落库 + goroutine 直接退出；kill 引擎 Pod 档案不丢，release/approve 后续跑；pause 是无需看护人的稳态，跨重启存活
- **启动宣判**（Recover）：进程重启后遗留的 created/queued/running 孤儿档案宣判死亡 + 清泄漏 K8s Job；pause 不碰
- **任务队列**：1s 派发器 + CAS 条件更新抢占（防双派）；全局并发上限 `MAX_RUNNING_TASKS`（默认 3，0=只收尾）；同 workflow 串行；pause 不占名额；task_id FIFO
- **取消**：`DELETE /api/tasks/:name/:id`——活任务 close(channel) 广播、执行循环一拍收口（202）；pause/queued 纯数据收口（200）；树级级联；终态 409
- **单 job 重试**：`spec.retry` + 化石现场清理 + Pod env `MP_ATTEMPT`（重试剧本可确定性分支）
- **超时与错误策略**：`timeout_minutes`（默认 10，重活显式调大）+ `error_policy` stop/retry/ignore（图纸白名单校验）；策略只改控制流、失败照红
- **克隆与整任务重试**：clone = 快照→新号→全新重跑（GlobalContext 清零）；retry = 树内手术——failed/cancelled/skipped 刹车残留复活、passed 子树不动、Retry 预算归零

### 变量传递

- 图纸任意字段写 `{{.ns.key}}`，run 时 JSON marshal → 字符串替换 → unmarshal 渲染（`SetEscapeHTML(false)` 保脚本字符）
- 步骤用 `===MP=== OUTPUT k=v` banner 上报输出 → 引擎收割成 `<job>.<output>` 点分键进 GlobalContext → 下游 job（含 deploy 的镜像字段）引用渲染

### 触发方式

- **手动**：run / clone / retry 三个入口；cron、webhook、手动共用统一触发构造器 TriggerWorkflow（唯一"发号→快照→入队"路径）
- **定时**：手搓 5 段 crontab 匹配器（`*/n`/范围/列表/周日双写，坏表达式写入时 400）+ 整分钟对齐调度器（truncate+sleep 不漂移）；`POST /api/crons/:name/fire` 手动点火免等钟点
- **webhook**：GitHub + Gitee push 事件；HMAC-SHA256 constant-time 验签（Gitee 兼明文 token；多配置任一验过即放行）；无签名仅在全部配置无 secret 时放行（显式裸奔模式）；repo 归一化（剥 `.git`/scheme/host）+ branch 匹配

### CI 步骤体系

- **五种步骤**：`shell` / `git`（--depth 1 + token 注入）/ `docker_build`（遥控节点 daemon 构建，推送目标三档见下"CD 与部署"）/ `junit_report`（通过率门禁，默认 100% 翻红）/ `archive`（tar.gz → GridFS 上传下载，32MB 上限）
- **bash driver + banner 协议**：引擎把 steps 编译成 POSIX sh 驱动脚本，Pod stdout 就是步骤状态信道（`===MP=== STEP-START/END` + `OUTPUT k=v`）——与 Zadig jobexecutor+ConfigMap 信息流等价但零新依赖零 RBAC；首行 `exec 2>&1` 单流化（根治 containerd 双 copier 乱序）、EXIT trap 补发末班 banner
- **分步日志**：`GET /api/tasks/:wf/:id/log?stage=&job=`——活任务透传 Pod 日志尾部，终态回档案按 banner 切段（64KB 保尾截断）
- **镜像三档**：`spec.image` > runner 0.2.0（大满贯单镜像：alpine + git/docker-cli/curl/make/jq + **golang 1.27 工具链**，自动档通吃免"现场选镜像"）> busybox

### CD 与部署

- **环境集合**（env = 名字 → namespace）：deploy job 的靶标映射，CRUD 全路径
- **deploy job**：不起 K8s Job 不挂 docker.sock——引擎 client-go StrategicMergePatch 直改 Deployment 镜像字段（只动目标容器），然后轮询 rollout（observedGeneration 追上 + 新副本齐 + 无不可用副本）；`wait_rollout=false` 专为"自举发布自己"防绞杀
- **镜像仓库管理**（2026-11-07，对照 Zadig registry_namespace 的极简版）：registry = 推送目标资源（地址/凭证/默认标记）CRUD；docker_build 推送目标三档解析（driver 编译前烘焙，凭证不落任务档案，retry/clone 拿当前值）：

  | 优先级 | 图纸写法 | 行为 |
  |---|---|---|
  | 1 | 显式 `registry: <名>` | 推到该仓库，凭证随脚本 `docker login`（--password-stdin + 临时 DOCKER_CONFIG，不写回节点） |
  | 2 | 裸名（无仓库前缀）+ 存在默认仓库 | 跟随 `is_default` 记录推送（⚠️ 设默认会改变所有裸名图纸的推送目标） |
  | 3 | 其余 | 原行为：全名直推（/root/.docker 兜底）/ 无默认时裸名推集群内置 registry:2 |
- **密封构建链路**：shell 造 Dockerfile → docker_build 推节点 registry:2（NodePort 30500）→ 临时 Job 经 kubelet `localhost:5000` mirror 拉起产物运行

### 协作与通知

- **审批**：闸门 stage 配 `approvers` + `policy`（any/all）；`POST …/approve` 记审批（同名重批覆盖）→ 满足策略自动放行，与 release 同锁同闸门位；老图纸空名单 = 一条审批即过 ≡ release
- **release**：`POST …/stages/:stage/release` 翻闸门续跑（releaseMu 防双击）
- **IM 通知**：notify 集合 CRUD；任务终态 fire-and-forget（1 重试，失败只记日志）；飞书/钉钉双报文 + 双平台加签；纯数据取消不发通知（无看护人在场）

### 前端（React + nginx 独立 Pod，有真执行门）

- React 18 + TypeScript(strict) + antd 5（Vite 构建），独立 nginx 镜像跑 Pod：ingress 统一入口，nginx 反代 `/api`、`/healthz` 到引擎（envsubst 模板 + 运行时 resolver）
- **画布式编辑器**（火山引擎持续交付同款语义）：横 = stage 串行时间轴（从左到右），竖 = stage 内 job 堆叠（并行=竖排、串行=↓ 链）；job 压成紧凑条框——悬停出 ⊕ 插 job、红框 = 配置校验错（实时），点击开右侧抽屉编辑（freestyle/deploy 二选一、**镜像自动档**——引擎按步骤自动选 runner/busybox、retry/超时/错误策略、deploy 靶标 env 下拉、5 种步骤表单含 shell 本地脚本导入）；列间分界线 ⊕ 插 stage；列底审批闸门条（any/all + 审批人）；+ JSON 双模逃生舱；保存走 PUT（真更新，非删除重建）
- 任务详情三级树 + 分步日志抽屉 + cancel/retry/clone/approve/release；环境/镜像仓库/通知/定时完整 CRUD（编辑/启用开关/手动点火）
- 质量门：tsc strict 零错构建 + jsdom 真执行冒烟（React 真挂载/真路由切换/真引擎数据）——"字符串在位"≠"脚本活着"（曾配套的四门 bundle 语法门已随验收脚本存档 tag acceptance-suite）

### 自举彩蛋（已活体闭环）

`deploy/bootstrap-workflow.json`：克隆本仓库 → go test 门禁 → docker_build 推 ACR `tag=bootstrap`（走 registry 引用 `acr`，Pod 内自行 login）→ deploy job patch 引擎**自身**的 Deployment（`wait_rollout=false`）。
"用流水线发布流水线自身"——集群引擎现跑的就是它自建的 `:bootstrap` 镜像。`bootstrap-ensure.sh` 幂等落库 env self + registry acr。

## 架构

```
手动/前端 ─────┐
cron 整分钟 ───┼→ POST /api/... → TriggerWorkflow（发号→快照→入队 queued）
webhook 验签 ──┘                        │
                    派发器（1s 一拍，CAS 抢占，MAX_RUNNING_TASKS 上限）
                                        │
                              engine.Run（runStages 状态机）
                 ┌──────────────────────┼──────────────────────┐
           freestyle job              deploy job            闸门 stage
           K8s Job Pod          StrategicMergePatch      pause 落库 + goroutine 退出
           driver 脚本执行       + rollout 轮询           release/approve 续跑
           banner→三级树收割
                 │
     pkg/step 生成驱动脚本 · pkg/k8s 构建/轮询/刮日志 · pkg/engine/exec 编排三件套 Run→SaveInfo→Clean
```

代码结构（依赖全部单向向下，无环；模块名 `pipeline`）：

```
pkg/
├── config/        环境配置（leaf）
├── models/        领域模型（leaf）：结构体 + Status/WorstOf + 纯领域逻辑（ValidateWorkflow/CronMatch/Satisfied/EffectiveSteps/NeedsRunner）
├── repo/          MongoDB 持久层（counter 发号 / ack 全量写 / GridFS artifacts；集合句柄全包内私有）
├── step/          脚本执行层：driver.go 驱动脚本生成 / StepCtl 接口+工厂（shell/git/dockerbuild/junit/archive 一Ctl一文件）/ logs.go banner 解析·切分·收口
├── k8s/           K8s 访问层：client / JobName+BuildJob / PodLog+TailJobLog / PodExitInfo+DeleteOpts
├── engine/        编排核心：run.go 入口+taskCtx / stages.go stage 状态机 / jobs.go job 状态机 / finish.go 收口
│   │              task.go 快照 / trigger.go 统一触发（spawn 唯一构造序列）/ ops.go 任务运维操作面
│   │              queue.go 派发器 / recover.go 宣判 / vars.go 变量
│   ├── cancel/    取消注册表（Register/Request/Signalled/Tree）
│   ├── exec/      JobCtl 执行器（K8sJobCtl 重试+轮询 / DeployJobCtl rollout / registry.go 推送目标三档解析；ack 经 Ctx 由 engine 注入）
│   ├── notify/    IM 通知
│   └── cron/      定时调度（FireFunc 回调注入触发动作，不 import engine）
├── webhook/       git 签名/载荷/匹配纯逻辑
└── api/           gin handler 层：纯 HTTP 形状翻译（业务全在 engine/models/repo；handler 全包内私有）
main.go            仅装配：config→repo.Init→Recover→派发器→cron→gin
docker/            三镜像 Dockerfile：引擎 / runner / web（React+nginx）
deploy/            chart/ + build-push.sh + build-runner.sh + build-web.sh + bootstrap-*（验收脚本已退役存档 tag acceptance-suite）
frontend/          React 18 + TS + antd 5 前端（Vite 构建，e2e/ 下 jsdom 冒烟）
```

## 快速开始

```bash
# 一条命令起全家桶（mongo/引擎/registry:2 七件套/前端；详见下"镜像分发链路"）
helm install mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --create-namespace

# 引擎入口
curl -k https://pipeline.47.113.177.198.nip.io/healthz

# 建一条工作流并跑起来
curl -X POST -k https://pipeline.47.113.177.198.nip.io/api/workflows -H "Content-Type: application/json" -d @wf.json
curl -X POST -k https://pipeline.47.113.177.198.nip.io/api/workflows/demo/run
curl -k https://pipeline.47.113.177.198.nip.io/api/tasks/demo/1

# 浏览器直接开（前端五页签同源）
open https://pipeline.47.113.177.198.nip.io
```

wf.json 示例（freestyle 脚本形态）：

```json
{
  "name": "demo",
  "stages": [
    {
      "name": "build",
      "jobs": [
        { "name": "hello", "type": "freestyle", "spec": { "image": "busybox:1.36", "scripts": ["echo hello pipeline"] } }
      ]
    }
  ]
}
```

步骤化图纸（CI 形态，`spec.steps` 优先于 `scripts`；旧图纸的 scripts 会翻译成隐式步骤 "main"）：

```json
{
  "name": "ci-demo",
  "stages": [{
    "name": "test",
    "jobs": [{
      "name": "runner", "type": "freestyle",
      "spec": { "timeout_minutes": 30, "retry": 1, "error_policy": "retry", "steps": [
        { "name": "fetch", "type": "git", "repo": "https://github.com/octocat/Hello-World.git", "branch": "master" },
        { "name": "check", "type": "shell", "script": "ls; head -3 README", "outputs": { "MSG": "echo done" } },
        { "name": "junit", "type": "junit_report", "report_glob": "*.xml" },
        { "name": "pack", "type": "archive", "paths": ["dist/"] }
      ] }
    }]
  }]
}
```

下游引用上游输出：`{{.check.MSG}}`（点分键 `<job>.<output>`）。闸门审批：

```json
{ "name": "gate", "jobs": [], "manual_exec": { "enabled": true, "approvers": ["alice", "bob"], "policy": "all" } }
```

## API 速查

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` | 存活探针 |
| POST/GET | `/api/workflows` · GET/DELETE `/api/workflows/:name` | 图纸 CRUD（保存=删除重建） |
| POST | `/api/workflows/:name/run` | 手动触发（发号→快照→入队） |
| GET | `/api/tasks/:name?status=&creator=&from=&to=&page=&size=` | 任务列表，envelope `{items,total,page,size}`，task_id 降序，size ≤ 100 |
| GET | `/api/tasks/:name/:id` | 任务详情（stage→job→step 三级树 + TestResult） |
| DELETE | `/api/tasks/:name/:id` | 取消（活 202 / 纯数据 200 / 终态 409） |
| POST | `/api/tasks/:name/:id/clone` · `/retry` | 克隆重跑 / 整任务重试 |
| GET | `/api/tasks/:name/:id/log?stage=&job=` | 分步日志（活=Pod 尾部透传，终态=档案切段） |
| POST/GET | `/api/tasks/:name/:id/artifact/:step` | 产物上传（Pod 内 archive 步骤）/ 下载 |
| POST | `/api/tasks/:name/:id/stages/:stage/release` | 放行暂停 stage |
| POST | `/api/tasks/:name/:id/approve` | 原生审批（any/all 策略） |
| POST | `/api/webhook/:workflow` | git webhook（GitHub/Gitee，HMAC 验签） |
| POST/GET/PUT/DELETE | `/api/crons`（+ `/api/crons/:name/fire`） | 定时配置 + 手动点火 |
| POST/GET/PUT/DELETE | `/api/notifies` | IM 通知配置（飞书/钉钉） |
| POST/GET/DELETE | `/api/envs` | 部署目标环境 |
| POST/GET/PUT/DELETE | `/api/registries` | 镜像仓库管理（is_default 全局唯一；删默认回落"无默认"稳态=裸名推内置本地） |

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| MONGO_URI | mongodb://root:…@mongo.pipeline.svc.cluster.local:27017 | 集群内 svc DNS；本机调试先 port-forward 再覆盖为 localhost:27017 |
| HTTP_ADDR | :8080 | 监听地址 |
| K8S_NAMESPACE | pipeline | K8s Job 所在 namespace |
| K8S_CONTEXT | （空） | 仅本机调试：kubeconfig context（如 k3s-heyuan）；集群内留空走 InClusterConfig |
| RUNNER_IMAGE | crpi-…-vpc…/codepipeline-learning/pipeline-runner:0.2.0 | git/docker_build/archive 步骤的默认 Job 容器（chart values runner.image 注入，升 tag 不用重编引擎） |
| MAX_RUNNING_TASKS | 3 | 全局并发上限；0 = 停止派发只收尾（维护用） |
| MP_ENGINE_URL | http://mini-pipeline.<ns>.svc:8080 | Pod 内访问引擎（archive 上传用） |

## 验收

活体验收七门（四门 test-mvp1~4 共 100 断言 + 老三门 test-m3 / test-m3.5 / test-cicd）已随 0.7.0 引擎封版退役（2026-11-06，学习集群到期收尾），终版全绿基线固化在 git tag [`acceptance-suite`](https://github.com/Edward-258/code-pipeline/releases/tag/acceptance-suite)。恢复任意一门：

```bash
git checkout acceptance-suite -- deploy/
MP_BASE=https://pipeline.47.113.177.198.nip.io bash deploy/test-mvp1.sh    # 示例：第一门 35 断言
```

现存活动验收（单测不需要集群；smoke 需引擎可达，⚠️ 勿与本机引擎同跑——会抢同一条队列）：

```bash
go test ./pkg/... -count=1                            # 11 包单测（repo 类需 port-forward mongo）
(cd frontend && MP_BASE=… npm run smoke)              # jsdom 真执行冒烟 5 断言
```

## 镜像分发链路

三条链路互不干扰：

1. **引擎 + runner 镜像 → 阿里云 ACR 个人版**（独立实例，crpi- 前缀域名）：push 走公网端点（`deploy/build-push.sh <tag>` / `build-runner.sh <tag>` 本机直构直推；build 必须 `--provenance=false`——ACR 拒收 BuildKit 默认的 attestation 清单），pull 走 `-vpc` 端点（集群内网免公网流量），凭证 = chart 内 imagePullSecret `acr-credential`
2. **流水线产物镜像（docker_build 步骤）→ 集群内 registry:2**：节点 docker push `<nodeIP>:30500`（NodePort 仅内网），kubelet 经三节点 registries.yaml 的 `localhost:5000` mirror 无感知拉取；registry:2 七件套（本体 + joxit UI + 双 ingress + PVC）由本 chart `tags.registry` 渲染（2026-11-07 从 ArgoCD 收回管理权，PVC 经 `existingClaim` 原地认领，`helm.sh/resource-policy: keep` 护体）
3. **对外入口**：ingress-nginx + nip.io（`https://pipeline.47.113.177.198.nip.io`），零新增公网端口

改代码后的标准循环：

```bash
deploy/build-push.sh <new-tag>
helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=<new-tag> --set tags.engine=true --wait
```

## 与 Zadig 的对照

三条灵魂（及格线，缺一不算复现）：**图纸/实例分离**（task 内嵌当时的完整定义快照）、**durable 暂停**（暂停=落库+goroutine 退出，进程重启可恢复）、**Job = K8s Job**。

有意分叉：pause 不进启动宣判扫描集（Zadig 连 waiting-approve 一起取消；我们是无需看护人的稳态）；bash driver + banner 协议替代 jobexecutor 二进制 + ConfigMap 回传（信息流等价、路径更短）；轮询替代 watch；registry 管理复刻其单表+is_default 骨架但**删默认回落"无默认"稳态不做选举补位**（Zadig 的"无默认"= 部署失败必须有人站；我们的"无默认"= 裸名回落内置本地，是设计好的稳态）；砍掉 RBAC/身份系统/多集群。更多源码级对照见 AGENTS.md §6/§7。

## 工程上下文

集群状态、部署运维、20 条实测踩坑记录（keepalived VIP/flannel、ACR 推送、containerd 日志乱序、busybox wget 截断、双引擎抢队列……）、架构决断全档：**[AGENTS.md](AGENTS.md)**。
