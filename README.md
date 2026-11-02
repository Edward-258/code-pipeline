# mini-pipeline（Zadig 微型复现）

以 Zadig 为蓝本的微型发布工作流引擎：图纸/实例分离 + stage 状态机 + durable 暂停 + Job=K8s Job。
引擎本体跑在 k3s 里（helm chart 部署），API 经 ingress-nginx 暴露，全内网通信。

## 结构

```
config/            环境配置（MONGO_URI / K8S_NAMESPACE / K8S_CONTEXT）
models/            三集合模型（workflow / workflow_task / counter）+ 状态常量
repo/              MongoDB 仓储层（counter 发号器 / ack 全量写）
api/               gin HTTP 层（workflow CRUD + run + task 查询）
engine/            执行器（双模式 K8s client / K8s Job 构建 / Run 主循环）
docker/            引擎镜像（多阶段，实际构建发生在集群节点上）
deploy/
  chart/mini-pipeline/   helm chart（tags.{engine,mongo,registry} 组件开关，对照 Zadig chart 模式）
  build-push.sh           镜像构建分发一条龙（打包→scp→节点 build→push 内置 registry）
```

## 快速开始

```bash
# 一条命令起全家桶（mongo/registry/引擎）
helm install mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --create-namespace
#    只起基础设施不起引擎时：--set tags.engine=false（引擎镜像需先进 registry）

# 引擎入口（复用集群现成的 ingress + nip.io 链路）
curl -k https://pipeline.47.113.177.198.nip.io/healthz

# 建一条工作流并跑起来
curl -X POST -k https://pipeline.47.113.177.198.nip.io/api/workflows -H "Content-Type: application/json" -d @wf.json
curl -X POST -k https://pipeline.47.113.177.198.nip.io/api/workflows/demo/run
curl -k https://pipeline.47.113.177.198.nip.io/api/tasks/demo/1
```

wf.json 示例：

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

## 镜像分发链路（M5 前无外部仓库的解法）

```
push: 节点上 docker push 127.0.0.1:30500/mini-pipeline:<tag>（NodePort 仅内网，安全组不放公网）
pull: 三节点 /etc/rancher/k3s/registries.yaml 把 "localhost:5000" mirror 到各节点内网 IP:30500
      → 镜像名两边一致（registry 存储剥离 host 前缀），kubelet 无感知
改代码后的标准循环：
  deploy/build-push.sh <new-tag>
  helm upgrade mini-pipeline deploy/chart/mini-pipeline --kube-context k3s-heyuan -n pipeline --set image.tag=<new-tag> --set tags.engine=true --wait
```

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| MONGO_URI | mongodb://root:…@mongo.pipeline.svc.cluster.local:27017 | 集群内 svc DNS；本机调试先 port-forward 再覆盖为 localhost:27017 |
| HTTP_ADDR | :8080 | 监听地址 |
| K8S_NAMESPACE | pipeline | K8s Job 所在 namespace |
| K8S_CONTEXT | （空） | 仅本机调试：kubeconfig context（如 k3s-heyuan）；集群内留空走 InClusterConfig |

## 里程碑

- [x] M0 地基：三集合 + counter 发号 + CRUD
- [x] M1 第一口气：run → K8s Job → 轮询 → ack → 清理；引擎 helm 化进集群
- [x] M2 多 stage 状态机（并行 + 最差者胜 + 失败刹车）
- [ ] M3 durable 暂停/恢复（灵魂；kill pod 验证）
- [ ] M4 变量传递 + 重试
- [ ] M5 fat runner + 真 Go 项目构建推镜像（registry 已就位）
- [ ] M6 半吊子前端

更多工程上下文（集群状态、已踩的坑、设计决断）见 AGENTS.md。
