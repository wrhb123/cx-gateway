package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/failover"
	"ai-proxy-gateway/internal/models"
	"ai-proxy-gateway/internal/router"
)

// ProxyHandler 定义代理处理器接口
type ProxyHandler interface {
	ProxyRequest(w http.ResponseWriter, r *http.Request, model string, body []byte, chType models.ChannelType)
}

// Server 持有所有 HTTP 处理器
type Server struct {
	db         *db.Database
	channelMgr *channel.Manager
	failover   *failover.FailoverManager
	modelRtr   *router.ModelRouter
	proxyHdl   ProxyHandler
}

// New 创建新的 API 服务器
func New(database *db.Database, chMgr *channel.Manager, fo *failover.FailoverManager, mr *router.ModelRouter, ph ProxyHandler) *Server {
	return &Server{
		db:         database,
		channelMgr: chMgr,
		failover:   fo,
		modelRtr:   mr,
		proxyHdl:   ph,
	}
}

// RegisterProxyRoutes 注册代理 API 路由（兼容 OpenAI 格式）
func (s *Server) RegisterProxyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/images/generations", s.handleImageGeneration)
	mux.HandleFunc("/v1/models", s.handleListModels)
	mux.HandleFunc("/", s.handleFallback)
}

// handleChatCompletions 处理聊天补全请求
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	chType := router.InferChannelType(req.Model)
	s.proxyHdl.ProxyRequest(w, r, req.Model, body, chType)
}

// handleImageGeneration 处理图片生成请求
func (s *Server) handleImageGeneration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	s.proxyHdl.ProxyRequest(w, r, "dall-e-3", body, models.ChannelOpenAIImage)
}

// handleListModels 列出所有可用模型
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	channels := s.channelMgr.GetAllChannels()
	models := make([]map[string]interface{}, 0)
	for _, ch := range channels {
		models = append(models, map[string]interface{}{
			"id":       ch.Model,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": string(ch.Type),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

// handleFallback 处理默认回退请求
func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "ai-proxy-gateway",
	})
}

// RegisterAdminRoutes 注册管理 API 路由
func (s *Server) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/channels", s.handleChannels)
	mux.HandleFunc("/api/channels/", s.handleChannelByID)
	mux.HandleFunc("/api/routes", s.handleRoutes)
	mux.HandleFunc("/api/routes/", s.handleRouteByID)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/stats/traffic", s.handleTrafficStats)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/reload", s.handleReload)
	mux.HandleFunc("/api/channels/{id}/ping", s.handlePingChannel)
}

// handleChannels 处理渠道列表和创建请求
func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		channels, err := s.db.ListChannels()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if channels == nil {
			channels = []models.Channel{}
		}
		json.NewEncoder(w).Encode(channels)

	case http.MethodPost:
		var ch models.Channel
		if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if ch.MaxRetries == 0 {
			ch.MaxRetries = 3
		}
		if ch.Timeout == 0 {
			ch.Timeout = 120
		}
		if ch.Weight == 0 {
			ch.Weight = 1
		}
		id, err := s.db.CreateChannel(&ch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ch.ID = id
		s.channelMgr.Reload()
		s.db.InitChannelStats(id)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ch)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleChannelByID 处理单个渠道的查询、更新和删除
func (s *Server) handleChannelByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/channels/")
	if idStr == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		ch, err := s.db.GetChannel(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(ch)

	case http.MethodPut:
		var ch models.Channel
		if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ch.ID = id
		if err := s.db.UpdateChannel(&ch); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.channelMgr.Reload()
		json.NewEncoder(w).Encode(ch)

	case http.MethodDelete:
		if err := s.db.DeleteChannel(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.channelMgr.Reload()
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRoutes 处理路由规则的查询和创建
func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		routes, err := s.db.ListRoutes()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if routes == nil {
			routes = []models.ModelRoute{}
		}
		json.NewEncoder(w).Encode(routes)

	case http.MethodPost:
		var route models.ModelRoute
		if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, err := s.db.CreateRoute(&route)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		route.ID = id
		s.modelRtr.Reload()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(route)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRouteByID 处理单个路由规则的查询、更新和删除
func (s *Server) handleRouteByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/routes/")
	if idStr == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if err := s.db.DeleteRoute(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.modelRtr.Reload()
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPut:
		var route models.ModelRoute
		if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(route.ChannelIDs) == 0 {
			http.Error(w, "channel_ids required", http.StatusBadRequest)
			return
		}
		route.ID = id
		if err := s.db.UpdateRoute(&route); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.modelRtr.Reload()
		json.NewEncoder(w).Encode(route)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStats 返回所有渠道的状态信息
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	channels, _ := s.db.ListChannels()
	stats := make([]map[string]interface{}, 0)
	for _, ch := range channels {
		stats = append(stats, map[string]interface{}{
			"channel_id":   ch.ID,
			"channel_name": ch.Name,
			"type":         string(ch.Type),
			"state":        s.failover.GetState(ch.ID),
			"enabled":      ch.Enabled,
		})
	}
	if stats == nil {
		stats = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(stats)
}

// handleReload 重新加载渠道和路由配置
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.channelMgr.Reload()
	s.modelRtr.Reload()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "reloaded"})
}

// handleTrafficStats 返回流量统计数据
func (s *Server) handleTrafficStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.ListAllChannelStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(stats)
}

// handleLogs 返回请求日志
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	recent := r.URL.Query().Get("recent")
	if recent == "1" {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		if hours <= 0 {
			hours = 24
		}
		data, err := s.db.GetRecentLogs(hours)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(data)
		return
	}

	logs, err := s.db.GetRequestLogs(limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(logs)
}

// handlePingChannel 测试渠道连通性
func (s *Server) handlePingChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	ch, err := s.db.GetChannel(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	start := time.Now()
	client := &http.Client{Timeout: 10 * time.Second}
	testURL := ch.BaseURL + "/v1/models"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, testURL, nil)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"latency": 0,
			"error":   err.Error(),
		})
		return
	}
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)

	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"latency": latency,
			"error":   err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  resp.StatusCode < 400,
		"latency":  latency,
		"status":   resp.StatusCode,
		"error":    "",
	})
}
