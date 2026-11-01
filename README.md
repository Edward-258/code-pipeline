# mini-pipeline（Zadig 微型复现）

以 Zadig 为蓝本的微型发布工作流引擎：图纸/实例分离 + stage 状态机 + durable 暂停 + Job=K8s Job。

## 结构

```
config/   环境配置
models/   三集合模型（workflow / workflow_task / counter）+ 状态常量
repo/     MongoDB 仓储层（counter 发号器 / ack 全量写）
api/      gin HTTP 层
deploy/   k3s 基础设施清单（mongo 等）
```

## 快速开始

```bash
# 1. 一条命令起全家桶（mongo/registry/引擎）——引擎镜像需先推入 registry，见 AGENTS.md
helm install mini-pipeline deploy/chart/mini-pipeline -n pipeline --create-namespace
#    只起基础设施不起引擎时：--set tags.engine=false

# 2. 集群内 MongoDB 已是 ClusterIP，本机调试走隧道
kubectl --context k3s-heyuan port-forward -n pipeline svc/mongo 27017:27017

# 3. 建一条工作流
curl -X POST localhost:8080/api/workflows -H "Content-Type: application/json" -d @- <<'EOF'
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
EOF

curl localhost:8080/api/workflows
```

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| MONGO_URI | mongodb://root:mini-pipeline-2026@8.155.137.224:30017 | MongoDB 连接串 |
| HTTP_ADDR | :8080 | 监听地址 |
| K8S_NAMESPACE | pipeline | K8s Job 所在 namespace（M1+） |

## 里程碑

- [x] M0 地基：三集合 + counter 发号 + CRUD
- [ ] M1 run → K8s Job → 轮询 → ack
- [ ] M2 多 stage 状态机
- [ ] M3 durable 暂停/恢复（灵魂）
- [ ] M4 变量传递 + 重试
- [ ] M5 fat runner + registry 真构建
- [ ] M6 半吊子前端
