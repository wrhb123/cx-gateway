# 用户指令记忆

本文件记录了用户的指令、偏好和教导，用于在未来的交互中提供参考。

## 格式

### 用户指令条目
用户指令条目应遵循以下格式：

[用户指令摘要]
- Date: [YYYY-MM-DD]
- Context: [提及的场景或时间]
- Instructions:
  - [用户教导或指示的内容，逐行描述]

### 项目知识条目
Agent 在任务执行过程中发现的条目应遵循以下格式：

[项目知识摘要]
- Date: [YYYY-MM-DD]
- Context: Agent 在执行 [具体任务描述] 时发现
- Category: [代码结构|代码模式|代码生成|构建方法|测试方法|依赖关系|环境配置]
- Instructions:
  - [具体的知识点，逐行描述]

## 去重策略
- 添加新条目前，检查是否存在相似或相同的指令
- 若发现重复，跳过新条目或与已有条目合并
- 合并时，更新上下文或日期信息
- 这有助于避免冗余条目，保持记忆文件整洁

## 条目

### AI Proxy Gateway 项目架构
- Date: 2026-05-07
- Context: Agent 在创建 AI API 代理网关项目时发现
- Category: 代码结构
- Instructions:
  - 项目使用 Go 语言开发，采用标准的 internal 包组织模式
  - cmd/server/main.go 为程序入口，使用 embed 嵌入 Web 管理界面
  - internal/proxy/handler.go 负责协议转换，将 OpenAI 格式转为 Claude/Gemini 原生格式
  - internal/channel/manager.go 实现负载均衡（轮询、加权、随机）
  - internal/failover/failover.go 实现熔断器模式进行故障转移
  - internal/router/router.go 使用 glob 模式匹配进行模型路由
  - internal/db/db.go 使用 SQLite WAL 模式存储配置
  - 构建命令: go build -o ai-proxy-gateway ./cmd/server
  - 环境变量: SERVER_PORT (默认 8080), ADMIN_PORT (默认 8081), DATABASE_PATH (默认 data/proxy.db)
  - 依赖: github.com/mattn/go-sqlite3 (需要 CGO)
