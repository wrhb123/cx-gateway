# AI Proxy Gateway 技术设计文档

## 1. 概述

AI Proxy Gateway 是一个高性能的 AI API 代理与协议转换网关，为多种 AI 提供商（Claude、OpenAI Chat、OpenAI Images、Codex Responses、Gemini）提供统一的 API 入口。客户端无需修改代码，只需通过标准的 OpenAI API 格式即可调用任意底层模型。

### 1.1 设计目标

| 目标 | 说明 |
|------|------|
| 统一入口 | 兼容 OpenAI API 格式，客户端零改造 |
| 协议转换 | 自动将 OpenAI 格式转为各提供商原生协议 |
| 高可用 | 熔断器 + 故障转移，单渠道故障不影响整体服务 |
| 高并发 | Go 原生并发 + 连接池优化 + SQLite WAL 模式 |
| 灵活路由 | 基于模型名称的模式匹配路由到指定渠道 |
| 可视化管理 | 内置 Web 控制台，零构建依赖 |

### 1.2 支持的协议

| 渠道类型 | 输入格式 | 输出协议 | 端点 |
|---------|---------|---------|------|
| OpenAI Chat | OpenAI | OpenAI (直连) | /v1/chat/completions |
| OpenAI Image | OpenAI | OpenAI (直连) | /v1/images/generations |
| Claude | OpenAI | Claude Messages API | /v1/messages |
| Gemini | OpenAI | Gemini GenerateContent API | /v1beta/models/... |
| Codex | OpenAI | OpenAI Responses API | /v1/chat/completions |

## 2. 系统架构

```
                    ┌──────────────────────────┐
                    │       客户端应用          │
                    │   (OpenAI SDK / curl)    │
                    └────────────┬─────────────┘
                                 │ OpenAI API 格式
                                 ▼
                    ┌──────────────────────────┐
                    │     代理服务器 :8080      │
                    │                          │
                    │  ┌────────────────────┐  │
                    │  │   模型路由器        │  │ ← 根据模型名匹配路由规则
                    │  │   (glob 模式匹配)   │  │   如 "gpt-*" → 渠道 [1,2]
                    │  └────────┬───────────┘  │
                    │           │              │
                    │  ┌────────▼───────────┐  │
                    │  │   渠道管理器        │  │ ← 负载均衡策略
                    │  │  轮询/加权/随机     │  │   选择最优渠道
                    │  └────────┬───────────┘  │
                    │           │              │
                    │  ┌────────▼───────────┐  │
                    │  │   故障转移管理器    │  │ ← 熔断器检查
                    │  │  (Circuit Breaker) │  │   closed/open/half-open
                    │  └────────┬───────────┘  │
                    │           │              │
                    │  ┌────────▼───────────┐  │
                    │  │   协议转换器        │  │ ← 格式翻译
                    │  │  OpenAI→Claude     │  │   OpenAI→Gemini
                    │  │  OpenAI→Gemini     │  │
                    │  └────────┬───────────┘  │
                    └───────────┼──────────────┘
                                │
              ┌─────────────────┼─────────────────┐
              ▼                 ▼                 ▼
        ┌──────────┐    ┌──────────┐      ┌──────────┐
        │ OpenAI   │    │ Claude   │      │ Gemini   │
        │ API      │    │ API      │      │ API      │
        └──────────┘    └──────────┘      └──────────┘

        ┌──────────────────────────┐
        │    管理服务器 :8081       │
        │                          │
        │  REST API + Web UI       │
        │  - 渠道 CRUD             │
        │  - 路由规则管理           │
        │  - 状态监控               │
        │  - 配置重载               │
        └──────────────────────────┘
```

## 3. 模块设计

### 3.1 项目结构

```
ai-proxy-gateway/
├── cmd/server/
│   ├── main.go              # 程序入口，双端口服务 + embed Web 资源
│   └── web/
│       └── index.html       # Vue 3 管理界面（零构建依赖）
├── internal/
│   ├── api/
│   │   └── server.go        # HTTP 处理器（代理 + 管理）
│   ├── channel/
│   │   ├── manager.go       # 渠道管理与负载均衡
│   │   └── errors.go        # 错误定义
│   ├── db/
│   │   └── db.go            # SQLite 数据库操作与表结构
│   ├── failover/
│   │   └── failover.go      # 熔断器与故障转移
│   ├── models/
│   │   └── models.go        # 数据模型定义
│   ├── proxy/
│   │   └── handler.go       # 核心代理逻辑与协议转换
│   └── router/
│       └── router.go        # 模型路由（glob 模式匹配）
├── go.mod
└── README.md
```

### 3.2 数据模型

#### 渠道 (Channel)

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 | 自增主键 |
| name | string | 渠道名称 |
| type | string | 渠道类型 (openai_chat/openai_image/claude/gemini/codex) |
| base_url | string | API 基础地址 |
| api_key | string | API 密钥 |
| model | string | 目标模型名称 |
| priority | int | 优先级（数值越大越优先） |
| weight | int | 权重（用于加权负载均衡） |
| enabled | bool | 是否启用 |
| max_retries | int | 最大重试次数 |
| timeout | int | 超时时间（秒） |

#### 模型路由 (ModelRoute)

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 | 自增主键 |
| pattern | string | 匹配模式（glob，如 "gpt-*"） |
| channel_ids | string | 关联渠道 ID（逗号分隔） |
| load_balance | string | 负载均衡策略 |
| priority | int | 优先级 |
| enabled | bool | 是否启用 |

#### 渠道统计 (ChannelStats)

| 字段 | 类型 | 说明 |
|------|------|------|
| channel_id | int64 | 渠道 ID（外键） |
| total_requests | int64 | 总请求数 |
| success_count | int64 | 成功数 |
| failure_count | int64 | 失败数 |
| avg_latency_ms | int64 | 平均延迟（毫秒） |
| is_healthy | bool | 是否健康 |
| last_error | string | 最后错误信息 |

### 3.3 核心模块详解

#### 3.3.1 协议转换模块 (proxy/handler.go)

**职责**：将 OpenAI 标准格式请求转换为目标提供商的原生协议格式。

**OpenAI → Claude 转换流程**：

```
OpenAI Request                    Claude Request
──────────────                    ──────────────
{                                 {
  "model": "gpt-4o",                "model": "claude-sonnet-4-20250514",
  "messages": [                     "messages": [
    {"role": "system",                {"role": "user", "content": "Hello"},
     "content": "You are..."},       {"role": "assistant", "content": "Hi!"}
    {"role": "user",                ],
     "content": "Hello"},           "system": "You are...",
    {"role": "assistant",           "max_tokens": 1000,
     "content": "Hi!"}              "stream": false
  ],                              }
  "max_tokens": 1000,
  "stream": false
}
```

关键转换点：
- OpenAI 的 `system` 角色消息提取为 Claude 的顶层 `system` 字段
- Claude 不直接支持 `system` 角色在 messages 数组中
- 认证头从 `Authorization: Bearer` 改为 `x-api-key` + `anthropic-version`

**OpenAI → Gemini 转换流程**：

```
OpenAI Request                    Gemini Request
──────────────                    ──────────────
{                                 {
  "messages": [                     "contents": [
    {"role": "user",                  {"role": "user", "parts": [{"text": "Hello"}]},
     "content": "Hello"},            {"role": "model", "parts": [{"text": "Hi!"}]}
    {"role": "assistant",           ],
     "content": "Hi!"}             "generationConfig": {
  ]                                   "maxOutputTokens": 1000
}                                   }
                                  }
```

关键转换点：
- `assistant` 角色映射为 `model`
- `system` 角色消息被忽略（Gemini 使用 system instruction 机制）
- content 嵌套在 `parts[].text` 中
- API Key 通过 URL 参数传递

#### 3.3.2 渠道管理器 (channel/manager.go)

**职责**：维护渠道缓存，实现负载均衡策略选择。

**支持的负载均衡策略**：

| 策略 | 实现方式 | 适用场景 |
|------|---------|---------|
| round_robin | 原子计数器轮询 | 渠道能力相近，均匀分配 |
| weighted | 权重随机选择 | 渠道配额/能力不同 |
| random | 完全随机 | 简单场景 |

**缓存策略**：
- 渠道数据在内存中维护，启动时从数据库加载
- 配置变更后通过 `Reload()` 刷新内存缓存
- 读写使用 `sync.RWMutex` 保护，读操作无锁竞争

#### 3.3.3 故障转移模块 (failover/failover.go)

**职责**：实现熔断器模式，自动隔离故障渠道。

**熔断器状态机**：

```
                    失败次数 >= threshold
    ┌─────────┐     ──────────────────►    ┌─────────┐
    │         │                             │         │
    │ CLOSED  │                             │  OPEN   │
    │         │◄────────────────────────────│         │
    └────┬────┘   半开状态请求成功           └────┬────┘
         │                                       │
         │ 半开状态请求失败                      │ resetTimeout 到期
         ▼                                       ▼
    ┌─────────┐                             ┌────────────┐
    │         │                             │            │
    │  OPEN   │────────────────────────────►│ HALF-OPEN  │
    │         │   (保持 open，更新 lastFail) │            │
    └─────────┘                             └────────────┘
```

- **CLOSED（闭合）**：正常状态，允许所有请求通过
- **OPEN（断开）**：失败次数达到阈值后打开，拒绝所有请求
- **HALF-OPEN（半开）**：超时后进入半开状态，允许一次探测请求

**恢复机制**：
- 半开状态下请求成功 → 回到 CLOSED，重置失败计数
- 半开状态下请求失败 → 回到 OPEN，重置超时计时

#### 3.3.4 模型路由器 (router/router.go)

**职责**：根据模型名称匹配路由规则，决定请求发往哪些渠道。

**匹配逻辑**：
1. 按优先级降序遍历路由规则
2. 使用 `filepath.Match` 进行 glob 模式匹配
3. 返回第一个匹配的规则的渠道列表和负载均衡策略
4. 无匹配时，根据模型名自动推断渠道类型

**模型类型推断规则**：

| 模型名前缀 | 推断类型 |
|-----------|---------|
| `claude` | ChannelClaude |
| `gem` | ChannelGemini |
| `dall-e-*` | ChannelOpenAIImage |
| 其他 | ChannelOpenAIChat |

### 3.4 数据库设计

**存储引擎**：SQLite（WAL 模式）

**选择理由**：
- 零配置，单文件部署
- WAL 模式支持并发读写
- 适合中小规模配置存储（渠道数 < 1000）

**表结构**：

```sql
channels          -- 渠道配置
model_routes      -- 模型路由规则
channel_stats     -- 渠道运行统计
request_logs      -- 请求日志
```

**索引策略**：
- `idx_channels_type`：按类型快速筛选渠道
- `idx_channels_enabled`：快速过滤已启用渠道
- `idx_routes_pattern`：加速路由模式匹配查询
- `idx_logs_created`：按时间范围查询日志

### 3.5 API 设计

#### 3.5.1 代理 API（默认端口 8080）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /v1/chat/completions | 聊天补全（兼容 OpenAI 格式） |
| POST | /v1/images/generations | 图片生成 |
| GET | /v1/models | 列出所有可用模型 |
| GET | / | 健康检查 |

#### 3.5.2 管理 API（默认端口 8081）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/channels | 列出所有渠道 |
| POST | /api/channels | 创建渠道 |
| GET | /api/channels/{id} | 获取渠道详情 |
| PUT | /api/channels/{id} | 更新渠道 |
| DELETE | /api/channels/{id} | 删除渠道 |
| GET | /api/routes | 列出路由规则 |
| POST | /api/routes | 创建路由规则 |
| GET | /api/stats | 查看渠道状态 |
| POST | /api/reload | 重载配置 |

### 3.6 请求处理流程

```
客户端请求
    │
    ▼
[1] 解析请求体，提取 model 字段
    │
    ▼
[2] 模型路由器 Resolve(model)
    │
    ├── 有匹配路由 → 使用路由指定的渠道 ID + 策略
    └── 无匹配路由 → InferChannelType(model) 推断类型，自动选择渠道
    │
    ▼
[3] 遍历候选渠道列表
    │
    ├── 渠道已禁用 → 跳过
    ├── 熔断器 OPEN → 跳过
    └── 渠道健康 → 执行转发
    │
    ▼
[4] 协议转换
    │
    ├── OpenAI → 直连（OpenAI/Codex）
    ├── OpenAI → Claude Messages API
    └── OpenAI → Gemini GenerateContent API
    │
    ▼
[5] 发送请求到目标 API
    │
    ├── 成功 → RecordSuccess，返回响应给客户端
    └── 失败 → RecordFailure，尝试下一个渠道
    │
    ▼
[6] 所有渠道失败 → 返回 503 Service Unavailable
```

## 4. 性能设计

### 4.1 连接池优化

```go
Transport: &http.Transport{
    MaxIdleConns:        1000,    // 最大空闲连接数
    MaxIdleConnsPerHost: 100,     // 每主机最大空闲连接
    IdleConnTimeout:     90 * time.Second,
}
```

### 4.2 并发安全

| 模块 | 保护机制 |
|------|---------|
| 渠道管理器 | sync.RWMutex，读写分离 |
| 熔断器 | sync.Mutex，状态变更串行化 |
| 轮询计数器 | sync/atomic.Int64，无锁原子操作 |
| 数据库 | SQLite WAL 模式，并发读写 |

### 4.3 超时控制

| 层级 | 超时值 | 说明 |
|------|--------|------|
| HTTP 客户端 | 300s | 整体请求超时 |
| 空闲连接 | 90s | 连接池空闲回收 |
| 渠道配置 | 可配置 | 每个渠道独立 timeout |

## 5. 部署配置

### 5.1 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| SERVER_PORT | 8080 | 代理服务器端口 |
| ADMIN_PORT | 8081 | 管理界面端口 |
| DATABASE_PATH | data/proxy.db | SQLite 数据库路径 |

### 5.2 构建与运行

```bash
# 构建
go build -o ai-proxy-gateway ./cmd/server

# 运行
./ai-proxy-gateway

# 自定义配置
SERVER_PORT=9000 ADMIN_PORT=9001 ./ai-proxy-gateway
```

## 6. 扩展方向

| 方向 | 说明 |
|------|------|
| 流式响应完整支持 | 当前 StreamResponse 为简单透传，需完善 SSE 事件解析 |
| 请求/响应日志持久化 | 当前 request_logs 表已就绪，需完善写入逻辑 |
| 配额管理 | 按渠道设置每日/每月 token 限额 |
| 鉴权中间件 | API Key 验证、IP 白名单、速率限制 |
| Prometheus 指标 | 导出请求延迟、成功率、渠道状态等指标 |
| 多实例部署 | 使用 Redis 作为共享配置存储，支持水平扩展 |
