package router

import (
	"path/filepath"
	"strings"
	"sync"

	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/models"
)

// ModelRouter 处理模型名称到渠道的路由
type ModelRouter struct {
	db     *db.Database
	mu     sync.RWMutex
	routes []models.ModelRoute
}

// New 创建新的模型路由器
func New(database *db.Database) *ModelRouter {
	mr := &ModelRouter{db: database}
	mr.Reload()
	return mr
}

// Reload 从数据库重新加载路由配置
func (mr *ModelRouter) Reload() error {
	routes, err := mr.db.ListRoutes()
	if err != nil {
		return err
	}

	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.routes = routes
	return nil
}

// Resolve 根据模型名称返回应处理的渠道 ID 列表、负载均衡策略和路由前缀
func (mr *ModelRouter) Resolve(modelName string) ([]int64, string, string) {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	for _, route := range mr.routes {
		if match(route.Pattern, modelName) {
			return route.ChannelIDs, route.LoadBalance, route.RoutePrefix
		}
	}

	return nil, "", ""
}

// match 检查模型名称是否匹配模式（支持 glob）
func match(pattern, name string) bool {
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// InferChannelType 根据模型名称推断渠道类型
func InferChannelType(modelName string) models.ChannelType {
	switch {
	case len(modelName) >= 6 && modelName[:6] == "claude":
		return models.ChannelClaude
	case len(modelName) >= 3 && modelName[:3] == "gem":
		return models.ChannelGemini
	case strings.HasPrefix(modelName, "dall-e-"):
		return models.ChannelOpenAIImage
	default:
		return models.ChannelOpenAIChat
	}
}
