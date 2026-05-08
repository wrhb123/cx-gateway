# TODO: Missing Capabilities (Gap Analysis vs CCX)

Generated: 2026-05-07
Reference: [BenedictKing/ccx](https://github.com/BenedictKing/ccx)

## 1. Proxy Protocol Expansion

### 1.1 Claude Messages API
- [x] `POST /v1/messages` — Anthropic Claude Messages 协议代理
- [x] 支持 `system` prompt、`tools`、`tool_use` 等完整特性（透传模式已支持）
- [x] 支持 SSE stream (`stream: true`)（透传模式）

### 1.2 Codex Responses API
- [x] `POST /v1/responses` — OpenAI Responses API 代理
- [x] `POST /v1/responses/{response_id}/compact` — 会话压缩

### 1.3 Gemini 原生协议
- [x] `POST /v1beta/models/{model}:generateContent` — Gemini 协议代理
- [x] `POST /v1beta/models/{model}:streamGenerateContent` — Gemini 流式协议

### 1.4 Token 计数端点
- [ ] `POST /v1/messages/count_tokens` — 统一 token 计数接口
- [ ] 内部路由到各提供商的 token 计算

### 1.5 Responses 会话跟踪
- [ ] 服务端维护 `response_id` 到会话历史的映射
- [ ] 支持多轮对话的上下文持久化

## 2. Channel Key Management (Multi-Key Rotation)

### 2.1 Data Model
- [x] 新增 `channel_keys` 表（id, channel_id, api_key, status, priority, usage_count, last_used, created_at）
- [x] 渠道与 Key 一对多关系
- [x] 现有 `channels.api_key` 作为 fallback 保留

### 2.2 API Endpoints
- [x] `POST /api/channels/{id}/keys` — 添加 Key
- [x] `GET /api/channels/{id}/keys` — 列出 Key 列表
- [x] `DELETE /api/channels/{id}/keys/{key_id}` — 删除 Key
- [x] `PUT /api/channels/{id}/keys/{key_id}` — 更新优先级/状态
- [x] `PUT /api/channels/{id}/keys/{key_id}/restore` — 通过 PUT 设置 status='active' 恢复

### 2.3 Channel Manager Update
- [x] 代理请求时优先使用 active 的 channel_keys（按优先级排序取第一个）
- [x] 使用 Key 时自动增加 usage_count 和更新 last_used
- [x] 无 channel_keys 时 fallback 到 channel.api_key

## 3. Channel Advanced Features

### 3.1 Per-Channel Proxy Support
- [x] Channel 模型新增 `proxy_url`、`proxy_type`（http/socks5）字段
- [x] HTTP 请求通过渠道配置的代理发送（Transport 层实现）

### 3.2 Custom Request Headers
- [x] Channel 模型新增 `custom_headers` 字段（JSON）
- [x] 代理请求时附加自定义 headers

### 3.3 Model Allowlist/Whitelist
- [x] Channel 模型新增 `supported_models` 字段（JSON 数组，支持通配符如 `gpt-*`）
- [x] 请求路由时校验模型是否在渠道白名单内
- [x] 白名单为空表示不限制

### 3.4 Custom Route Prefix
- [x] Route 模型新增 `route_prefix` 字段
- [x] 支持 `:routePrefix/v1/chat/completions` 等自定义前缀路由
- [x] 默认使用 `v1` 作为前缀

### 3.5 Drag-and-Drop Priority
- [ ] 前端支持拖拽调整渠道优先级
- [ ] 后端持久化 priority 排序

### 3.6 Promotion Window
- [x] Channel 模型新增 `promotion_start`、`promotion_end` 字段
- [x] 在促销期内自动提升渠道权重/优先级

### 3.7 Channel Resume
- [x] `POST /api/channels/{id}/resume` — 恢复被禁用的渠道
- [x] 渠道从 failover 状态中恢复并重新加入调度

## 4. Capability Testing

### 4.1 Per-Model Testing
- [x] `POST /api/channels/{id}/test` — 对渠道支持的模型逐一测试
- [x] 返回每个模型的可用性、延迟、响应体摘要
- [x] 前端弹窗展示测试进度和结果

### 4.2 Channel Details Modal
- [ ] 前端渠道详情弹窗（配置、统计、日志）
- [ ] 渠道日志查看（按渠道过滤）

## 5. Observability Depth

### 5.1 Metrics Isolation by Protocol
- [ ] 指标按协议类型（openai/claude/gemini）隔离统计
- [ ] 前端支持按协议切换指标视图

### 5.2 Key-Level Metrics
- [x] Key 级别的请求数、成功率、延迟统计
- [x] `channel_keys` 表关联指标

### 5.3 Model-Level Historical Stats
- [ ] 按模型维度的历史统计
- [ ] 新增 `model_stats` 表或扩展 `channel_stats`

### 5.4 Request Log Enhancement
- [x] 日志新增 `source`（客户端 IP）、`interface`（代理/管理）、`key_mask`（脱敏 key 前缀）字段
- [x] 日志支持按渠道、模型、状态码过滤

## 6. Deployment & Infrastructure

### 6.1 Health Check Endpoint
- [x] `GET /health` — 返回服务状态（数据库连接、渠道数量、运行时间）
- [x] 返回 JSON：`{ status: "ok", uptime: "1h2m3s", db: "connected", channels: 5 }`

### 6.2 Version Management
- [x] 编译时注入版本号（`-ldflags "-X main.version=..."`）
- [x] `/api/version` 返回版本信息
- [x] 前端展示当前版本

### 6.3 Single-Port Deployment
- [x] 前端嵌入 + 代理 API 共用同一端口
- [x] 通过环境变量 `SINGLE_PORT=true` 切换
- [x] 管理后台路由 `/admin/` 与代理路由 `/v1/` 共存

### 6.4 Docker Support
- [x] `Dockerfile` 多阶段构建
- [x] `docker-compose.yml`（含 SQLite 持久化）

### 6.5 i18n Internationalization
- [x] 前端多语言支持（中/英）
- [ ] 后端错误消息国际化

## Priority Order

| 优先级 | 模块 | 理由 |
|--------|------|------|
| P0 | 6.1 Health Check | 快速收益，监控必需 |
| P0 | 2 Channel Key Management | 核心差异化功能，解决单点故障 |
| P1 | 3.3 Model Allowlist | 防止无效模型请求打到错误渠道 |
| P1 | 4 Capability Testing | 提升运维效率，快速定位问题渠道 |
| P2 | 3.1 Per-Channel Proxy | 企业内网场景需求 |
| P2 | 5.2 Key-Level Metrics | 多 Key 场景下的可观测性 |
| P2 | 6.2 Version Management | 版本追踪 |
| P3 | 1 Protocol Expansion | 协议覆盖，工作量大 |
| P3 | 3.6 Promotion Window | 高级调度策略 |
| P3 | 6.3 Single-Port | 部署优化 |
| P3 | 6.4 Docker | 部署标准化 |
| P3 | 6.5 i18n | 国际化 |
