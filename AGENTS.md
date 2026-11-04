# AGENTS.md — mini-pipeline 项目上下文（给下一个会话的你）

> **工作台（2026-11-02 起迁移）**：WSL2 Ubuntu-24.04，本仓库位于 `/home/edward/projects/mini-pipeline`（git 历史完整迁入，`core.autocrlf=input`）。工具链：go 1.26.4（/usr/local/go，GOPROXY=goproxy.cn）、helm v4.0.0、kubectl（context `k3s-heyuan` 已配通）、jq、gcc、git（身份 Edward-258）、**Docker Desktop 29.x（2026-11-04 起，本机直构直推镜像）**。路径映射：Zadig 模板 → `/mnt/e/code pipeline学习模板(基于zadig项目)/zadig`；SSH key → `~/.ssh/heyuan81.pem`（/mnt/e 原件 drvfs 权限 ssh 不认，已拷贝 + chmod 600）；Windows 临时目录 → `/mnt/c/Temp/opencode`。`deploy/build-push.sh`/`build-runner.sh` 是本机构建脚本（docker build + push ACR，不再 ssh worker）。Windows 旧副本 `E:\code pipeline的小范围复现` 仅作存档，勿双向编辑。

> **远端（2026-11-03 关联）**：`origin` = https://github.com/Edward-258/code-pipeline.git（**私有**，首推 master；仓库含明文密码，永不可转公开）。WSL 无 GitHub 凭证，推送走 Windows GCM 桥——credential.helper 已写进本仓库 local config（指向 `/mnt/c/Program Files/Git/mingw64/bin/git-credential-manager.exe`），裸 `git push` 即可，弹窗点一下就行。

> 任何 AI 会话开始于此目录时，先读完本文件再动手。内容按"必读 → 速查 → 参考"排序。

## 0. 铁律

1. **Zadig 模板仓库绝对只读**：`E:\code pipeline学习模板(基于zadig项目)\zadig` 只能读代码做对照，改一个字节都不行。本项目（`E:\code pipeline的小范围复现`）是独立的复现项目。
2. 本项目是**学习性质的微型复现**，不是产品：砍掉了 RBAC/审批/IM/多集群，代码宁可直接不要过度设计。
3. 集群是临时的学习环境（master 节点 2026-11-16 到期），别在上面跑任何重要东西。
4. **三节点 /etc/rancher/k3s/registries.yaml 是手工合并出来的**（原有 docker.io/quay.io 加速 + 我们加的 localhost:5000 mirror），改它必须先 cat 看现状再合并，改完要滚动重启 k3s（master=`k3s`，worker=`k3s-agent`）。

## 1. 项目定位

以 Zadig（本地模板，HEAD `a1fe8122e`，v5.0.0-dev 线）为蓝本的微型发布工作流引擎：

- Go + gin + mongo-driver(v1) + client-go，单体服务，module 名 `pipeline`
- MongoDB 三集合：`workflow`（图纸）/ `workflow_task`（施工档案）/ `counter`（发号器）
- Job = K8s Job：主控只创建/轮询/清理，不执行脚本
- **引擎本体也跑在 k3s 里**（helm chart 部署，对齐 Zadig 的发行形态），API 经 ingress-nginx 暴露

**三条灵魂（及格线，缺一不算复现）**：
1. 图纸/实例分离，task 内嵌当时的完整定义快照
2. Stage 状态机 + **durable 暂停**（暂停=落库+goroutine 退出，进程重启可恢复）
3. Job = K8s Job

## 2. 当前进度（截至 2026-11-04）

**M0 + M1 + M2 + M3 + M3.5 + 核心 CI/CD + M5 密封构建已完成**。M1 = run 端点 → counter 发号 → 快照落库 → K8s Job 创建/轮询/清理 → ack 全量写；引擎已镜像化部署进 k3s，经 ingress 对外。M2 = runStages 状态机（stage 内并行/串行 + 最差者胜聚合 + 失败刹车）。M3 = durable 暂停（闸门落库+goroutine 退出 → kill 引擎 Pod 档案仍在 → release 续跑）。M3.5 = 启动宣判（running 孤儿档案 cancelled + 泄漏 Job 清理；pause 稳态不碰——分叉自 Zadig，它连 waiting-approve 一起取消）。核心 CI/CD = 步骤化三级树 + bash driver + junit 门禁 + 分步日志（2026-11-04）。**M5 密封构建闭环收官（2026-11-04 晚，镜像 0.4.6，33 断言全绿）**：docker_build → registry:2 → 临时 Job 拉起产物全链路打通；收官路上修了三个真 bug（$MP_NODE_REGISTRY 单引号不展开 / containerd 双 copier 乱序写日志 / 验收脚本查错档案的化石红灯），详见 §9.10 修订与 §9.12。基础设施三连升：registry:2 移交 ArgoCD（#11）+ 引擎镜像走 ACR（#12）+ runner 镜像走 ACR（#12 扩展，2026-11-04）。

里程碑总览（详细验收标准见桌面 `mini-zadig-计划.md`）：

- [x] M0 地基
- [x] M1 第一口气：run → K8s Job → 轮询 → ack → 清理（本地与集群双验收过）
- [x] **M2 状态机**（2026-11-01 完成，镜像 0.2.0）：runStages（stage 内 Parallel 并行/串行）+ WorstOf 最差者胜聚合 + 失败刹车（串行=剩余 job 置 skipped；并行=全员跑完再聚合；后续 stage 全 skipped）。验收 22 断言全过（`C:\Temp\opencode\test-m2.ps1`，走 ingress）
- [x] **M3 灵魂时刻**（2026-11-03 完成，镜像 0.3.0）：runStages 闸门（`stageGated`，对照 Zadig waitForManualExec）→ pause 落库 + goroutine 直接 return；release 端点 `POST /api/tasks/:name/:id/stages/:stage/release`（releaseMu 防双击；只翻 `Executed` 位，pause→running 翻转单点在 engine.Run）；Run() 的 StartTime 首次标记（恢复不覆盖）。验收 11 断言全过（`deploy/test-m3.sh`，含 kill 引擎 Pod 后档案仍 pause 的 durable 证明 + 双 release 409）
- [x] **M3.5 启动宣判**（2026-11-03 完成，镜像 0.3.1）：`engine.Recover()`（main.go 启动时跑，在 gin 起来之前）扫 created/running 孤儿档案 → `declareDead`（running→cancelled、created→skipped、终态不改写）→ 清泄漏 K8s Job。对照 Zadig queue.go:110 InitQueue "取消所有 running tasks"（revoker=system）；**有意分叉：pause 不进扫描集**（我们是无需看护人的稳态，Zadig 连 waiting-approve 一起杀）。不变量：running 要么有活着的看护人，要么是终态。验收 13 断言全过（`deploy/test-m3.5.sh`，running 死宣判+Job 清理 / pause 死存活+release 照常，两剧本对照）
- [x] **核心 CI/CD**（2026-11-04 完成，镜像 0.4.4，runner `pipeline-runner:0.1.0` 进 ACR）：spec.steps 三级树（stage→job→step，StepTask 自带 status/起止/Log/TestResult——分节点分界面的数据源）+ StepCtl/JobCtl 注册表（对照 Zadig 17 StepCtl/50 JobCtl 的 switch 注册）+ 引擎代码生成 POSIX sh 驱动脚本（banner 协议 `===MP=== STEP-START/END`，日志流=状态信道，零新依赖零 RBAC）+ junit 通过率门禁（AfterRun 引擎侧解析，默认 100% 翻红）+ 分步日志端点 `GET /api/tasks/:wf/:id/log?stage=&job=`。步骤首发四种：shell/git/docker_build/junit_report；旧图纸 Scripts 翻译成隐式 step "main"（EffectiveSteps 单源）。验收 33 断言全过（`deploy/test-cicd.sh`：幕0 兼容/幕1 密封构建闭环/幕2 门禁红绿/幕3 失败路径/幕4 git+ACR 拉取链路+附加分步日志；MP_ACT1=1 已默认可跑）
- [ ] M4 变量传递（global_context，JSON→字符串替换→反序列化）+ 单 job 重试
- [x] **M5 真构建闭环**（2026-11-04 晚收官，镜像 0.4.6）：密封构建三步全绿——shell 造 Dockerfile → docker_build 遥控节点 daemon 构建并推 `<nodeIP>:30500`（两台 worker daemon.json insecure-registries 已放行，补丁脚本 `deploy/fix-daemon-insecure.sh` 已实锤生效）→ 临时 Job 经 `localhost:5000` mirror 拉起产物运行。收官路上三修：① docker_build 生成的 `-t '$MP_NODE_REGISTRY/...'` 单引号禁展开（exit=125）→ 前缀段改双引号引号外拼接；② containerd 双 copier 乱序写日志（§9.10 修订）→ driver 首行 `exec 2>&1` 单流化；③ 验收脚本拿 cicd-git 的 task_id 查 cicd-fail 档案读到了化石（§9.12）
- [ ] M6 半吊子前端（单 HTML + fetch 轮询，不上框架；三级树+TestResult+分步日志数据源已就绪）

已有代码结构（2026-11-04 起业务包收进 `pkg/`，模块名 `pipeline` 不变，import 路径带 `pkg/` 前缀）：`pkg/config/` `pkg/models/` `pkg/repo/` `pkg/api/`(CRUD+run/task+分步日志) `pkg/engine/`(k8s.go 双模式 client / job.go Job 构建 / run.go 执行器 / driver.go 脚本生成 / stepctl.go+jobctl.go 注册表 / logs.go banner 解析) `main.go`(根，仅装配) `docker/`(引擎+pipeline-runner) `deploy/chart/mini-pipeline/`(helm chart) `deploy/build-push.sh`(引擎镜像) `deploy/build-runner.sh`(runner 镜像) `deploy/test-cicd.sh`(验收)。

## 3. 环境速查（全部实测可用）

| 项 | 值 |
|---|---|
| k3s master | 8.155.137.224（内网 172.29.237.251，control-plane+etcd，**2026-11-16 到期**） |
| k3s worker-1 | 8.155.128.91（内网 172.29.237.250） |
| k3s worker-2 | 8.155.128.81（内网 172.29.237.249）✅ 网络已修复（2026-11-01，见 §9 案例存档） |
| kubectl context | `k3s-heyuan` |
| MongoDB | ns `pipeline`，**ClusterIP**（chart 内置），本机调试走 `kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017` |
| registry:2 | **归 ArgoCD 管**（k3s-gitops `registry` app，2026-11-02 移交）。NodePort 30500 仅内网（节点 push / kubelet mirror 链路不变）；对外 `http://registry.47.113.177.198.nip.io`（ingress 反代；Docker Desktop 直连需配 insecure-registries）；前端 `http://registry-ui.47.113.177.198.nip.io`（joxit UI，同源代理 /v2/，可浏览/删镜像）。角色已定：引擎/runner 镜像走 ACR，**registry:2 = docker_build 步骤的产物仓库**（M5 收官验证：push `<nodeIP>:30500` + kubelet `localhost:5000` mirror 拉起产物闭环） |
| **ACR 个人版** | 引擎 + runner 镜像分发仓库：独立实例 `crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com`（公网推送）+ `-vpc` 后缀变体（集群 VPC 拉取，免公网流量）。命名空间 `codepipeline-learning` 下两仓：`mini-pipeline`（引擎）、`pipeline-runner`（步骤执行器，chart values `runner.image`）。用户名 `nick9188231182`，密码在 chart values `acr.password`。build 必须 `--provenance=false`（详见 §9.9）；老共享域名 `registry.cn-heyuan.aliyuncs.com` 不属于本实例（Forbidden Host），别用 |
| 引擎入口 | `https://pipeline.47.113.177.198.nip.io`（复用集群现成 nip.io+ingress-nginx 链路，SG 80/443 已放行） |
| 安全组 | `sg-f8z8wcoum4myexrjfr40`；**30017 已收掉**，现有 80/443/6443/22/3389 + 同组内网互通 |
| 集群存量 | argocd（ns argocd）、ingress-nginx（DaemonSet hostNetwork 双 worker）——都不是我们的，别动 |
| SSH | `ssh -i ~/.ssh/heyuan81.pem root@<节点IP>`，三台全通（集群操作直接 ssh，pem 是 /mnt/e 原件的拷贝） |
| aliyun CLI | 已配 AK/SK（profile default，查询时显式 `--RegionId cn-heyuan`）。仅云 API 查询用（如安全组），够不到集群本身 |

## 4. 已定稿的架构决断（不要再讨论）

1. **引擎跑在集群内**（2026-11-01 拍板，推翻更早的"本机跑"方案）：InClusterConfig + SA `pipeline-engine`（Role 仅 jobs/pods 权限）；代码保留 kubeconfig fallback 供本机调试（`K8S_CONTEXT=k3s-heyuan`）。
2. **部署形态对齐 Zadig = helm chart**：`deploy/chart/mini-pipeline`，`tags.{engine,mongo,registry}` 组件开关 + `connections.mongo.uri` 参数化（对照 Zadig chart 的 tags.mongodb / connections 模式）。⚠️ Helm v4 升级默认**复用**上次 --set 值，覆盖要显式 `--set`。
3. **registry:2 提前建**（原 M5 组件）：镜像名统一 `localhost:5000/mini-pipeline:<tag>`，push 侧节点 docker 推 `127.0.0.1:30500`，pull 侧三节点 registries.yaml 把 `localhost:5000` mirror 到三个节点内网 IP:30500（不能只配 127.0.0.1——worker-2 回环 NodePort 不通，见 §9）。registry 存储会剥离 host 前缀，两边镜像名天然一致。（2026-11-02 起资源归 ArgoCD 管，见决断 #11；两条链路本身不变）
4. **镜像在本机构建直推 ACR**（2026-11-04 起本机 Docker Desktop 可用后改版）：`deploy/build-push.sh <tag>` = 本机 `docker build`（多阶段 golang:1.26-alpine，GOPROXY=goproxy.cn，.dockerignore 瘦上下文）→ push ACR 公网端点（`--provenance=false` 硬要求，见 §9.9）。历史：2026-11-03~04 曾走"scp worker-1 远程构建"链路（本机无 docker），ssh 僵尸会话坑频发，本机 engine 就位后退役；worker 上构建一去不复返。
5. **镜像构建走路线①**（M5 生效）：fat image（docker CLI）+ 挂宿主 `/var/run/docker.sock`；k3s 是 containerd，两个镜像仓库互不相认，registry:2 是必选项。
6. 状态获取用**轮询**（每 2s 查 K8s Job），不上 watch。
7. runner 不主动回调：**Pod 退出码 = job 结果**，日志用 API 拉。
8. 第一版不做队列/并发控制。
9. 挂 docker.sock 的 Pod ≈ 节点 root，仅限学习集群，fat image 永不推公网。
10. Mongo 密码明文写在 chart values 和 config/config.go——学习版接受，此仓库别推公开远端（2026-11-03 起已有**私有**远端 `origin` = github.com/Edward-258/code-pipeline，保持私有即合规；ACR 密码 `acr.password` 同档处理，只进私有处）。
11. **registry:2 归 ArgoCD 管**（2026-11-02）：资源清单进 k3s-gitops 仓库（`registry` app，app-of-apps 第 4 位，sync-wave 3），chart 的 `tags.registry` 默认 false 只留作全新环境 bootstrap。新增 ingress 反代 `registry.47.113.177.198.nip.io` 供本机直连（无认证无 TLS 公网可达，仅学习期接受）。helm→argocd 移交手法见 §9.8。
12. **引擎镜像分发走 ACR 个人版独立实例**（2026-11-03）：push = 公网端点（构建侧，节点或本地），pull = VPC 端点（`-vpc` 后缀，集群内网免公网流量）；凭证 = chart 内 imagePullSecret `acr-credential`（dockerconfigjson，节点 registries.yaml 不动、不重启 k3s，其它镜像拉取链路零影响）。**2026-11-04 扩展：runner 镜像同链路跟进**（`codepipeline-learning/pipeline-runner`，chart values `runner.image` → env `RUNNER_IMAGE` 注入引擎，升 tag 不用重编引擎）。⚠️ 引擎运行时创建的 Job Pod 必须**自带 imagePullSecrets acr-credential**（Deployment 挂的 secret 不会继承，engine/job.go buildJob 已内置）——dockerconfigjson 按 registry host 匹配，busybox 等 docker.io 公共镜像拉取不受影响。registry:2 角色已定 = docker_build 步骤的产物仓库（2026-11-04 M5 收官跑通），两条链路互不干扰。ACR 的坑见 §9.9。

## 5. 常用命令

```bash
# ===== 部署循环（改代码后的标准姿势；本机 Docker Desktop 直构直推） =====
# 1) 构建镜像进 ACR（--provenance=false 是 ACR 硬要求，脚本已内置；首次需 docker login 一次）
deploy/build-push.sh 0.2.2
# 2) 升级 release（注意 helm v4 会复用上次 --set，覆盖要显式）
helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=0.2.2 --set tags.engine=true --wait

# ===== runner 镜像（低频；runner 变更时跑，tag 须同步 chart values runner.image） =====
deploy/build-runner.sh 0.1.0

# ===== 核心 CI/CD 验收 =====
bash deploy/test-cicd.sh              # 幕1 默认跳过；MP_ACT1=1 全套 33 断言（daemon.json 已放行，密封构建可跑）

# ===== git 远端（私有 GitHub） =====
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

## 8. 下一步

> 2026-11-04 晚状态：**M5 密封构建收官**（幕1 全绿，镜像 0.4.6，33 断言全过；两个新教训入档 §9.10 修订/§9.12）。设计底稿 `~/.claude/plans/mellow-spinning-acorn.md`（含 Zadig 源码核对结论）。

未竟事项（按优先级）：

1. **自举彩蛋**：git 步骤克隆私有 code-pipeline 仓库构建引擎自身镜像（需用户提供 GitHub PAT；公开仓库克隆已验通——幕4 octocat/Hello-World）。
2. **M4 变量传递**：global_context（JSON marshal → `{{.key}}` 替换 → unmarshal，Zadig 粗暴方案）+ 单 job 重试 + outputs（StepCtl 挂点已留）。
3. **M6 半吊子前端**：单 HTML + fetch 轮询；数据源已就绪（task 树带 steps/TestResult + 分步日志端点）。

核心 CI/CD 完成细节（留档）：driver 生成 `engine/driver.go`（⚠️ POSIX sh——alpine 是 ash，无 local/[[ ]]/数组；**首行 `exec 2>&1` 单流化是切分确定性的地基**，见 §9.10）；banner 解析/切分/收口 `engine/logs.go`（applyStepBanners 幂等重放且**行中 banner 也认** / sliceStepLogs 按 START 切段，未知步骤进黑洞 / finalizeSteps：created→skipped、悬空 running→failed、AfterRun 门禁、64KB 保尾部截断）；StepCtl `engine/stepctl.go`（git clone --depth 1 + token 注入 URL、docker_build 无 registry 前缀时加 `"$MP_NODE_REGISTRY"/`——**前缀段双引号在单引号外**，否则 sh 不展开、docker 收字面量报 exit=125、junit 只认 testsuite 开标签属性跳过 testsuites 包装层防双算）；JobCtl `engine/jobctl.go`（Run 轮询循环内顺手刮日志、SaveInfo **稳定探测**连续等长兜尾部）；镜像三档 spec.image > runner > busybox；roundtrip 测试 `engine/driver_roundtrip_test.go` 真 /bin/sh 跑→**分离捕获断言单流不变量**→banner 反解→finalize 全链。

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
