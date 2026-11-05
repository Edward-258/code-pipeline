# AGENTS.md — mini-pipeline 项目上下文（给下一个会话的你）

> **工作台（2026-11-02 起迁移）**：WSL2 Ubuntu-24.04，本仓库位于 `/home/edward/projects/mini-pipeline`（git 历史完整迁入，`core.autocrlf=input`）。工具链：go 1.27.1（/usr/local/go，GOPROXY=goproxy.cn，2026-11-05 校对）、helm v3.19.0（~/.local/bin，2026-11-05 校对；⚠️ v3 的 upgrade **不复用**上次 --set，部署时所有 --set 必须显式带全）、kubectl（context `k3s-heyuan` 已配通）、jq、gcc、git（身份 Edward-258）、**Docker Desktop 29.x（2026-11-04 起，本机直构直推镜像）**。路径映射：Zadig 模板 → `/mnt/e/code pipeline学习模板(基于zadig项目)/zadig`；SSH key → `~/.ssh/heyuan81.pem`（/mnt/e 原件 drvfs 权限 ssh 不认，已拷贝 + chmod 600）；Windows 临时目录 → `/mnt/c/Temp/opencode`。`deploy/build-push.sh`/`build-runner.sh` 是本机构建脚本（docker build + push ACR，不再 ssh worker）。Windows 旧副本 `E:\code pipeline的小范围复现` 仅作存档，勿双向编辑。

> **远端（2026-11-05 更新）**：`origin` = https://github.com/Edward-258/code-pipeline.git（**public**——用户 2026-11-05 拍板"本来就是开源的"，明文密码暴露风险知情接受，**禁止翻转可见性**；详见 §9 记录 10）。推送走 Windows GCM 桥——credential.helper 已写进本仓库 local config（指向 `/mnt/c/Program Files/Git/mingw64/bin/git-credential-manager.exe`），裸 `git push` 即可；gh CLI 2.103.3 已装 `~/.local/bin` 并经 GCM token 授权——MVP 执行期走**真 GitHub PR 流**（任务=分支=PR，阶段收口在 GitHub 侧合并，见桌面 `mvp复现计划.md` §0.3）。

> 任何 AI 会话开始于此目录时，先读完本文件再动手。内容按"必读 → 速查 → 参考"排序。

## 0. 铁律

1. **Zadig 模板仓库绝对只读**：`E:\code pipeline学习模板(基于zadig项目)\zadig` 只能读代码做对照，改一个字节都不行。本项目（`E:\code pipeline的小范围复现`）是独立的复现项目。
2. 本项目是**学习性质的微型复现**，不是产品：砍掉了 RBAC/身份系统/多集群/弹性伸缩，代码宁可直接不要过度设计（审批/IM 通知/webhook/cron 已按极简形态落地，能力全景见 §2）。
3. 集群是临时的学习环境（master 节点 2026-11-16 到期），别在上面跑任何重要东西。
4. **三节点 /etc/rancher/k3s/registries.yaml 是手工合并出来的**（原有 docker.io/quay.io 加速 + 我们加的 localhost:5000 mirror），改它必须先 cat 看现状再合并，改完要滚动重启 k3s（master=`k3s`，worker=`k3s-agent`）。

## 1. 项目定位

以 Zadig（本地模板，HEAD `a1fe8122e`，v5.0.0-dev 线）为蓝本的微型发布工作流引擎：

- Go + gin + mongo-driver(v1) + client-go，单体服务，module 名 `pipeline`
- MongoDB 三集合：`workflow`（图纸）/ `workflow_task`（施工档案）/ `counter`（发号器）
- Job = K8s Job：主控只创建/轮询/清理，不执行脚本
- **引擎本体也跑在 k3s 里**（helm chart 部署，对齐 Zadig 的发行形态），API 经 ingress-nginx 暴露

MVP 已全量落地：变量/重试/取消/队列/CD deploy/archive/自举/cron/webhook/审批/通知/半吊子前端全部活体验收（能力全景见 §2；README.md 是对外能力说明书）。

**三条灵魂（及格线，缺一不算复现）**：
1. 图纸/实例分离，task 内嵌当时的完整定义快照
2. Stage 状态机 + **durable 暂停**（暂停=落库+goroutine 退出，进程重启可恢复）
3. Job = K8s Job

## 2. 已实现能力全景（截至 2026-11-06，集群镜像 0.7.0 / runner 0.1.1 / web 0.1.2）

> 引擎全部能力域均已落地并活体验收：四门（35+20+25+20=100 断言）+ frontend jsdom 冒烟 5 断言在 0.7.0 上全绿，`go test ./pkg/...` 11 包单测绿。历史任务账本（MVP 四阶段，PR #1–#31）见桌面 `mvp复现计划.md`（存档）；踩坑实录全在 §9。以下按能力域描述引擎**现在能做什么**。

### 里程碑时间线（全部落地）

M0 地基 → M1 第一口气（run→Job→轮询→ack→清理，引擎 helm 化进集群）→ M2 状态机（0.2.0）→ M3 durable 暂停（0.3.0）→ M3.5 启动宣判（0.3.1）→ 核心 CI/CD 步骤化（0.4.4，runner 进 ACR）→ M5 密封构建闭环（0.4.6）→ MVP 四阶段：执行基建（变量/重试/取消/队列）→ CD 闭环（env/deploy/archive/自举图纸）→ 触发与协作（cron/webhook/审批/通知）→ 门面（列表分页/前端）→ pkg 模块化分层（PR #31）。各阶段踩的坑与修法全部沉淀在 §9；详细验收标准见桌面 `mini-zadig-计划.md`。

### 引擎核心

- 三集合 `workflow`（图纸）/ `workflow_task`（施工档案）/ `counter`（发号器）；run = 发号 → 快照落库 → K8s Job 创建/轮询（2s）/收割/清理；ack 全量写（`_id` 自然键 `<wf>-<taskID>`）
- runStages 状态机：stage 内并行/串行 + WorstOf 最差者胜（cancelled(7)>failed(6)>pause(5)>running(4)>passed(3)>created(2)>skipped(1)）+ 失败刹车（串行剩余置 skipped / 并行全员跑完再聚合 / 后续 stage 全 skipped）
- durable 暂停：闸门落库 + goroutine 退出，kill 引擎 Pod 档案仍在；release 端点只翻 `ManualExec.Executed` 位（releaseMu 防双击，与 approve 同锁）
- 启动宣判 Recover（main.go 在 gin 起来之前跑）：created/queued/running 孤儿宣判死亡 + 清泄漏 Job；pause 稳态不碰（有意分叉自 Zadig：它连 waiting-approve 一起杀）。不变量：running 要么有活着的看护人，要么是终态
- 任务队列：1s ticker 扫 queued（task_id FIFO）→ CAS 抢占（repo.ClaimQueued 条件更新防双派）→ go Run；全局 `MAX_RUNNING_TASKS`（默认3，0=只收尾）+ 同 wf 串行；pause 不占名额；release/approve/cron/webhook 全走正常入队；Recover 扫描集含 queued（重启排空）
- 取消 DELETE /api/tasks/:name/:id：活任务 close(chan) 信号 202 一拍收口 / pause·queued·孤儿 CancelTree 纯数据 200 / 终态 409；cancelEntry closed 标志防二次 close panic
- K8s Job 名 `mp-<wf>-<taskID>-<stage>-<job>`（跨 wf 唯一，§9.14 的教训）

### 变量与重试

- renderVars：JSON marshal → `{{.ns.key}}` 替换（SetEscapeHTML(false) 保脚本字符）→ unmarshal；driver `===MP=== OUTPUT k=v` banner 收割 → GlobalContext（`<job>.<output>` 点分键）→ 下游 job Run 时渲染（锁内，防并行收割竞态）
- 单 job 重试：spec.Retry + resetForRetry（化石现场清理）+ Pod env `MP_ATTEMPT`（重试剧本可确定性分支）
- 超时与错误策略：spec.TimeoutMinutes（默认 10，重活显式调大——§9.18）+ spec.ErrorPolicy（stop/retry/ignore，图纸白名单校验）；**控制/上报分离**（策略只改控制流，失败照红）；task 尾态 WorstOf 如实收口
- clone（快照→新号→入队，creator=clone:<src>，GlobalContext 全新）/ retry（终态 409 守卫；resetForRetryRun 树内手术：failed/cancelled/**skipped 刹车残留**复活、passed 子树不动、Retry 预算归零）

### CD 闭环

- env 集合：name/namespace/desc，deploy job 的靶标映射（POST/GET/DELETE /api/envs，409/400/404 全路径）
- deploy job（CD 本体）：JobTypeDeploy + DeploySpec（env/dep名/容器名/镜像/wait_rollout 默认 true）；**不变量3：不起 K8s Job 不挂 sock，引擎 client-go StrategicMergePatch 直 patch**（只动目标容器）；rollout 轮询 = observedGeneration 追上 + updated 齐 + unavailable=0；wait_rollout=false = 自举防绞杀；DeploySpec 进变量渲染管线（image 可引上游 output）
- chart Role 已含 apps 组 deployments/statefulsets/daemonsets patch/update（活体生效，引擎 SA 可 patch 自身 Deployment）
- archive step：tar czf → curl 上传引擎 GridFS（bucket artifacts，键 `<wf>-<taskID>-<step>` 重跑覆盖，32MB 上限 413）→ GET 流式下载；Pod env 注入 MP_ENGINE_URL/MP_TASK_WF/MP_TASK_ID（curl -f 让上传失败翻红——busybox wget 截断教训见 §9.17）
- 自举闭环（活体已通，bootstrap-3 passed）：deploy/bootstrap-workflow.json（checkout→go test 门禁→docker_build 推 ACR tag=bootstrap）+ deploy job 打自身（wait_rollout=false）+ bootstrap-ensure.sh 落库（env self = pipeline ns）——集群引擎现跑的就是流水线自建的 `:bootstrap` 镜像，"用流水线发布流水线自身"
- deploy 单测手法：fake clientset + reactor 模拟 rollout 控制器追上（fake 无控制器行为）；pollInterval/rolloutWaitTimeout 做成 var 供测试缩短节拍

### 触发与协作

- cron：手搓 5 段 crontab 匹配器（*/n/范围/列表/周日双写，坏表达式写入时 400）+ 整分钟对齐调度器（truncate+sleep 不漂移）；POST /api/crons/:name/fire 手动点火（验收免等钟点）
- webhook：POST /api/webhook/:workflow；HMAC-SHA256 验签先行（GitHub X-Hub-Signature-256 / Gitee 双轨含明文 token；constant-time；多配置任一通过）；无签名仅当全部配置无 secret 放行（显式裸奔模式）；repo 归一（剥 .git/scheme/host）+ branch 匹配；creator=webhook:<repo>@<branch>
- **TriggerWorkflow 统一触发入口**：cron/webhook/手动共用唯一"发号→快照→入队"构造器（RunWorkflow 重构其上）
- 原生审批：ManualExec 扩 approvers/policy(any/all)/approvals；POST /api/tasks/:name/:id/approve（与 release 同锁同闸门位，同名重批覆盖 comment）；审批记录属于运行不入图纸快照（快照漏拷 approvers 曾让 all 静默变 any，已修）；老图纸空名单 = 一条审批即过 ≡ release
- 通知：notify 集合 CRUD + 任务终态 fire-and-forget 钩子（1 重试，失败只记日志）；飞书/钉钉双报文 + 双平台加签（两家 HMAC 约定不同，注释钉死）；**不对称文档化**：CancelTree 纯数据取消不发通知（无看护人在场）

### 门面

- 任务列表过滤分页：GET /api/tasks/:name?status=&creator=&from=&to=&page=&size= → **envelope {items,total,page,size}**（裸数组形状已废，消费方注意）；task_id 降序 + size 上限 100 + CountDocuments 总数
- workflow/env **PUT 端点已补**（PR #33）：PUT /api/workflows/:name、PUT /api/envs/:name——全量替换、路径名为准、name 不一致 400、created_at 从旧档带过来；前端"保存=删除重建"时代结束
- **React 前端**（2026-11-06 重构，PR #34–#36）：`frontend/`（Vite+TS strict+antd5，暗色 teal 侧边栏自成一体）；页面 = 工作流列表/**可视化四层编辑器**（stage 管道卡片+并行开关+审批闸门；job freestyle/deploy 二选一：镜像自动/自定义+retry/timeout/error_policy、deploy 靶标四项+env 下拉；5 种 step 表单含 **shell 本地脚本导入（FileReader）+手写**；JSON 双模逃生舱）/任务列表过滤分页/任务详情三级树（分步日志抽屉+cancel/retry/clone/approve/release）；api 层 typed client 对齐 24 端点、usePolling（setTimeout 链防重叠、终态停）
- 前端部署形态：**独立 nginx Pod**（多阶段 Dockerfile node build→nginx；/api、/healthz 反代引擎 service 经 envsubst 模板+resolv.conf 注入 resolver，§9.21），ingress 单条规则指前端 Pod（chart `tags.web`）；引擎 API-only（go:embed 已退役，main.go 无前端）
- 前端质量门：`npm run build`（tsc strict 零错）+ test-mvp4 幕2 **bundle ESM 语法门**（node --check 实吐字节+导航内容断言）+ `frontend/e2e/smoke.mjs` **jsdom 真执行冒烟**（5 断言：React 真挂载/五导航/真引擎数据/菜单真切换路由/API 可达；手法=vite 单 chunk 无 import 语句→new Function eval+window 全量桥接，§9.22）——"字符串在位"≠"脚本活着"（§9.19）

### CI 步骤实现要点（留档）

- driver 生成 `pkg/step/driver.go`：⚠️ POSIX sh——alpine 是 ash，无 local/[[ ]]/数组；**首行 `exec 2>&1` 单流化是切分确定性的地基**（§9.10）
- banner 解析/切分/收口 `pkg/step/logs.go`：ApplyStepBanners 幂等重放且**行中 banner 也认** / SliceStepLogs 按 START 切段（未知步骤进黑洞）/ FinalizeSteps：created→skipped、悬空 running→failed、AfterRun 门禁、64KB 保尾部截断
- StepCtl `pkg/step/stepctl.go`：git clone --depth 1 + token 注入 URL；docker_build 无 registry 前缀时加 `"$MP_NODE_REGISTRY"/`——**前缀段双引号在单引号外**（否则 sh 不展开，docker 收字面量报 exit=125）；junit 只认 testsuite 开标签属性、跳过 testsuites 包装层防双算
- JobCtl `pkg/engine/exec/k8s.go`：Run 轮询循环内顺手刮日志、SaveInfo **稳定探测**（连续等长兜尾部）；镜像三档 spec.image > runner > busybox；roundtrip 测试 `pkg/step/driver_roundtrip_test.go` 真 /bin/sh 跑→分离捕获断言单流不变量→banner 反解→finalize 全链

### 代码结构（2026-11-06 模块化重构后，依赖全部单向向下无环；模块名 `pipeline`）

`pkg/config/`(leaf 环境配置) → `pkg/models/`(leaf 领域模型：结构体＋Status/WorstOf＋纯领域逻辑 ValidateWorkflow/CronMatch/Satisfied/EffectiveSteps/NeedsRunner·WantsDockerSock) → `pkg/repo/`(mongo 持久层＋GridFS artifacts，集合句柄全包内私有) →
`pkg/step/`(脚本执行层：driver.go POSIX 驱动生成 / StepCtl 接口+工厂，shell/git/dockerbuild/junit/archive 一Ctl一文件全私有 / logs.go banner 解析·切分·收口——只 import models) →
`pkg/k8s/`(K8s 访问层：client.go 双模式 client / build.go JobName+BuildJob / log.go PodLog+TailJobLog / util.go PodExitInfo+DeleteOpts) →
`pkg/engine/`(编排核心：run.go Run 入口+taskCtx+ack / stages.go stage 状态机+闸门刹车谓词 / jobs.go job 状态机+变量渲染编排+输出收割 / finish.go 终态收口+通知 / task.go BuildTask 快照 / trigger.go 统一触发+spawn 唯一构造序列 / ops.go 任务运维操作面 CloneTask·RetryTask·CancelTask·ReleaseGate·RecordApproval·TaskLog+sentinel 错误 / queue.go 派发器 / recover.go 启动宣判 / vars.go 变量渲染) 下挂四个子包：
　`engine/cancel/`(取消注册表 Register/Request/Signalled/Tree/ErrCancelled)、`engine/exec/`(JobCtl 接口+Ctx 视图：k8s.go K8sJobCtl+重试+轮询、deploy.go DeployJobCtl+rollout；jobTimeout/pollInterval 在此；整树写回经 Ctx.Ack 由 engine 注入)、`engine/notify/`(IM 通知 NotifyOnFinish)、`engine/cron/`(定时调度 cron.Start(FireFunc)——触发动作回调注入，不 import engine) →
`pkg/webhook/`(git 签名/载荷/匹配纯逻辑层) → `pkg/api/`(gin handler 层：纯 HTTP 形状翻译——taskFromPath 样板单点、28 个 handler 全包内私有、业务全在 engine/models/repo；workflow/task 运维+approve+artifact+env/notify/cron CRUD+webhook 编排) → `main.go`(根，仅装配：config→repo.Init→engine.Recover→go exec 派发器→go cron.Start(engine.TriggerWorkflow)→gin)。
另有 `docker/`(引擎/runner/web 三镜像 Dockerfile) `deploy/chart/mini-pipeline/`(helm) `deploy/build-push.sh`/`build-runner.sh`/`build-web.sh`(本机构建) `deploy/test-mvp*.sh`(四门验收)＋`test-m3*.sh`/`test-cicd.sh`(老三门) `deploy/bootstrap-*`(自举) `frontend/`(React 前端，e2e/smoke.mjs jsdom 冒烟)。

### 验收资产（全部打集群引擎，单一引擎纪律见 §9.20）

- `MP_BASE=https://pipeline.47.113.177.198.nip.io bash deploy/test-mvp1.sh`（35 断言：变量/重试/取消/队列/clone/超时错误策略）
- `MP_ROLE_LIVE=1 MP_BASE=… bash deploy/test-mvp2.sh`（20 断言：env/deploy/archive，含引擎 SA patch 自身 Deployment 活体）
- `MP_BASE=… bash deploy/test-mvp3.sh`（25 断言：cron/webhook/审批/通知/release）
- `MP_BASE=… bash deploy/test-mvp4.sh`（20 断言：列表分页/前端 bundle ESM 语法门/日志）+ `cd frontend && MP_BASE=… npm run smoke`（jsdom 真执行 5 断言，§9.22）
- 老三门仍有效：test-m3.sh（durable 暂停 11，含 kill Pod）/ test-m3.5.sh（启动宣判 13）/ test-cicd.sh（核心 CI/CD 33）
- `go test ./pkg/... -count=1`：11 包单测（repo 类需 port-forward mongo）

## 3. 环境速查（全部实测可用）

| 项 | 值 |
|---|---|
| k3s master | 8.155.137.224（内网 172.29.237.251，control-plane+etcd，**2026-11-16 到期**） |
| k3s worker-1 | 8.155.128.91（内网 172.29.237.250） |
| k3s worker-2 | 8.155.128.81（内网 172.29.237.249）✅ 网络已修复（2026-11-01，见 §9 案例存档） |
| kubectl context | `k3s-heyuan` |
| MongoDB | ns `pipeline`，**ClusterIP**（chart 内置），本机调试走 `kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017` |
| registry:2 | **归 ArgoCD 管**（k3s-gitops `registry` app，2026-11-02 移交）。NodePort 30500 仅内网（节点 push / kubelet mirror 链路不变）；对外 `http://registry.47.113.177.198.nip.io`（ingress 反代；Docker Desktop 直连需配 insecure-registries）；前端 `http://registry-ui.47.113.177.198.nip.io`（joxit UI，同源代理 /v2/，可浏览/删镜像）。角色已定：引擎/runner 镜像走 ACR，**registry:2 = docker_build 步骤的产物仓库**（M5 收官验证：push `<nodeIP>:30500` + kubelet `localhost:5000` mirror 拉起产物闭环） |
| **ACR 个人版** | 引擎 + runner + 前端镜像分发仓库：独立实例 `crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com`（公网推送）+ `-vpc` 后缀变体（集群 VPC 拉取，免公网流量）。命名空间 `codepipeline-learning` 下三仓：`mini-pipeline`（引擎）、`pipeline-runner`（步骤执行器，chart values `runner.image`）、`mini-pipeline-web`（React 前端+nginx，chart values `web.image`）。用户名 `nick9188231182`，密码在 chart values `acr.password`。build 必须 `--provenance=false`（详见 §9.9）；老共享域名 `registry.cn-heyuan.aliyuncs.com` 不属于本实例（Forbidden Host），别用 |
| 引擎入口 | `https://pipeline.47.113.177.198.nip.io`（复用集群现成 nip.io+ingress-nginx 链路，SG 80/443 已放行） |
| 安全组 | `sg-f8z8wcoum4myexrjfr40`；**30017 已收掉**，现有 80/443/6443/22/3389 + 同组内网互通 |
| 集群存量 | argocd（ns argocd）、ingress-nginx（DaemonSet hostNetwork 双 worker）——都不是我们的，别动 |
| SSH | `ssh -i ~/.ssh/heyuan81.pem root@<节点IP>`，三台全通（集群操作直接 ssh，pem 是 /mnt/e 原件的拷贝） |
| aliyun CLI | 已配 AK/SK（profile default，查询时显式 `--RegionId cn-heyuan`）。仅云 API 查询用（如安全组），够不到集群本身 |

## 4. 已定稿的架构决断（不要再讨论）

1. **引擎跑在集群内**（2026-11-01 拍板，推翻更早的"本机跑"方案）：InClusterConfig + SA `pipeline-engine`（Role 仅 jobs/pods 权限）；代码保留 kubeconfig fallback 供本机调试（`K8S_CONTEXT=k3s-heyuan`）。
2. **部署形态对齐 Zadig = helm chart**：`deploy/chart/mini-pipeline`，`tags.{engine,mongo,registry}` 组件开关 + `connections.mongo.uri` 参数化（对照 Zadig chart 的 tags.mongodb / connections 模式）。⚠️ 本机 helm v3.19.0：upgrade **不复用**上次 --set 值——部署时所有 --set 必须显式带全。
3. **registry:2 提前建**（原 M5 组件）：镜像名统一 `localhost:5000/mini-pipeline:<tag>`，push 侧节点 docker 推 `127.0.0.1:30500`，pull 侧三节点 registries.yaml 把 `localhost:5000` mirror 到三个节点内网 IP:30500（不能只配 127.0.0.1——worker-2 回环 NodePort 不通，见 §9）。registry 存储会剥离 host 前缀，两边镜像名天然一致。（2026-11-02 起资源归 ArgoCD 管，见决断 #11；两条链路本身不变）
4. **镜像在本机构建直推 ACR**（2026-11-04 起本机 Docker Desktop 可用后改版）：`deploy/build-push.sh <tag>` = 本机 `docker build`（多阶段 golang:1.26-alpine，GOPROXY=goproxy.cn，.dockerignore 瘦上下文）→ push ACR 公网端点（`--provenance=false` 硬要求，见 §9.9）。历史：2026-11-03~04 曾走"scp worker-1 远程构建"链路（本机无 docker），ssh 僵尸会话坑频发，本机 engine 就位后退役；worker 上构建一去不复返。
5. **镜像构建走路线①**（M5 生效）：fat image（docker CLI）+ 挂宿主 `/var/run/docker.sock`；k3s 是 containerd，两个镜像仓库互不相认，registry:2 是必选项。
6. 状态获取用**轮询**（每 2s 查 K8s Job），不上 watch。
7. runner 不主动回调：**Pod 退出码 = job 结果**，日志用 API 拉。
8. 第一版不做队列/并发控制。
9. 挂 docker.sock 的 Pod ≈ 节点 root，仅限学习集群，fat image 永不推公网。
10. Mongo 密码明文写在 chart values 和 config/config.go——学习版接受（2026-11-05 起仓库已定 **public**，用户知情接受明文密码暴露风险并拍板维持公开，禁止翻转可见性，见 §9 记录 10；轮换 mongo/ACR 密码为可选卫生动作，集群 11-16 到期风险窗口有限）；ACR 密码 `acr.password` 同档处理。
11. **registry:2 归 ArgoCD 管**（2026-11-02）：资源清单进 k3s-gitops 仓库（`registry` app，app-of-apps 第 4 位，sync-wave 3），chart 的 `tags.registry` 默认 false 只留作全新环境 bootstrap。新增 ingress 反代 `registry.47.113.177.198.nip.io` 供本机直连（无认证无 TLS 公网可达，仅学习期接受）。helm→argocd 移交手法见 §9.8。
12. **引擎镜像分发走 ACR 个人版独立实例**（2026-11-03）：push = 公网端点（构建侧，节点或本地），pull = VPC 端点（`-vpc` 后缀，集群内网免公网流量）；凭证 = chart 内 imagePullSecret `acr-credential`（dockerconfigjson，节点 registries.yaml 不动、不重启 k3s，其它镜像拉取链路零影响）。**2026-11-04 扩展：runner 镜像同链路跟进**（`codepipeline-learning/pipeline-runner`，chart values `runner.image` → env `RUNNER_IMAGE` 注入引擎，升 tag 不用重编引擎）。⚠️ 引擎运行时创建的 Job Pod 必须**自带 imagePullSecrets acr-credential**（Deployment 挂的 secret 不会继承，engine/job.go buildJob 已内置）——dockerconfigjson 按 registry host 匹配，busybox 等 docker.io 公共镜像拉取不受影响。registry:2 角色已定 = docker_build 步骤的产物仓库（2026-11-04 M5 收官跑通），两条链路互不干扰。ACR 的坑见 §9.9。

## 5. 常用命令

```bash
# ===== 部署循环（改代码后的标准姿势；本机 Docker Desktop 直构直推） =====
# 1) 构建镜像进 ACR（--provenance=false 是 ACR 硬要求，脚本已内置；首次需 docker login 一次）
deploy/build-push.sh 0.7.0
# 2) 升级 release（helm v3 不复用上次 --set，所有 --set 显式带全）
helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=0.7.0 --set tags.engine=true --wait

# ===== runner 镜像（低频；runner 变更时跑，tag 须同步 chart values runner.image） =====
deploy/build-runner.sh 0.1.0

# ===== 验收（四门 + jsdom 冒烟，全部打集群引擎；⚠️ 勿与本机引擎同跑——双引擎抢队列 §9.20） =====
MP_BASE=https://pipeline.47.113.177.198.nip.io bash deploy/test-mvp1.sh                  # 35 断言
MP_ROLE_LIVE=1 MP_BASE=https://pipeline.47.113.177.198.nip.io bash deploy/test-mvp2.sh   # 20 断言（env/deploy/archive 活体）
MP_BASE=https://pipeline.47.113.177.198.nip.io bash deploy/test-mvp3.sh                  # 25 断言
MP_BASE=https://pipeline.47.113.177.198.nip.io bash deploy/test-mvp4.sh && (cd frontend && MP_BASE=https://pipeline.47.113.177.198.nip.io npm run smoke)   # 20+5 断言
bash deploy/test-cicd.sh              # 老核心 CI/CD 门 33 断言（幕1 默认跳过；MP_ACT1=1 全套）

# ===== git 远端（GitHub，public；PR 流见桌面 mvp复现计划.md §0.3） =====
git push   # GCM 凭证桥已配在仓库 local config，裸敲即可（首次会弹 Windows 登录窗）

# ===== 本机调试（引擎跑本机，连集群 mongo/创建集群 Job） =====
kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017   # 窗口A
export MONGO_URI="mongodb://root:mini-pipeline-2026@localhost:27017" K8S_CONTEXT=k3s-heyuan && go run .   # 窗口B
# （Windows/PS5.1 时代的引号坑全档在 §9；bash 下引号可靠，JSON 直接 -d 单引号串）

# ===== 集群 =====
kubectl --context k3s-heyuan get pods -n pipeline
kubectl --context k3s-heyuan get jobs -n pipeline        # 验收：mp-N-… 出现又消失

# ===== 直连集群里的 Mongo 看数据 =====
kubectl exec -i -n pipeline deploy/mongo -- mongosh -u root -p mini-pipeline-2026 --quiet <<'EOF'
db = db.getSiblingDB("pipeline")
printjson(db.getCollectionNames())
EOF

go test ./pkg/repo/ -v -run TestNextID   # 需先 port-forward mongo
```

## 6. 写代码时遵守的设计原则（从 Zadig 实读得出）

1. **暂停的实现**：`stage.Status = pause` 写 Mongo → goroutine 直接 return（不 sleep 不占线程）→ 人调 release API 标记 `ManualExec.Executed = true` → 任务重跑 → `RunStages` 跳过已 passed 的 stage → 闸门放行。暂停多久都不丢。
2. **资源与数据分离**：K8s 资源是消耗品（结束/暂停时清理），Mongo 记录是档案（跑完不删，只受管理员保留策略约束）。
3. **状态聚合最差者胜**：Zadig 原版 cancelled(7)>timeout(6)>failed(5)>pause(4)>reject(3)>passed(2)>unstable(1)>skipped(0)。**学习版已落地**（`models/status.go` 的 statusWeight/WorstOf）：cancelled(7)>failed(6)>pause(5)>running(4)>passed(3)>created(2)>skipped(1)——running 排在 passed 前（没跑完不许宣布成功），skipped 垫底（被刹车跳过的 job 不改变聚合结果）。
4. **ack() 全量写**：任务每前进一步把整棵树 ReplaceOne 写回，不做增量 patch。前提：`_id` 是插入前定好的自然键字符串（`<wf>-<taskID>`）——空 `_id` 插入会让 Mongo 生成 ObjectId 而结构体拿不回，ack 从此静默失效（M1 踩过）。
5. **变量渲染**：JSON marshal → 字符串 `{{.key}}` 替换 → unmarshal（M4 照抄 Zadig 的"粗暴"方案）。
6. **启动宣判，不复活**（M3.5，对照 Zadig InitQueue）：进程重启后遗留的 running 档案宣判 cancelled（revoker 语义=system）、泄漏 Job 清掉，用户重跑即可——adopt 半跑任务树是不确定性的脏活。pause 例外：它是无需看护人的稳态，跨重启存活（比 Zadig 更 durable，有意为之）。
7. **构建 job 的真身是 freestyle + 步骤列表**（2026-11-03 核对实证，见 §7 新增行）：`zadig-build` 在 `initJobCtl` 的 switch 里没有专门 case，落 default = FreestyleJobCtl。步骤（git → shell 编译 → docker_build → …）在 **Pod 内**由 jobexecutor 顺序执行（失败且非 OnFailure 则跳过剩余步骤），步骤状态经 ConfigMap（名字=K8sJobName）回传、aslan 每秒轮询。"分节点分界面"的数据源就是 workflow_task 里 stages→jobs→steps 三级状态树，前端无特殊机制。我们做步骤化时可用 bash 驱动脚本替代 jobexecutor 二进制（ConfigMap 放 steps JSON，逐条执行回写）。
8. **日志流=状态信道**（核心 CI/CD 落地，2026-11-04）：引擎把 steps 编译成 POSIX sh 驱动脚本（每步函数化 mp_step_N + mp_run 包装 + banner `===MP=== STEP-START/END <name> passed|failed exit=N`），Pod stdout 就是步骤状态信道——与 Zadig jobexecutor+ConfigMap 信息流等价但路径更短（零新依赖零 RBAC）。引擎每 2s 刮 Pod 日志**幂等重放** banner 维护三级树；终态 SaveInfo 全量收割→切分→收口（AfterRun 门禁）→再 Clean 删 Job（**顺序铁律 Run→SaveInfo→Clean，先收割再销毁**）。护栏三件套见 §9.10/§9.11。

## 7. Zadig 参考文件地图（写对应模块前先读）

| 主题 | 模板仓库内路径 |
|---|---|
| 蓝图模型 | `pkg/microservice/aslan/core/common/repository/models/workflow_v4.go` |
| 运行实例模型 | `.../models/wokflow_task_v4.go`（文件名拼写错误是上游的） |
| **Stage 引擎（灵魂）** | `pkg/microservice/aslan/core/common/service/workflowcontroller/stage.go` |
| Run 主循环 + ack | 同目录 `workflow.go` |
| Job 执行/清理 | 同目录 `jobcontroller/job.go`（蓝绿豁免 565 行） |
| Step 层接口（aslan 侧 PreRun/AfterRun） | 同目录 `jobcontroller/stepcontroller/step.go` |
| Pod 内步骤执行器（jobexecutor） | `pkg/microservice/jobexecutor/core/service/{job.go, step.go}`（顺序跑 steps，输出收集进 ConfigMap/termination message） |
| K8s Job 生成 | 同目录 `jobcontroller/kubernetes.go`（docker.sock 挂载 832 行、DOCKER_HOST 注入 745 行） |
| counter 发号器 | `.../models/counter.go`（22 行） |
| ack 写模式 | `.../mongodb/workflow_task_v4.go:318` |
| 状态常量 | `pkg/microservice/aslan/config/consts.go:106-125` |
| 变量渲染 | `.../workflow/controller/workflow.go`（ToJobTasks） |
| 保留策略 | `.../system/service/capacity.go` |
| helm chart 形态 | 独立 chart 仓库 `koderover-chart/zadig`（tags.* + connections.* 参数化，主仓库只出 docker/*.Dockerfile） |

## 8. 当前部署与验证态势（快照，2026-11-06 深夜）

- 集群 release：helm `mini-pipeline` rev26，引擎 **0.7.0**（结构化重构：engine 四层切面+ops.go+依赖注入+导出面收口，PR #39–#43）+ runner **0.1.1** + **web 0.1.2**（React 前端+nginx Pod，白底主题，`tags.web=true`）；mongo 8.0 / registry:2 / registry-ui 同 ns 共存
- 引擎入口 `https://pipeline.47.113.177.198.nip.io`：ingress 单条规则指 **前端 Pod**，nginx 反代 `/api`、`/healthz` 到引擎 service（统一入口）；健康门 `curl -k …/healthz` 不变；引擎自身无前端路由（API-only）
- **单引擎纪律**：集群引擎自带派发器，本机 `go run` 引擎与它共用 mongo 时会抢同一条队列（§9.20）——本机引擎只作调试，一切验收打集群 ingress（§2 验收资产）
- 改动后的标准循环 = build-push（引擎）/ build-web.sh（前端）→ helm upgrade（--set 显式带全）→ 四门 + smoke 对集群跑（命令在 §5）；bootstrap 自举图纸可整链发布引擎自身（tag=bootstrap）
- 前端迭代循环：`cd frontend && npm run dev`（vite proxy 指集群 API）→ `npm run build` → `deploy/build-web.sh <tag>` → helm upgrade --set web.image… 或改 values 默认 tag

## 9. 案例存档 / 已知坑

1. **【已解决】worker-2 跨节点 Pod 网络不通**（2026-11-01，教科书级案例）：
   - **真凶在 worker-1**：keepalived 的 HaVip `172.29.237.252` 挂在 w1 eth0 上（EIP 47.113.177.198 绑此 VIP 做 ingress 高可用，w1=MASTER/w2=BACKUP）。k3s flannel 启动时扫 eth0 选中这个 VIP 作 vxlan 源地址 → VPC 反欺骗校验丢弃一切源=VIP 的包（VIP 只许入不许主动发出）→ 所有涉及 w1 的跨节点 vxlan 全灭。master↔w2 一直是健康的。
   - **解法（改 keepalived，不动 flannel）**：VIP 从 eth0 挪到 `dummy0` 接口——`virtual_ipaddress { 172.29.237.252/32 dev dummy0 }`，VRRP 报文仍走 eth0；dummy0 由 keepalived 的 systemd drop-in（`/etc/systemd/system/keepalived.service.d/dummy0.conf`，ExecStartPre 建 dummy0）持久化。eth0 只剩主 IP → flannel 自然选对。两台 worker 都改了（保证 failover 后行为一致）。ingress 经 EIP→HaVip→dummy0 验证 200 无恙。
   - **教训**：flannel 的 public-ip 注解是它的输出不是输入（手工改了会被重启覆写，实验两次证实）；host-gw 后端在阿里云 VPC 是死路（VPC 网络层按 ENI 注册表丢裸 Pod 源地址包，加安全组放行+关 SourceDestCheck 都救不了，实测）；wireguard-native 也不必了（dummy0 方案最干净，保持默认 vxlan）。
   - **运维注意**：`kubectl get nodes` 若再见 w1 的 `flannel.alpha.coreos.com/public-ip` 变成 .252 → 说明 keepalived 的 VIP 跑回 eth0 了（比如有人还原了配置）。
2. **keepalived 底稿未进仓**：`/etc/keepalived/keepalived.conf` 注释里写"仓库底稿:system/keepalived/"，但 k3s-gitops 仓库（github.com/Edward-258/k3s-gitops，私有）里没有该目录——节点层配置是单点，dummy0 改动目前只活在那两台机器上。建议补进 gitops 仓库。
3. Windows PowerShell 5.1 四坑已入档：中文路径的 .ps1 必须无中文或参数传入（编码）；curl JSON 必须 `-d @file`（吞引号）；远程 bash 里的 `sed '$d'` 要写成 `\$d`；**SSH 远程命令内层引号必被剥——传配置文件/脚本一律 base64**（`echo <b64> | base64 -d > file` 或 `| bash`，本项目已踩三次）。WSL 侧再添两坑（2026-11-03）：ssh 会话**偶发僵死**（scp 已落地但远端命令链卡在起跑线，节点上无任何进程，新开 ssh 一切正常）——杀掉重跑即好，构建脚本卡住先 `ps aux | grep ssh` 排查；想顺手清僵尸时 `pkill -f 'sshd.*notty'` 会把**自己这条**无终端会话一起杀掉（自杀），别用。
4. helm v4：upgrade 默认复用上次 --set 值（v3 行为相反），覆盖必须显式。
5. 删 K8s Job 默认保留子 Pod，必须 `PropagationPolicy: Background`（engine/run.go 的 deleteOpts() 已封装）。
6. `flannel-backend` 是 k3s **server 专属**参数，agent 配了会起不来（flag not defined）；worker 的 backend 由 master 下发。
7. PowerShell 5.1 再添两坑（2026-11-01，都在 M2 期间踩的）：**手拼 JSON 字符串必数错括号**——用 hashtable + `ConvertTo-Json` 构造（见 test-m2.ps1 的 Job/Ensure-Workflow 函数）；**`Set-Content -Encoding ASCII` 会把文件里的中文全毁成 `?`**（批量替换 values.yaml 时差点全毁，`git diff` 出现 32 行异常变更才发现）——改文件走编辑工具，批量文本操作后必 `git diff` 自检。**手拼 JSON 坑 2026-11-04 在 bash heredoc 复发**：test-cicd.sh 手写 body 尾部多一个 `]`，此前仓内版从没真正跑过（一直跑的是 python 生成的 /tmp 版）才发现；现 ensure_wf 已加 `jq -e .name` 预检，手数错误当场炸亮而不是静默错断言。复杂 body 一律 python json.dumps。
8. **helm→argocd 资源移交**（2026-11-02，registry:2 实操）：直接让 ArgoCD SSA 接管 helm 部署的资源会打架（`managed-by: Helm` 标签的字段所有权冲突）。正确三步：① 活体 PVC 先打 `helm.sh/resource-policy: keep` 注解——否则 helm upgrade 摘除时连数据卷一起删（默认回收策略下 PV 也没了，镜像全灭）；② `helm upgrade --set tags.<x>=false` 摘除资源；③ 新清单**逐字段抄活体**（storageClassName 等默认值都要抄）先 `kubectl apply` 铺上再推 git，ArgoCD 认领零漂移（出现即 Synced/Healthy），停机窗口秒级。附带发现：ArgoCD 对新 app 的发现依赖 root app 刷新（默认 ~3min 轮询），等不及可 `kubectl patch application root -n argocd -p '{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"normal"}}}' --type merge` 手动触发（UI 的 Refresh 按钮同款）。
9. **ACR 个人版推送三坑**（2026-11-03，引擎镜像迁 ACR 实录）：① 新版个人版是"独立实例"形态，专属域名带 `crpi-` 前缀（本实例 `crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com`）；老共享域名 `registry.cn-heyuan.aliyuncs.com` 对它一律 **Forbidden Host**（端点活着但不是它的家，别被 401 挑战骗了）。② ACR 拒收 BuildKit 默认生成的 provenance/attestation 附加清单，报 `unknown manifest class for application/vnd.oci.empty.v1+json`；`docker build --provenance=false` 即解，层已推上去不受影响（重推全命中 Layer already exists）。③ 认证是 Bearer token 流：`curl -u` 直怼 `/v2/` 永远 401（不代表凭证错，我为此冤枉猜过三种用户名格式）；正经测法 = 先去 `dockerauth-cn-heyuan.aliyuncs.com/auth?service=…&scope=…` 换 token，且返回字段叫 `access_token` 不是 `token`（jq 取 .token 会拿到 null → 报 Forbidden Host 的迷惑性错误）。仓库无需预建，push 自动创建。aliyun CLI 的 `cr` 元数据 API（老 ROA 接口）本版本调不通，registry HTTP API 完全够用。④ **helm chart 存不进免费版**（2026-11-03 实测）：`helm push` OCI 形式报 `unknown manifest class for application/vnd.cncf.helm.config.v1+json`——manifest 白名单只认镜像类，chart 托管是 EE 能力。chart 真相源留在 git 仓库即可，镜像走 ACR / chart 走 git 本就是标准分工。
10. **containerd 双 copier 乱序写日志**（2026-11-04 初诊"stderr 迟到"、当晚幕1 收官时推翻重诊）：containerd shim 用两条独立 copier 各自读容器 stdout/stderr 管道并**追加**写同一日志文件——**写入顺序不等于写出时间**。短命容器（几十毫秒）整个生命周期里 copier 可能直到进程退出才被调度，两路谁先落盘全看调度脸色：实测 `echo >&2` 的行落在了时间上更早的 STEP-START banner **前面**。初诊以为是"迟到 1~2 秒"并加了 SaveInfo 稳定探测（600ms×N 连续等长才收，`engine/jobctl.go`，作为尾部兜底保留）——方向错了：stderr 不在窗口内晚到，而是**排到了 banner 前面**，按 banner 切分（sliceStepLogs）整段掉黑洞；当初的"稳定探测修好了"实为插队恰好没发生的巧合性通过。**根治 = driver 首行 `exec 2>&1` 单流化**（`engine/driver.go` prelude）：shell 层把 stderr 并进 stdout，单管道单 copier = 文件顺序严格等于写出顺序，切分从此确定性成立。回归测试 `driver_roundtrip_test.go` 钉死单流不变量（分离捕获断言 stderr 缓冲必须为空）。教训：混流日志里做按行切协议，先问流是怎么被合的——两路并发追加 = 顺序无保证。
11. **driver banner 协议的两道护栏**（2026-11-04，两个真 bug 均实测复现后修复）：① **黏连 banner**——步骤输出可能没有尾换行（printf 无 \n 的 junit XML），echo 出的 banner 会黏在输出尾部不在行首、解析器看瞎 → 双修：say() 用 `printf '\n===MP=== %s\n'` 前置换行 + 解析器 `strings.Index` 行中匹配；② **裸 exit 漏 banner**——步骤体直接 `exit N` 掀翻 shell，mp_run 的 END banner 永远打不出 → EXIT trap + MP_CUR 当前步骤指针补发末班 banner（正常路径先清 MP_CUR 再 exit，trap 哑火不双打）。回归测试在 `engine/logs_test.go`（黏连）与 `driver_roundtrip_test.go`（裸 exit）。
12. **验收脚本查化石档案的假红灯**（2026-11-04，三连红的真凶）：每个 workflow 的 task counter **各自独立递增**（`workflow_task:<name>`），test-cicd.sh 附加断言曾拿幕4 cicd-git 的 task_id 去查 cicd-fail 的日志——两 counter 早已分叉（11 vs 4），读到的是旧引擎时代的任务 4 档案，新引擎修什么它都恒红。修法：幕3 先存 `FTID=$(run_wf cicd-fail)` 自己的号。教训：跨 workflow 借 id = 读化石；断言红三连且"修了没变化"时，先怀疑被测对象是不是你以为的那个。
13. **本机 Docker Desktop 拉不动 docker.io**（2026-11-04）：WSL 侧 docker pull 报 `auth.docker.io/token ... EOF`——TLS 握手即被掐（SNI 阻断），非瞬时抖动。解法（不动 Docker Desktop 全局配置）：经 mirror 拉基镜像再 retag，`docker pull docker.m.daocloud.io/library/golang:1.26-alpine && docker tag ... golang:1.26-alpine`，构建走本地缓存不再碰 docker.io；worker 节点的 daemon.json 自带 mirror 列表不受影响。
14. **K8s Job 名必须含 workflow 名**（2026-11-05，T1.6 队列活体验收拆的 M1 老雷）：旧规则 `mp-<taskID>-<stage>-<job>` 不含 wf 名——两个不同 workflow 的同号任务 + 同名 stage/job = **同一个 K8s Job 名**：幂等 Delete 删掉别人家的 Job、Clean 掀掉别人正在轮询的执行物（实测 t16capA/B 同 task_id=1 同 s1/j 互踩，B 报 `poll k8s job: not found`）。单 wf 时代永远撞不上，队列让跨 wf 并发成为常态后必炸。修法：`mp-<wf>-<taskID>-<stage>-<job>`（wf 名唯一，sanitize+截断 63）+ 回归测试 `TestJobName`。教训：资源命名的唯一性边界要按"并发单元"划，不按"引入顺序"划。
15. **jq 查含点字面键要用 `["a.b"]`**（2026-11-05，test-mvp1.sh 两连红真凶）：GlobalContext 收割键是 `<job>.<output>` 字面含点（如 `build.MSG`），`jq '.global_context.build.MSG'` 把它当两层路径 → null。正确写法 `.global_context["build.MSG"]`。引擎数据没问题（下游渲染已证），断言先红时先怀疑查询语法。
16. **节点的 docker login 凭证不进 Pod**（2026-11-05，bootstrap 二跑活体抓的）：ssh 到 worker `docker login` 存的是宿主 `/root/.docker/config.json`；Job Pod 里的 docker CLI 读的是**容器内** `/root/.docker`——宿主凭证不传播。M5 时代没暴露因为推的是 `<nodeIP>:30500` 免认证 registry，bootstrap 推 ACR 才炸（`insufficient_scope`）。修法：`buildJob` 在 `jobWantsDockerSock` 同门挂 `/root/.docker` hostPath（DirectoryOrCreate + 只读）——fat 教义内无新权限面（有 docker.sock 已 ≈ 节点 root）。前置语义不变：要 push 的节点先 login。
17. **busybox wget `--post-file` 截断 POST body**（2026-11-05，mvp4 对自举引擎活体抓的，T4.3 workaround 翻车）：busybox wget 在引擎响应到达时**提前断开**，body 只送出任意前缀——实测 3B→存 0 / 180B→存 3 / 10KB→存 41 字节，引擎 `io.Copy` 如实存残包还回 200（**静默腐败**，比 404 毒；pack 步骤日志里的 `{"stored":...}` 完全看不出问题）。根因：wget -O- 把"收到响应"当请求完成，而引擎（正确的）边读边落 GridFS、读完才响应——时序竞速谁先到头。修法三件套：runner 镜像 0.1.1 装 curl；`jobNeedsRunner` 加 StepArchive（带 archive 的 job 落 runner）；ArchiveCtl 生成 `curl -s -f --data-binary`（-f 让上传失败翻红）。教训：给脚本生成器选 HTTP 工具时，"谁先关连接"的语义比"能不能发 POST"重要。
18. **T1.9 兜底 10 分钟墙钟会杀冷启动重活**（2026-11-05，bootstrap 首跑活体抓的）：per-job 默认 `jobTimeout = 10min`（run.go:21）对轻 job 是护栏，对"冷缓存 go test 6min + docker build 3min"的自举 job 是处决刀——pod 好好的，引擎 10min 整点删 Job。修法：图纸显式 `timeout_minutes: 30`（#26）。教训：兜底默认值的适用边界要写进图纸模板，重 CI job 一律显式配超时。
19. **前端 JS 首次真浏览器执行即死——模板反引号配对崩坏**（2026-11-05 晚，用户浏览器首访抓的）：T4.2 的 index.html 在 openTask 的 tree 模板收尾处多了 `}` + 多余反引号（`` </li>`).join("")}`; ``），整段 `<script>` 语法错 → **零个 handler 挂上**，症状 = 页面内容空 + 五页签全点不动，而 test-mvp4 的 grep 门全绿——验收只看字符串、从不执行 JS，playwright 走查又因 Docker 端口发布坏（2026-11-05 确认：**新容器 -p 照样不发布，Docker Desktop 端口转发机制整体失效**，与 chrome 容器无关）从未跑成。修法三件套：① 反引号收尾只留一个（`</li>`).join("");`）+ `ts()` 兼容 RFC3339/unix 双形态时间戳；② test-mvp4 幕2 加 **node --check 语法门**（打 ingress 实吐字节）；③ `deploy/web/smoke.js` 入库——jsdom 真执行 + 真 fetch 五页签冒烟（9 断言，不需要真浏览器）。定位手法：node --check 逐块二分 + 手写模板状态机扫描器逐行打印栈（比肉眼数反引号可靠）。教训：**"HTML 里字符串在位" ≠ "脚本活着"，任何 JS 交付必须过一次真执行**；环境工具坏掉时降级路径（jsdom）比死磕环境修复（Docker Desktop 端口转发）更快。
20. **双引擎抢同一个队列**（2026-11-06 晚，mvp1 本机活体三连怪红真凶）：集群引擎升到 0.6.x 后自带派发器，而本机验收姿势（§0.4：本机引擎 + port-forward mongo）与集群引擎连的是**同一个 mongo**——两台引擎的 dispatchOnce 在同一条队列上赛跑：本机脚本排的队被集群引擎抢走跑（task 永远"不来"）、RunningStats 读到对方刚 CAS 完的状态让断言时序全乱。0.4.6 时代集群引擎无派发器所以从没暴露。修法二选一：① 验证收敛到单一引擎——四门全部打集群 ingress（`MP_BASE=https://pipeline.47.113.177.198.nip.io`，跑的就是刚 upgrade 的镜像）；② 临时 `kubectl scale deploy/mini-pipeline --replicas=0` 再本机跑。另附：本机引擎 POST→立刻 GET 的 "queued" 断言天然与 1s 派发拍竞速，快机器上必输——test-mvp1 已放宽为 queued|running（"run 即返回不阻塞"语义不变，同 wf 串行由后续断言承担）。
21. **nginx proxy_pass 写死主机名 = 启动期静态解析，解析不到直接 emerg 拒启**（2026-11-06 深夜，web 镜像 0.1.0 本地 docker run 抓的）：`proxy_pass http://mini-pipeline.pipeline.svc.cluster.local:8080` 直写主机名，nginx 启动配置校验阶段就做 DNS 解析——本地 docker run 无 K8s DNS、或集群里引擎 service 晚创建（web Pod 先起），都 `[emerg] host not found in upstream` 拒启（本地实测容器秒退）。修法三件套（web 镜像 0.1.1）：① `set $engine_upstream ${MP_ENGINE_UPSTREAM}; proxy_pass http://$engine_upstream;`——变量形式 = 运行时解析，解析不到只 502 进程照常活；② resolver 从容器自己的 resolv.conf 注入（`/docker-entrypoint.d/99-resolver.sh`，nginx 官方镜像钩子序；kube-dns / docker embedded dns 通吃）；③ envsubst 注意：nginx 官方 20-envsubst 只替换容器已定义 env，`$engine_upstream` 这种 nginx 变量不会被误展开（模板里放心混写）。教训：反代配置里"名字何时被解析"和"名字指向什么"同等重要——静态解析把启动顺序耦合进了配置。
22. **WSL 无 sudo 时的浏览器走查降级路线 + npm 断网残局**（2026-11-06 深夜，PR-F 验收实录）：① 本机 `~/.cache/ms-playwright/chromium-1243` 二进制起不来——`libnspr4.so` 等系统库缺失，`sudo apt-get` 要密码（用户在睡），playwright 报的"Target closed"其实是加载库失败；Windows Chrome CDP 也不通（`--remote-debugging-port` 绑 Windows 回环，WSL2 NAT 够不着）。② 降级正解 = **jsdom 真执行 React 产物**（`frontend/e2e/smoke.mjs`，5 断言全绿）：vite 单 chunk 产物是自执行代码（无 import/export 语句）→ `new Function(bundle)()` eval 即真跑；三个前置坑全踩过才通——**node fetch 严格校验自签 TLS**（`NODE_TLS_REJECT_UNAUTHORIZED=0`，curl -k 同款语义）；**bundle 裸引用 HTMLElement/MutationObserver 等全局类**（一次性 `Object.getOwnPropertyNames(window)` 桥接到 globalThis，testing-library 手法；逐个 stub 是打地鼠：ShadowRoot→HTMLElement→…）；**自写 waitFor 返回 `true` 而非 pred 的值**（元素断言拿到布尔，`taskMenu.click is not a function` 的真凶）。③ npm 侧：npmmirror CDN 大量 ECONNRESET 时 `--maxsockets=3 --fetch-retries=5` 重装可通，但失败 install 会留下**空壳 @types 目录**——目录在而文件空，tsc 把它当隐式类型入口报 TS2688 "Cannot find type definition"；`rm -rf node_modules/@types/babel__*` 后重装（npm 不会自愈空壳）。教训：环境工具不可用时，"产物的真执行"往往还有第二条路——前提是产物形态允许（自执行 bundle + jsdom）；降级方案要沉淀成脚本进库，别做成一次性手工操作。
