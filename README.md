# rule-engine

基于 KubeEdge 的边缘规则引擎。订阅边缘节点本地 MQTT Broker，按规则对传感器数据进行过滤、转换或路由，再将结果转发到云端 MQTT Broker 或第三方 HTTP 接口。

## 架构概览

```
边缘设备/传感器
      │  MQTT Publish
      ▼
边缘 MQTT Broker (localhost:1883)
      │  Subscribe (topics: sensors/batch, ...)
      ▼
┌─────────────────────────────────────┐
│           rule-engine               │
│                                     │
│  ┌─────────┐  ┌──────────────────┐  │
│  │ forward │  │ filter           │  │
│  │ rule    │  │ rule             │  │
│  └────┬────┘  └────────┬─────────┘  │
│       │                │            │
│  ┌────▼────┐  ┌────────▼─────────┐  │
│  │transform│  │ JS script        │  │
│  │ rule    │  │ rule (Goja)      │  │
│  └────┬────┘  └────────┬─────────┘  │
│       │                │            │
│  ┌────▼────────────────▼─────────┐  │
│  │   Target Router               │  │
│  │   MQTT Target / HTTP Target   │  │
│  └───────────────────────────────┘  │
│                                     │
│  HTTP 管理 API  :9090               │
└─────────────────────────────────────┘
      │
      ▼
云端 MQTT Broker / 第三方 HTTP 接口
```

## 功能特性

| 规则类型 | 描述 |
|---------|------|
| `forward` | 将消息原样转发到目标 |
| `filter` | 按字段条件过滤（`gt` / `lt` / `gte` / `lte` / `eq` / `neq` / `contains`），不匹配则丢弃 |
| `transform` | 字段级操作（`rename` / `add` / `remove`），标准化后转发 |
| `script` | 内联或外部 JS 脚本（[Goja](https://github.com/dop251/goja) 引擎），自由编程处理逻辑 |

| 目标类型 | 描述 |
|---------|------|
| `mqtt` | 推送到 MQTT Broker（支持重连、QoS 0/1/2） |
| `http` | POST/PUT 到 HTTP 接口，支持自定义 Header 和超时 |

**管理 API**（默认端口 `9090`）：

| 路径 | 说明 |
|-----|------|
| `GET /health` | 健康检查 |
| `GET /api/rules` | 查看所有规则状态（已处理/丢弃/错误计数） |
| `GET /api/rules/stats` | 全局汇总统计 |

## 目录结构

```
rule-engine/
├── cmd/
│   └── main.go              # 程序入口，初始化引擎，启动 HTTP 管理 API
├── config/
│   └── config.go            # 配置结构体与 YAML 解析
├── engine/
│   ├── engine.go            # 核心引擎：MQTT 订阅、规则分发、运行状态
│   └── rule.go              # Rule 接口定义 + 运行时包装（统计、错误处理）
├── rules/
│   ├── builtin/
│   │   ├── filter.go        # 过滤规则实现
│   │   ├── forward.go       # 转发规则实现
│   │   └── transform.go     # 字段变换规则实现
│   └── script/
│       └── js_rule.go       # JS 脚本规则实现（Goja）
├── target/
│   ├── target.go            # Target 接口定义
│   ├── mqtt.go              # MQTT 目标实现
│   └── http.go              # HTTP 目标实现
├── helm/
│   └── rule-engine/         # Helm Chart（用于 KubeEdge 边缘节点部署）
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
├── config.yaml              # 配置示例（包含所有规则类型用法）
├── Dockerfile               # 多阶段构建，支持 linux/amd64 & linux/arm64
└── go.mod
```

## 快速开始

### 本地运行

```bash
# 克隆项目
git clone https://github.com/kubeedge/rule-engine.git
cd rule-engine

# 编译
go build -o rule-engine ./cmd/main.go

# 运行（需要本地 MQTT Broker）
./rule-engine --config-file config.yaml
```

### Docker 运行

```bash
docker run -d \
  --name rule-engine \
  --network host \
  -v $(pwd)/config.yaml:/etc/rule-engine/config.yaml \
  harbor.zkjgy.online/library/rule-engine:latest
```

### Helm 部署到 KubeEdge 边缘节点

```bash
# 安装（指定边缘节点名称）
helm install rule-engine helm/rule-engine \
  --set nodeName=<your-edge-node-name> \
  --set engineConfig.source.broker="tcp://localhost:1883" \
  --set engineConfig.rules[0].target.broker="tcp://<cloud-mqtt>:1883"

# 查看状态
kubectl get pod -l app=rule-engine

# 查看运行中的规则统计
kubectl exec -it <pod-name> -- curl http://localhost:9090/api/rules/stats
```

## 配置说明

配置文件默认路径为 `/etc/rule-engine/config.yaml`，可通过 `--config-file` 参数指定。

```yaml
source:
  broker: "tcp://localhost:1883"   # 边缘 MQTT Broker 地址
  username: ""
  password: ""
  client_id: "rule-engine"
  qos: 0
  topics:                          # 订阅的 topic 列表
    - "sensors/batch"

http:
  port: 9090                       # 管理 API 端口

rules:
  - name: "forward-to-cloud"
    enabled: true
    source_topic: "sensors/batch"
    type: "forward"                # forward | filter | transform | script
    target:
      type: "mqtt"                 # mqtt | http
      broker: "tcp://192.168.1.100:1883"
      topic: "cloud/sensors/batch"
      qos: 0
```

### 规则类型详细说明

**filter 规则**

```yaml
type: "filter"
filter:
  field: "value"
  operator: "gt"        # gt | lt | gte | lte | eq | neq | contains
  threshold: "900"
```

**transform 规则**

```yaml
type: "transform"
transform:
  operations:
    - op: "rename"
      from: "name"
      to: "tag_name"
    - op: "add"
      field: "source_node"
      value: "edge-node-01"
    - op: "remove"
      field: "value_state"
```

**script 规则（内联 JS）**

```yaml
type: "script"
script: |
  function process(messages) {
    // messages 为传感器数据数组，返回处理后的数组
    // 返回 null 或空数组则丢弃该消息
    return messages.filter(m => m.value > 500);
  }
```

**script 规则（外部文件）**

```yaml
type: "script"
script_file: "/etc/rule-engine/scripts/my-rule.js"
```

**HTTP 目标**

```yaml
target:
  type: "http"
  url: "http://data-sink-service/api/v1/sensors"
  method: "POST"
  headers:
    Content-Type: "application/json"
    Authorization: "Bearer your-token"
  timeout_seconds: 10
```

## 开发指南

### 依赖

| 依赖 | 版本 | 用途 |
|-----|------|------|
| Go | 1.22+ | 编译运行 |
| [goja](https://github.com/dop251/goja) | v0.0.0-20240927 | JS 脚本执行引擎 |
| [paho.mqtt.golang](https://github.com/eclipse/paho.mqtt.golang) | v1.2.0 | MQTT 客户端 |
| [klog/v2](https://github.com/kubernetes/klog) | v2.120.1 | 结构化日志 |

### 构建

```bash
# 本地构建
go build ./...

# 构建 amd64 镜像
docker build --platform linux/amd64 -t rule-engine:dev .

# 构建 arm64 镜像（适合 KubeEdge 边缘节点）
docker build --platform linux/arm64 -t rule-engine:dev-arm64 .
```

### 添加新规则类型

1. 在 `rules/` 下新增实现文件，实现 `engine.Rule` 接口（`Name()`、`SourceTopic()`、`Process(payload []byte) ([]byte, error)`）
2. 在 `engine/engine.go` 的 `buildRule()` 函数中注册新类型

## CI/CD

项目使用 GitHub Actions 实现自动化构建和发布：

| 触发条件 | 执行步骤 |
|---------|---------|
| push 到 `main` 分支 | 构建 amd64 & arm64 镜像 → 推送到 Harbor → 合并 multi-arch manifest → 打包并推送 Helm Chart |
| 推送 tag（如 `v1.0.0`） | 以上所有步骤 + 保存镜像 tar.gz + 创建 GitHub Release（含镜像包和 Helm Chart） |

所需 GitHub Secrets：

| Secret | 说明 |
|--------|------|
| `HARBOR_USERNAME` | Harbor 仓库用户名 |
| `HARBOR_PASSWORD` | Harbor 仓库密码 |

## License

Apache 2.0
