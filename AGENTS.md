# AGENTS.md — mini-pipeline 项目上下文（给下一个会话的你）

> **工作台（2026-11-02 起迁移）**：WSL2 Ubuntu-24.04，本仓库位于 `/home/edward/projects/mini-pipeline`（git 历史完整迁入，`core.autocrlf=input`）。工具链：go 1.26.4（/usr/local/go，GOPROXY=goproxy.cn）、helm v4.0.0、kubectl（context `k3s-heyuan` 已配通）、jq、gcc、git（身份 Edward-258）。路径映射：Zadig 模板 → `/mnt/e/code pipeline学习模板(基于zadig项目)/zadig`；SSH key → `~/.ssh/heyuan81.pem`（/mnt/e 原件 drvfs 权限 ssh 不认，已拷贝 + chmod 600）；Windows 临时目录 → `/mnt/c/Temp/opencode`。`deploy/build-push.sh` 是 WSL 版构建脚本（tar/scp/ssh 原生三件套，源码取自本仓库；Windows 时代的 build-push.ps1 已删）。Windows 旧副本 `E:\code pipeline的小范围复现` 仅作存档，勿双向编辑。

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

## 2. 当前进度（截至 2026-11-03）

**M0 + M1 + M2 已完成**。M1 = run 端点 → counter 发号 → 快照落库 → K8s Job 创建/轮询/清理 → ack 全量写；引擎已镜像化部署进 k3s，经 ingress 对外。M2 = runStages 状态机（stage 内并行/串行 + 最差者胜聚合 + 失败刹车），细节见下方里程碑列表。两个生产级 bug 的教训已固化进代码注释：task `_id` 必须插入前定为自然键 `<wf>-<taskID>`（否则 ack 全部静默失效）；删 Job 必须显式 `PropagationPolicy: Background`（否则留孤儿 Pod）。基础设施侧 2026-11-02/03 两连升：registry:2 移交 ArgoCD + 引擎镜像分发改走 ACR 个人版（决断 #11/#12）。

里程碑总览（详细验收标准见桌面 `mini-zadig-计划.md`）：

- [x] M0 地基
- [x] M1 第一口气：run → K8s Job → 轮询 → ack → 清理（本地与集群双验收过）
- [x] **M2 状态机**（2026-11-01 完成，镜像 0.2.0）：runStages（stage 内 Parallel 并行/串行）+ WorstOf 最差者胜聚合 + 失败刹车（串行=剩余 job 置 skipped；并行=全员跑完再聚合；后续 stage 全 skipped）。验收 22 断言全过（`C:\Temp\opencode\test-m2.ps1`，走 ingress）
- [ ] M3 灵魂时刻：manual_exec 暂停（落库+退出）→ kill 进程重启验证恢复 → release 端点放行续跑
  - 集群形态下 kill 进程 = `kubectl -n pipeline delete pod -l app=mini-pipeline`，比 Ctrl+C 更真实
- [ ] M4 变量传递（global_context，JSON→字符串替换→反序列化）+ 单 job 重试
- [ ] M5 真构建：fat runner（docker CLI）+ 挂宿主 docker.sock + 真 Go 项目构建推镜像（registry 已就位，M5 直接用）
- [ ] M6 半吊子前端（单 HTML + fetch 轮询，不上框架）

已有代码结构：`config/` `models/` `repo/` `api/`(CRUD+run/task) `engine/`(k8s.go 双模式 client / job.go 构建 / run.go 执行器) `docker/Dockerfile` `deploy/chart/mini-pipeline/`(helm chart) `deploy/build-push.sh`(镜像循环)。

## 3. 环境速查（全部实测可用）

| 项 | 值 |
|---|---|
| k3s master | 8.155.137.224（内网 172.29.237.251，control-plane+etcd，**2026-11-16 到期**） |
| k3s worker-1 | 8.155.128.91（内网 172.29.237.250） |
| k3s worker-2 | 8.155.128.81（内网 172.29.237.249）✅ 网络已修复（2026-11-01，见 §9 案例存档） |
| kubectl context | `k3s-heyuan` |
| MongoDB | ns `pipeline`，**ClusterIP**（chart 内置），本机调试走 `kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017` |
| registry:2 | **归 ArgoCD 管**（k3s-gitops `registry` app，2026-11-02 移交）。NodePort 30500 仅内网（节点 push / kubelet mirror 链路不变）；对外 `http://registry.47.113.177.198.nip.io`（ingress 反代；Docker Desktop 直连需配 insecure-registries）；前端 `http://registry-ui.47.113.177.198.nip.io`（joxit UI，同源代理 /v2/，可浏览/删镜像）。**2026-11-03 起退居 M5 流水线产物专用**（引擎镜像已改走 ACR） |
| **ACR 个人版** | 引擎镜像分发仓库（2026-11-03 起）：独立实例 `crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com`（公网推送）+ `-vpc` 后缀变体（集群 VPC 拉取，免公网流量）。命名空间 `codepipeline-learning`，用户名 `nick9188231182`，密码在 chart values `acr.password`。build 必须 `--provenance=false`（详见 §9.12）；老共享域名 `registry.cn-heyuan.aliyuncs.com` 不属于本实例（Forbidden Host），别用 |
| 引擎入口 | `https://pipeline.47.113.177.198.nip.io`（复用集群现成 nip.io+ingress-nginx 链路，SG 80/443 已放行） |
| 安全组 | `sg-f8z8wcoum4myexrjfr40`；**30017 已收掉**，现有 80/443/6443/22/3389 + 同组内网互通 |
| 集群存量 | argocd（ns argocd）、ingress-nginx（DaemonSet hostNetwork 双 worker）——都不是我们的，别动 |
| SSH | `ssh -i ~/.ssh/heyuan81.pem root@<节点IP>`，三台全通（集群操作直接 ssh，pem 是 /mnt/e 原件的拷贝） |
| aliyun CLI | 已配 AK/SK（profile default，查询时显式 `--RegionId cn-heyuan`）。仅云 API 查询用（如安全组），够不到集群本身 |

## 4. 已定稿的架构决断（不要再讨论）

1. **引擎跑在集群内**（2026-11-01 拍板，推翻更早的"本机跑"方案）：InClusterConfig + SA `pipeline-engine`（Role 仅 jobs/pods 权限）；代码保留 kubeconfig fallback 供本机调试（`K8S_CONTEXT=k3s-heyuan`）。
2. **部署形态对齐 Zadig = helm chart**：`deploy/chart/mini-pipeline`，`tags.{engine,mongo,registry}` 组件开关 + `connections.mongo.uri` 参数化（对照 Zadig chart 的 tags.mongodb / connections 模式）。⚠️ Helm v4 升级默认**复用**上次 --set 值，覆盖要显式 `--set`。
3. **registry:2 提前建**（原 M5 组件）：镜像名统一 `localhost:5000/mini-pipeline:<tag>`，push 侧节点 docker 推 `127.0.0.1:30500`，pull 侧三节点 registries.yaml 把 `localhost:5000` mirror 到三个节点内网 IP:30500（不能只配 127.0.0.1——worker-2 回环 NodePort 不通，见 §9）。registry 存储会剥离 host 前缀，两边镜像名天然一致。（2026-11-02 起资源归 ArgoCD 管，见决断 #11；两条链路本身不变）
4. **镜像构建在节点上做**：`deploy/build-push.sh <tag>` 一条命令 = WSL 打包源码 → scp worker-1 → docker build（多阶段 golang:1.26-alpine，GOPROXY=goproxy.cn）→ push ACR（2026-11-03 起目标从内置 registry 改为 ACR，见 #12 和 §9.12；本机 Docker Desktop 走 SSH 隧道直推的方案实测可行但需常驻隧道，弃用）。
5. **镜像构建走路线①**（M5 生效）：fat image（docker CLI）+ 挂宿主 `/var/run/docker.sock`；k3s 是 containerd，两个镜像仓库互不相认，registry:2 是必选项。
6. 状态获取用**轮询**（每 2s 查 K8s Job），不上 watch。
7. runner 不主动回调：**Pod 退出码 = job 结果**，日志用 API 拉。
8. 第一版不做队列/并发控制。
9. 挂 docker.sock 的 Pod ≈ 节点 root，仅限学习集群，fat image 永不推公网。
10. Mongo 密码明文写在 chart values 和 config/config.go——学习版接受，此仓库别推公开远端（2026-11-03 起已有**私有**远端 `origin` = github.com/Edward-258/code-pipeline，保持私有即合规；ACR 密码 `acr.password` 同档处理，只进私有处）。
11. **registry:2 归 ArgoCD 管**（2026-11-02）：资源清单进 k3s-gitops 仓库（`registry` app，app-of-apps 第 4 位，sync-wave 3），chart 的 `tags.registry` 默认 false 只留作全新环境 bootstrap。新增 ingress 反代 `registry.47.113.177.198.nip.io` 供本机直连（无认证无 TLS 公网可达，仅学习期接受）。helm→argocd 移交手法见 §9.8。
12. **引擎镜像分发走 ACR 个人版独立实例**（2026-11-03）：push = 公网端点（构建侧，节点或本地），pull = VPC 端点（`-vpc` 后缀，集群内网免公网流量）；凭证 = chart 内 imagePullSecret `acr-credential`（dockerconfigjson，**只挂引擎 Deployment**，节点 registries.yaml 不动、不重启 k3s，其它镜像拉取链路零影响）。registry:2 退居 M5 流水线产物专用，两条链路互不干扰。ACR 的坑见 §9.12。

## 5. 常用命令

```bash
# ===== 部署循环（改代码后的标准姿势） =====
# 1) 构建镜像进 ACR（在 worker-1 上 build+push；--provenance=false 是 ACR 硬要求，脚本已内置）
deploy/build-push.sh 0.2.2
# 2) 升级 release（注意 helm v4 会复用上次 --set，覆盖要显式）
helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=0.2.2 --set tags.engine=true --wait

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

go test ./repo/ -v -run TestNextID   # 需先 port-forward mongo
```

## 6. 写代码时遵守的设计原则（从 Zadig 实读得出）

1. **暂停的实现**：`stage.Status = pause` 写 Mongo → goroutine 直接 return（不 sleep 不占线程）→ 人调 release API 标记 `ManualExec.Executed = true` → 任务重跑 → `RunStages` 跳过已 passed 的 stage → 闸门放行。暂停多久都不丢。
2. **资源与数据分离**：K8s 资源是消耗品（结束/暂停时清理），Mongo 记录是档案（跑完不删，只受管理员保留策略约束）。
3. **状态聚合最差者胜**：Zadig 原版 cancelled(7)>timeout(6)>failed(5)>pause(4)>reject(3)>passed(2)>unstable(1)>skipped(0)。**学习版已落地**（`models/status.go` 的 statusWeight/WorstOf）：cancelled(7)>failed(6)>pause(5)>running(4)>passed(3)>created(2)>skipped(1)——running 排在 passed 前（没跑完不许宣布成功），skipped 垫底（被刹车跳过的 job 不改变聚合结果）。
4. **ack() 全量写**：任务每前进一步把整棵树 ReplaceOne 写回，不做增量 patch。前提：`_id` 是插入前定好的自然键字符串（`<wf>-<taskID>`）——空 `_id` 插入会让 Mongo 生成 ObjectId 而结构体拿不回，ack 从此静默失效（M1 踩过）。
5. **变量渲染**：JSON marshal → 字符串 `{{.key}}` 替换 → unmarshal（M4 照抄 Zadig 的"粗暴"方案）。
6. **构建 job 的真身是 freestyle + 步骤列表**（2026-11-03 核对实证，见 §7 新增行）：`zadig-build` 在 `initJobCtl` 的 switch 里没有专门 case，落 default = FreestyleJobCtl。步骤（git → shell 编译 → docker_build → …）在 **Pod 内**由 jobexecutor 顺序执行（失败且非 OnFailure 则跳过剩余步骤），步骤状态经 ConfigMap（名字=K8sJobName）回传、aslan 每秒轮询。"分节点分界面"的数据源就是 workflow_task 里 stages→jobs→steps 三级状态树，前端无特殊机制。我们做步骤化时可用 bash 驱动脚本替代 jobexecutor 二进制（ConfigMap 放 steps JSON，逐条执行回写）。

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

## 8. 下一步（M3）具体要做什么

> 2026-11-03 状态：用户当前方向是"分节点分界面的编译构建 + 统一生命周期管理"，Zadig 核对报告已交（§6.6 是结论）——步骤化方案 A（bash executor + ConfigMap 回传）vs B（不加步骤层）待拍板；M3 暂缓但不废弃（闸门设计与 Zadig stage.go 的 waitForManualExec 已逐行核对一致）。

1. `engine/run.go` 的 `runStages` 里加闸门（对照 Zadig stage.go 的 `waitForManualExec`）：stage 开跑前若 `ManualExec.Enabled && !Executed` → `stage.Status = pause` 落库 → **goroutine 直接 return**（不 sleep 不占线程）。
2. release 端点（api/run.go + router）：POST `/api/tasks/:name/:id/release` → 从 Mongo 读最新 task → 置对应 stage 的 `ManualExec.Executed = true` → 重新 `go engine.Run(t)` 续跑。注意 Run() 入口现在无条件置 running/StartTime，恢复路径要兼容（StartTime 别覆盖，或首次标记）。
3. 续跑正确性：`runStages` 已会跳过 passed/skipped stage（M2 埋好的），M3 直接受益；恢复时 task.Status pause → running 的翻转在 Run 或 release 里做，别做两处。
4. 验收：两 stage 流水线，stage2 带 `manual_exec` → 跑到 pause → **kill 引擎 Pod**（`kubectl -n pipeline delete pod -l app=mini-pipeline`）→ 查 Mongo task 仍 pause（durable 证明）→ release → 续跑到 passed。

## 9. 案例存档 / 已知坑

1. **【已解决】worker-2 跨节点 Pod 网络不通**（2026-11-01，教科书级案例）：
   - **真凶在 worker-1**：keepalived 的 HaVip `172.29.237.252` 挂在 w1 eth0 上（EIP 47.113.177.198 绑此 VIP 做 ingress 高可用，w1=MASTER/w2=BACKUP）。k3s flannel 启动时扫 eth0 选中这个 VIP 作 vxlan 源地址 → VPC 反欺骗校验丢弃一切源=VIP 的包（VIP 只许入不许主动发出）→ 所有涉及 w1 的跨节点 vxlan 全灭。master↔w2 一直是健康的。
   - **解法（改 keepalived，不动 flannel）**：VIP 从 eth0 挪到 `dummy0` 接口——`virtual_ipaddress { 172.29.237.252/32 dev dummy0 }`，VRRP 报文仍走 eth0；dummy0 由 keepalived 的 systemd drop-in（`/etc/systemd/system/keepalived.service.d/dummy0.conf`，ExecStartPre 建 dummy0）持久化。eth0 只剩主 IP → flannel 自然选对。两台 worker 都改了（保证 failover 后行为一致）。ingress 经 EIP→HaVip→dummy0 验证 200 无恙。
   - **教训**：flannel 的 public-ip 注解是它的输出不是输入（手工改了会被重启覆写，实验两次证实）；host-gw 后端在阿里云 VPC 是死路（VPC 网络层按 ENI 注册表丢裸 Pod 源地址包，加安全组放行+关 SourceDestCheck 都救不了，实测）；wireguard-native 也不必了（dummy0 方案最干净，保持默认 vxlan）。
   - **运维注意**：`kubectl get nodes` 若再见 w1 的 `flannel.alpha.coreos.com/public-ip` 变成 .252 → 说明 keepalived 的 VIP 跑回 eth0 了（比如有人还原了配置）。
2. **keepalived 底稿未进仓**：`/etc/keepalived/keepalived.conf` 注释里写"仓库底稿:system/keepalived/"，但 k3s-gitops 仓库（github.com/Edward-258/k3s-gitops，私有）里没有该目录——节点层配置是单点，dummy0 改动目前只活在那两台机器上。建议补进 gitops 仓库。
3. Windows PowerShell 5.1 四坑已入档：中文路径的 .ps1 必须无中文或参数传入（编码）；curl JSON 必须 `-d @file`（吞引号）；远程 bash 里的 `sed '$d'` 要写成 `\$d`；**SSH 远程命令内层引号必被剥——传配置文件/脚本一律 base64**（`echo <b64> | base64 -d > file` 或 `| bash`，本项目已踩三次）。
4. helm v4：upgrade 默认复用上次 --set 值（v3 行为相反），覆盖必须显式。
5. 删 K8s Job 默认保留子 Pod，必须 `PropagationPolicy: Background`（engine/run.go 的 deleteOpts() 已封装）。
6. `flannel-backend` 是 k3s **server 专属**参数，agent 配了会起不来（flag not defined）；worker 的 backend 由 master 下发。
7. PowerShell 5.1 再添两坑（2026-11-01，都在 M2 期间踩的）：**手拼 JSON 字符串必数错括号**——用 hashtable + `ConvertTo-Json` 构造（见 test-m2.ps1 的 Job/Ensure-Workflow 函数）；**`Set-Content -Encoding ASCII` 会把文件里的中文全毁成 `?`**（批量替换 values.yaml 时差点全毁，`git diff` 出现 32 行异常变更才发现）——改文件走编辑工具，批量文本操作后必 `git diff` 自检。
8. **helm→argocd 资源移交**（2026-11-02，registry:2 实操）：直接让 ArgoCD SSA 接管 helm 部署的资源会打架（`managed-by: Helm` 标签的字段所有权冲突）。正确三步：① 活体 PVC 先打 `helm.sh/resource-policy: keep` 注解——否则 helm upgrade 摘除时连数据卷一起删（默认回收策略下 PV 也没了，镜像全灭）；② `helm upgrade --set tags.<x>=false` 摘除资源；③ 新清单**逐字段抄活体**（storageClassName 等默认值都要抄）先 `kubectl apply` 铺上再推 git，ArgoCD 认领零漂移（出现即 Synced/Healthy），停机窗口秒级。附带发现：ArgoCD 对新 app 的发现依赖 root app 刷新（默认 ~3min 轮询），等不及可 `kubectl patch application root -n argocd -p '{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"normal"}}}' --type merge` 手动触发（UI 的 Refresh 按钮同款）。
9. **ACR 个人版推送三坑**（2026-11-03，引擎镜像迁 ACR 实录）：① 新版个人版是"独立实例"形态，专属域名带 `crpi-` 前缀（本实例 `crpi-28fqgwft325ylccc.cn-heyuan.personal.cr.aliyuncs.com`）；老共享域名 `registry.cn-heyuan.aliyuncs.com` 对它一律 **Forbidden Host**（端点活着但不是它的家，别被 401 挑战骗了）。② ACR 拒收 BuildKit 默认生成的 provenance/attestation 附加清单，报 `unknown manifest class for application/vnd.oci.empty.v1+json`；`docker build --provenance=false` 即解，层已推上去不受影响（重推全命中 Layer already exists）。③ 认证是 Bearer token 流：`curl -u` 直怼 `/v2/` 永远 401（不代表凭证错，我为此冤枉猜过三种用户名格式）；正经测法 = 先去 `dockerauth-cn-heyuan.aliyuncs.com/auth?service=…&scope=…` 换 token，且返回字段叫 `access_token` 不是 `token`（jq 取 .token 会拿到 null → 报 Forbidden Host 的迷惑性错误）。仓库无需预建，push 自动创建。aliyun CLI 的 `cr` 元数据 API（老 ROA 接口）本版本调不通，registry HTTP API 完全够用。④ **helm chart 存不进免费版**（2026-11-03 实测）：`helm push` OCI 形式报 `unknown manifest class for application/vnd.cncf.helm.config.v1+json`——manifest 白名单只认镜像类，chart 托管是 EE 能力。chart 真相源留在 git 仓库即可，镜像走 ACR / chart 走 git 本就是标准分工。
