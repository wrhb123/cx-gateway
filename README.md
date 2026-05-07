# AI Proxy Gateway

高性能 AI API 代理与协议转换网关，支持多种 AI 提供商的统一接入。

## 特性

- **统一入口**: 兼容 OpenAI API 格式，客户端无需修改代码即可切换底层模型
- **多协议支持**: Claude、OpenAI Chat、OpenAI Images (DALL-E)、Codex Responses、Gemini
- **协议转换**: 自动将 OpenAI 格式请求转换为各提供商的原生协议
- **渠道编排**: 支持多个 API Key 配置，灵活管理不同渠道
- **故障转移**: 内置熔断器模式，自动切换健康渠道
- **负载均衡**: 支持轮询、加权、随机多种策略
- **模型路由**: 基于模型名称的模式匹配路由
- **Web 管理界面**: 内置管理控制台，可视化管理渠道和路由
- **高性能**: Go 原生并发，连接池优化，SQLite WAL 模式

## 快速开始

### 构建

```bash
go build -o ai-proxy-gateway ./cmd/server
```

### 运行

```bash
# 使用默认配置
./ai-proxy-gateway

# 自定义端口
SERVER_PORT=8080 ADMIN_PORT=8081 ./ai-proxy-gateway

# 自定义数据库路径
DATABASE_PATH=./data/proxy.db ./ai-proxy-gateway
```

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| SERVER_PORT | 8080 | 代理服务器端口 |
| ADMIN_PORT | 8081 | 管理界面端口 |
| DATABASE_PATH | data/proxy.db | SQLite 数据库路径 |

## API 使用

### 代理端点 (默认 :8080)

#### Chat Completions

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}],
    "max_tokens": 100
  }'
```

#### Image Generation

```bash
curl http://localhost:8080/v1/images/generations \
  -H "Content-Type: application/json" \
  -d '{
    "model": "dall-e-3",
    "prompt": "A beautiful sunset",
    "n": 1,
    "size": "1024x1024"
  }'
```

#### List Models

```bash
curl http://localhost:8080/v1/models
```

### 管理 API (默认 :8081)

#### 渠道管理

```bash
# 添加渠道
curl -X POST http://localhost:8081/api/channels \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My OpenAI Channel",
    "type": "openai_chat",
    "base_url": "https://api.openai.com",
    "api_key": "sk-xxx",
    "model": "gpt-4o",
    "priority": 10,
    "weight": 1,
    "enabled": true
  }'

# 列出渠道
curl http://localhost:8081/api/channels

# 更新渠道
curl -X PUT http://localhost:8081/api/channels/1 \
  -H "Content-Type: application/json" \
  -d '{...}'

# 删除渠道
curl -X DELETE http://localhost:8081/api/channels/1
```

#### 模型路由

```bash
# 添加路由
curl -X POST http://localhost:8081/api/routes \
  -H "Content-Type: application/json" \
  -d '{
    "pattern": "gpt-*",
    "channel_ids": [1, 2],
    "load_balance": "round_robin",
    "priority": 10
  }'

# 列出路由
curl http://localhost:8081/api/routes
```

#### 状态监控

```bash
# 查看所有渠道状态
curl http://localhost:8081/api/stats

# 重载配置
curl -X POST http://localhost:8081/api/reload
```

## 架构

```
                    ┌─────────────────┐
                    │   Client App    │
                    └────────┬────────┘
                             │ OpenAI API
                             ▼
                    ┌─────────────────┐
                    │  Proxy Server   │  :8080
                    │                 │
                    │  ┌───────────┐  │
                    │  │  Router   │  │── Model Pattern Match
                    │  └───────────┘  │
                    │  ┌───────────┐  │
                    │  │ Channel   │  │── Load Balancing
                    │  │  Manager  │  │
                    │  └───────────┘  │
                    │  ┌───────────┐  │
                    │  │Failover   │  │── Circuit Breaker
                    │  └───────────┘  │
                    │  ┌───────────┐  │
                    │  │ Protocol  │  │── Format Translation
                    │  │ Translator│  │
                    │  └───────────┘  │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
        ┌──────────┐  ┌──────────┐  ┌──────────┐
        │ OpenAI   │  │ Claude   │  │ Gemini   │
        │ API      │  │ API      │  │ API      │
        └──────────┘  └──────────┘  └──────────┘

                    ┌─────────────────┐
                    │  Admin Panel    │  :8081
                    │  Vue.js + HTML  │
                    └─────────────────┘
```

## 项目结构

```
ai-proxy-gateway/
├── cmd/server/
│   ├── main.go          # 入口，嵌入 Web 文件
│   └── web/
│       └── index.html   # 管理界面
├── internal/
│   ├── api/
│   │   └── server.go    # HTTP handlers (代理 + 管理)
│   ├── channel/
│   │   ├── manager.go   # 渠道管理与负载均衡
│   │   └── errors.go    # 错误定义
│   ├── db/
│   │   └── db.go        # SQLite 数据库操作
│   ├── failover/
│   │   └── failover.go  # 熔断器与故障转移
│   ├── models/
│   │   └── models.go    # 数据模型
│   ├── proxy/
│   │   └── handler.go   # 协议转换与代理逻辑
│   └── router/
│       └── router.go    # 模型路由
├── go.mod
└── README.md
```

## 许可证

MIT
