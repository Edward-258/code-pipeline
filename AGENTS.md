# AGENTS.md — mini-pipeline 项目上下文（给下一个会话的你）

> 任何 AI 会话开始于此目录时，先读完本文件再动手。内容按"必读 → 速查 → 参考"排序。

## 0. 铁律

1. **Zadig 模板仓库绝对只读**：`E:\code pipeline学习模板(基于zadig项目)\zadig` 只能读代码做对照，改一个字节都不行。本项目（`E:\code pipeline的小范围复现`）是独立的复现项目。
2. 本项目是**学习性质的微型复现**，不是产品：砍掉了 RBAC/审批/IM/多集群，代码宁可直接不要过度设计。
3. 集群是临时的学习环境（master 节点 2026-11-16 到期），别在上面跑任何重要东西。

## 1. 项目定位

以 Zadig（本地模板，HEAD `a1fe8122e`，v5.0.0-dev 线）为蓝本的微型发布工作流引擎：

- Go + gin + mongo-driver(v1) + client-go，单体服务，module 名 `pipeline`
- MongoDB 三集合：`workflow`（图纸）/ `workflow_task`（施工档案）/ `counter`（发号器）
- Job = K8s Job：主控只创建/轮询/清理，不执行脚本
- 目标集群：阿里云河源 3×ECS k3s

**三条灵魂（及格线，缺一不算复现）**：
1. 图纸/实例分离，task 内嵌当时的完整定义快照
2. Stage 状态机 + **durable 暂停**（暂停=落库+goroutine 退出，进程重启可恢复）
3. Job = K8s Job

## 2. 当前进度（截至 2026-11-01）

**M0 已完成并提交（git `f4b16d2`）**：三集合模型 + counter 发号（集成测试过：1→2 严格递增）+ workflow CRUD + 校验。MongoDB 已部署进 k3s 并验收。

里程碑总览（详细验收标准见桌面 `mini-zadig-计划.md`）：

- [x] M0 地基
- [ ] **M1 第一口气（下一步）**：`POST /api/workflows/:name/run` → counter 发 taskID → 生成带快照的 workflow_task → 在 k3s 创建 K8s Job（busybox echo）→ 轮询 → ack 落库 → 清理 Job
- [ ] M2 状态机：多 stage 串行 + stage 内并行 + 最差者胜聚合 + 失败刹车
- [ ] M3 灵魂时刻：manual_exec 暂停（落库+退出）→ kill 进程重启验证恢复 → release 端点放行续跑
- [ ] M4 变量传递（global_context，JSON→字符串替换→反序列化）+ 单 job 重试
- [ ] M5 真构建：fat runner（docker CLI）+ 挂宿主 docker.sock + registry:2 + 真 Go 项目构建推镜像
- [ ] M6 半吊子前端（单 HTML + fetch 轮询，不上框架）

已有代码结构：`config/`(env) `models/`(workflow/task/counter/status) `repo/`(mongo/counter/workflow/task) `api/`(gin CRUD) `deploy/`(k3s 清单) `main.go`。`repo.UpdateTask` 就是未来 ack() 的落点（ReplaceOne 整树替换）。

## 3. 环境速查（全部实测可用）

| 项 | 值 |
|---|---|
| k3s master | 8.155.137.224（内网 172.29.237.251，control-plane+etcd） |
| k3s worker | 8.155.128.91（worker-1）、8.155.128.81（worker-2） |
| kubectl context | `k3s-heyuan`（已合并进本机 kubeconfig，insecure-skip-tls-verify 因证书 SAN 无公网 IP） |
| 集群运行时 | containerd://2.3.4-k3s1.36（**k3s 保持 containerd 不换引擎，已定稿**） |
| MongoDB | k3s namespace `pipeline` 内 deployment，NodePort **30017**，认证 root / mini-pipeline-2026，PVC 5Gi local-path |
| 默认连接串 | `mongodb://root:mini-pipeline-2026@8.155.137.224:30017`（已写在 config/config.go） |
| SSH | `ssh -i "E:\aliyun-pem\河源81.pem" root@<节点IP>`，三台全通，默认 root |
| aliyun CLI | 已配 AK/SK（profile default，region cn-hangzhou，查询时显式 `--RegionId cn-heyuan`） |
| 安全组 | `sg-f8z8wcoum4myexrjfr40`，已加放行 TCP 30017（描述 mini-pipeline mongo nodeport）；M5 时需再加 registry 的端口 |
| 节点到期 | master 是 PrePaid，**2026-11-16 到期**；两台 worker 按量付费长期 |

## 4. 已定稿的架构决断（不要再讨论）

1. **镜像构建走路线①**：fat image（docker CLI）+ 挂宿主 `/var/run/docker.sock`（对应 Zadig `UseHostDockerDaemon=true` 模式）。节点上有全套 docker，k3s 用 containerd 跑 Pod——两个镜像仓库互不相认，所以 **registry:2 是必选项**（构建→push→kubelet 从 registry 拉）。备选路线②（containerd.sock+nerdctl）已评估不采用。
2. 状态获取用**轮询**（每 2s 查 K8s Job），不上 watch。
3. runner 不主动回调：**Pod 退出码 = job 结果**，日志用 API 拉。
4. 第一版不做队列/并发控制。
5. 挂 docker.sock 的 Pod ≈ 节点 root，仅限学习集群，fat image 永不推公网。
6. Mongo 密码明文写在 deploy/mongo.yaml 和 config/config.go——学习版接受，此仓库别推公开远端。

## 5. 常用命令

```powershell
# 起服务（本机 8080）
cd "E:\code pipeline的小范围复现"; go run .

# 集群
kubectl --context k3s-heyuan get nodes -o wide
kubectl --context k3s-heyuan get pods -n pipeline

# 直连集群里的 Mongo 看数据
@'
db = db.getSiblingDB("pipeline")
printjson(db.getCollectionNames())
'@ | kubectl exec -i -n pipeline deploy/mongo -- mongosh -u root -p mini-pipeline-2026 --quiet
# 注意：PowerShell 下 kubectl exec --eval 的引号会被吃，务必走 stdin 管道

# 发号器集成测试
go test ./repo/ -v -run TestNextID
```

## 6. 写代码时遵守的设计原则（从 Zadig 实读得出）

1. **暂停的实现**：`stage.Status = pause` 写 Mongo → goroutine 直接 return（不 sleep 不占线程）→ 人调 release API 标记 `ManualExec.Executed = true` → 任务重跑 → `RunStages` 跳过已 passed 的 stage → 闸门放行。暂停多久都不丢。
2. **资源与数据分离**：K8s 资源是消耗品（结束/暂停时清理），Mongo 记录是档案（跑完不删，只受管理员保留策略约束）。
3. **状态聚合最差者胜**：cancelled(7) > timeout(6) > failed(5) > pause(4) > reject(3) > passed(2) > unstable(1) > skipped(0)（学习版砍到 7 态）。
4. **ack() 全量写**：任务每前进一步把整棵树 ReplaceOne 写回，不做增量 patch。
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

## 8. 下一步（M1）具体要做什么

1. `api` 加 `POST /api/workflows/:name/run`：查图纸 → `repo.NextID("workflow_task:"+name)` 发 taskID → 构造 `WorkflowTask`（**快照整个图纸进 workflow_args**、stages 转成 StageTask/JobTask、status=created）→ `CreateTask` 落库 → `go engine.Run(task)` 异步执行。
2. `engine/` 包：client-go（读本机 kubeconfig，context k3s-heyuan）在 namespace `pipeline` 创建 K8s Job（busybox，脚本用 ConfigMap 或 command 拼 shell -c），轮询 Job 状态（Complete/Failed），回写 JobTask.Status，最终 ack（`repo.UpdateTask` 整树）+ 删除 K8s Job。
3. `api` 加 `GET /api/tasks/:name/:id`。
4. 验收：run 后 `kubectl get jobs -n pipeline` 能看到 Job 出现又消失；Mongo 里 task 文档状态从 created→running→passed 流转。
