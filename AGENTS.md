# AGENTS.md — mini-pipeline 项目上下文（给下一个会话的你）

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

## 2. 当前进度（截至 2026-11-01）

**M0 + M1 已完成**。M1 = run 端点 → counter 发号 → 快照落库 → K8s Job 创建/轮询/清理 → ack 全量写；引擎已镜像化部署进 k3s，经 ingress 对外。两个生产级 bug 的教训已固化进代码注释：task `_id` 必须插入前定为自然键 `<wf>-<taskID>`（否则 ack 全部静默失效）；删 Job 必须显式 `PropagationPolicy: Background`（否则留孤儿 Pod）。

里程碑总览（详细验收标准见桌面 `mini-zadig-计划.md`）：

- [x] M0 地基
- [x] **M1 第一口气**：run → K8s Job → 轮询 → ack → 清理（本地与集群双验收过）
- [ ] M2 状态机：多 stage 串行 + stage 内并行 + 最差者胜聚合 + 失败刹车
- [ ] M3 灵魂时刻：manual_exec 暂停（落库+退出）→ kill 进程重启验证恢复 → release 端点放行续跑
  - 集群形态下 kill 进程 = `kubectl -n pipeline delete pod -l app=mini-pipeline`，比 Ctrl+C 更真实
- [ ] M4 变量传递（global_context，JSON→字符串替换→反序列化）+ 单 job 重试
- [ ] M5 真构建：fat runner（docker CLI）+ 挂宿主 docker.sock + 真 Go 项目构建推镜像（registry 已就位，M5 直接用）
- [ ] M6 半吊子前端（单 HTML + fetch 轮询，不上框架）

已有代码结构：`config/` `models/` `repo/` `api/`(CRUD+run/task) `engine/`(k8s.go 双模式 client / job.go 构建 / run.go 执行器) `docker/Dockerfile` `deploy/chart/mini-pipeline/`(helm chart) `deploy/build-push.ps1`(镜像循环)。

## 3. 环境速查（全部实测可用）

| 项 | 值 |
|---|---|
| k3s master | 8.155.137.224（内网 172.29.237.251，control-plane+etcd，**2026-11-16 到期**） |
| k3s worker-1 | 8.155.128.91（内网 172.29.237.250） |
| k3s worker-2 | 8.155.128.81（内网 172.29.237.249）⚠️ **已 cordon：pod 跨节点网络不通**（见 §9 遗留问题） |
| kubectl context | `k3s-heyuan` |
| MongoDB | ns `pipeline`，**ClusterIP**（chart 内置），本机调试走 `kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017` |
| registry:2 | chart 内置，NodePort **30500 仅内网**；节点 docker push `127.0.0.1:30500/…`；kubelet 拉镜像走 registries.yaml mirror |
| 引擎入口 | `https://pipeline.47.113.177.198.nip.io`（复用集群现成 nip.io+ingress-nginx 链路，SG 80/443 已放行） |
| 安全组 | `sg-f8z8wcoum4myexrjfr40`；**30017 已收掉**，现有 80/443/6443/22/3389 + 同组内网互通 |
| 集群存量 | argocd（ns argocd）、ingress-nginx（DaemonSet hostNetwork 双 worker）——都不是我们的，别动 |
| SSH | `ssh -i "E:\aliyun-pem\河源81.pem" root@<节点IP>`，三台全通 |
| aliyun CLI | 已配 AK/SK（profile default，查询时显式 `--RegionId cn-heyuan`） |

## 4. 已定稿的架构决断（不要再讨论）

1. **引擎跑在集群内**（2026-11-01 拍板，推翻更早的"本机跑"方案）：InClusterConfig + SA `pipeline-engine`（Role 仅 jobs/pods 权限）；代码保留 kubeconfig fallback 供本机调试（`K8S_CONTEXT=k3s-heyuan`）。
2. **部署形态对齐 Zadig = helm chart**：`deploy/chart/mini-pipeline`，`tags.{engine,mongo,registry}` 组件开关 + `connections.mongo.uri` 参数化（对照 Zadig chart 的 tags.mongodb / connections 模式）。⚠️ Helm v4 升级默认**复用**上次 --set 值，覆盖要显式 `--set`。
3. **registry:2 提前建**（原 M5 组件）：镜像名统一 `localhost:5000/mini-pipeline:<tag>`，push 侧节点 docker 推 `127.0.0.1:30500`，pull 侧三节点 registries.yaml 把 `localhost:5000` mirror 到三个节点内网 IP:30500（不能只配 127.0.0.1——worker-2 回环 NodePort 不通，见 §9）。registry 存储会剥离 host 前缀，两边镜像名天然一致。
4. **镜像构建在节点上做**（本机 Docker Desktop 常年没开）：`deploy/build-push.ps1 -Tag x.y.z` 一条命令 = 本机打包源码 → scp worker-1 → docker build（多阶段 golang:1.26-alpine，GOPROXY=goproxy.cn）→ push registry。
5. **镜像构建走路线①**（M5 生效）：fat image（docker CLI）+ 挂宿主 `/var/run/docker.sock`；k3s 是 containerd，两个镜像仓库互不相认，registry:2 是必选项。
6. 状态获取用**轮询**（每 2s 查 K8s Job），不上 watch。
7. runner 不主动回调：**Pod 退出码 = job 结果**，日志用 API 拉。
8. 第一版不做队列/并发控制。
9. 挂 docker.sock 的 Pod ≈ 节点 root，仅限学习集群，fat image 永不推公网。
10. Mongo 密码明文写在 chart values 和 config/config.go——学习版接受，此仓库别推公开远端。

## 5. 常用命令

```powershell
# ===== 部署循环（改代码后的标准姿势） =====
# 1) 构建镜像进 registry（在 worker-1 上 build+push）
powershell -ExecutionPolicy Bypass -File deploy\build-push.ps1 -Tag 0.1.2
# 2) 升级 release（注意 helm v4 会复用上次 --set，覆盖要显式）
helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=0.1.2 --set tags.engine=true --wait

# ===== 本机调试（引擎跑本机，连集群 mongo/创建集群 Job） =====
kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017   # 窗口A
$env:MONGO_URI="mongodb://root:mini-pipeline-2026@localhost:27017"; $env:K8S_CONTEXT="k3s-heyuan"; go run .   # 窗口B
# ⚠️ curl 发 JSON 必须用 -d @文件（PowerShell 5.1 传参会吞引号），参考 C:\Temp\opencode\test-m1.ps1 的 Api 函数

# ===== 集群 =====
kubectl --context k3s-heyuan get pods -n pipeline
kubectl --context k3s-heyuan get jobs -n pipeline        # 验收：mp-N-… 出现又消失

# ===== 直连集群里的 Mongo 看数据 =====
@'
db = db.getSiblingDB("pipeline")
printjson(db.getCollectionNames())
'@ | kubectl exec -i -n pipeline deploy/mongo -- mongosh -u root -p mini-pipeline-2026 --quiet
# 注意：PowerShell 下 kubectl exec --eval 的引号会被吃，务必走 stdin 管道

go test ./repo/ -v -run TestNextID   # 需先 port-forward mongo
```

## 6. 写代码时遵守的设计原则（从 Zadig 实读得出）

1. **暂停的实现**：`stage.Status = pause` 写 Mongo → goroutine 直接 return（不 sleep 不占线程）→ 人调 release API 标记 `ManualExec.Executed = true` → 任务重跑 → `RunStages` 跳过已 passed 的 stage → 闸门放行。暂停多久都不丢。
2. **资源与数据分离**：K8s 资源是消耗品（结束/暂停时清理），Mongo 记录是档案（跑完不删，只受管理员保留策略约束）。
3. **状态聚合最差者胜**：cancelled(7) > timeout(6) > failed(5) > pause(4) > reject(3) > passed(2) > unstable(1) > skipped(0)（学习版砍到 7 态）。
4. **ack() 全量写**：任务每前进一步把整棵树 ReplaceOne 写回，不做增量 patch。前提：`_id` 是插入前定好的自然键字符串（`<wf>-<taskID>`）——空 `_id` 插入会让 Mongo 生成 ObjectId 而结构体拿不回，ack 从此静默失效（M1 踩过）。
5. **变量渲染**：JSON marshal → 字符串 `{{.key}}` 替换 → unmarshal（M4 照抄 Zadig 的"粗暴"方案）。

## 7. Zadig 参考文件地图（写对应模块前先读）

| 主题 | 模板仓库内路径 |
|---|---|
| 蓝图模型 | `pkg/microservice/aslan/core/common/repository/models/workflow_v4.go` |
| 运行实例模型 | `.../models/wokflow_task_v4.go`（文件名拼写错误是上游的） |
| **Stage 引擎（灵魂）** | `pkg/microservice/aslan/core/common/service/workflowcontroller/stage.go` |
| Run 主循环 + ack | 同目录 `workflow.go` |
| Job 执行/清理 | 同目录 `jobcontroller/job.go`（蓝绿豁免 565 行） |
| K8s Job 生成 | 同目录 `jobcontroller/kubernetes.go`（docker.sock 挂载 832 行、DOCKER_HOST 注入 745 行） |
| counter 发号器 | `.../models/counter.go`（22 行） |
| ack 写模式 | `.../mongodb/workflow_task_v4.go:318` |
| 状态常量 | `pkg/microservice/aslan/config/consts.go:106-125` |
| 变量渲染 | `.../workflow/controller/workflow.go`（ToJobTasks） |
| 保留策略 | `.../system/service/capacity.go` |
| helm chart 形态 | 独立 chart 仓库 `koderover-chart/zadig`（tags.* + connections.* 参数化，主仓库只出 docker/*.Dockerfile） |

## 8. 下一步（M2）具体要做什么

1. `engine/run.go` 的裸双层循环升级为真正的 `RunStages`：stage 顺序不变，stage 内按 `Parallel` 标志并行/串行执行 job（对照 Zadig stage.go 的 `runStage`）。
2. 聚合规则落地：stage/job 状态用"最差者胜"聚合出任务终态（`models/status.go` 加权重函数）。
3. 失败刹车：stage 内任一 job 失败 → 同 stage 其余 job 跑完不再起新的 → 后续 stage 全部 skipped。
4. 验收：三 stage 流水线（含一个失败 stage）跑一遍，终态与各 stage/jobs 状态符合预期。

## 9. 遗留问题 / 已知坑

1. **worker-2 pod 跨节点网络不通**（2026-11-01 发现，先 cordon 绕过）：症状=该节点 Pod 连 CoreDNS/mongo svc/跨节点 Pod IP 全部超时，但节点物理网正常、host 发起的 NodePort DNAT 正常。该节点 iptables/nft 的 KUBE- 规则数量正常、kube-router 链正常、rp_filter=0、conntrack 正常——根因未明（net-check pod 3 天前就重启过，疑似存量问题）。修法方向：对比 flannel.1 vxlan 转发、br_netfilter、或直接 reboot worker-2 再 uncordon。修复前**一切调度避开 worker-2**。
2. Windows PowerShell 5.1 三坑已入档：中文路径的 .ps1 必须无中文或参数传入（编码）；curl JSON 必须 `-d @file`（吞引号）；远程 bash 里的 `sed '$d'` 要写成 `\$d`（$ 会被远端展开）。
3. helm v4：upgrade 默认复用上次 --set 值（v3 行为相反），覆盖必须显式。
4. 删 K8s Job 默认保留子 Pod，必须 `PropagationPolicy: Background`（engine/run.go 的 deleteOpts() 已封装）。
