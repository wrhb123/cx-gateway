package models

// ChannelType 表示 AI 提供商渠道类型
type ChannelType string

const (
	ChannelClaude      ChannelType = "claude"
	ChannelOpenAIChat  ChannelType = "openai_chat"
	ChannelOpenAIImage ChannelType = "openai_image"
	ChannelCodex       ChannelType = "codex"
	ChannelGemini      ChannelType = "gemini"
)

// Channel 表示一个 API 渠道/提供商配置
type Channel struct {
	ID         int64       `json:"id"`
	Name       string      `json:"name"`
	Type       ChannelType `json:"type"`
	BaseURL    string      `json:"base_url"`
	APIKey     string      `json:"api_key"`
	Model      string      `json:"model"`
	Priority   int         `json:"priority"`
	Weight     int         `json:"weight"`
	Enabled    bool        `json:"enabled"`
	MaxRetries int         `json:"max_retries"`
	Timeout    int         `json:"timeout"` // 秒
	CreatedAt  string      `json:"created_at"`
	UpdatedAt  string      `json:"updated_at"`
}

// ChannelStats 跟踪渠道健康状态和使用情况
type ChannelStats struct {
	ChannelID     int64  `json:"channel_id"`
	TotalRequests int64  `json:"total_requests"`
	SuccessCount  int64  `json:"success_count"`
	FailureCount  int64  `json:"failure_count"`
	AvgLatency    int64  `json:"avg_latency_ms"`
	IsHealthy     bool   `json:"is_healthy"`
	LastError     string `json:"last_error"`
	LastChecked   string `json:"last_checked"`
}

// ModelRoute 定义模型名称的路由规则
type ModelRoute struct {
	ID          int64   `json:"id"`
	Pattern     string  `json:"pattern"` // glob 模式，如 "gpt-*", "claude-*"
	ChannelIDs  []int64 `json:"channel_ids"`
	LoadBalance string  `json:"load_balance"` // "round_robin", "weighted", "random"
	Priority    int     `json:"priority"`
	Enabled     bool    `json:"enabled"`
}

// RequestLog 存储代理请求日志
type RequestLog struct {
	ID          int64  `json:"id"`
	RequestID   string `json:"request_id"`
	Model       string `json:"model"`
	ChannelID   int64  `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Status      int    `json:"status"`
	Latency     int64  `json:"latency_ms"`
	CreatedAt   string `json:"created_at"`
}

// Config 表示应用程序配置
type Config struct {
	ServerPort     int    `json:"server_port"`
	AdminPort      int    `json:"admin_port"`
	AdminUsername  string `json:"admin_username"`
	AdminPassword  string `json:"admin_password"`
	ProxyAPIKey    string `json:"proxy_api_key"`
	DatabasePath   string `json:"database_path"`
	LogLevel       string `json:"log_level"`
	MaxConcurrent  int    `json:"max_concurrent"`
	RequestTimeout int    `json:"request_timeout"` // 秒
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		ServerPort:     8080,
		AdminPort:      8081,
		AdminUsername:  "admin",
		AdminPassword:  "admin",
		DatabasePath:   "data/proxy.db",
		LogLevel:       "info",
		MaxConcurrent:  1000,
		RequestTimeout: 300,
	}
}
