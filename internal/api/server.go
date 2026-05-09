package api

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-proxy-gateway/internal/auth"
	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/failover"
	"ai-proxy-gateway/internal/i18n"
	"ai-proxy-gateway/internal/models"
	"ai-proxy-gateway/internal/router"
	"ai-proxy-gateway/internal/token"
)

// ProxyHandler 定义代理处理器接口
type ProxyHandler interface {
	ProxyRequest(w http.ResponseWriter, r *http.Request, model string, body []byte, chType models.ChannelType)
}

// Server 持有所有 HTTP 处理器
type Server struct {
	db           *db.Database
	channelMgr   *channel.Manager
	failover     *failover.FailoverManager
	modelRtr     *router.ModelRouter
	proxyHdl     ProxyHandler
	authMgr      *auth.Manager
	proxyAPIKey  string
	protoAdapter interface {
		HandleClaudeMessages(w http.ResponseWriter, r *http.Request)
		HandleResponsesAPI(w http.ResponseWriter, r *http.Request)
		HandleResponsesCompact(w http.ResponseWriter, r *http.Request)
		HandleGeminiGenerateContent(w http.ResponseWriter, r *http.Request)
		HandleGeminiStreamGenerateContent(w http.ResponseWriter, r *http.Request)
	}
	version   string
	buildTime string
	gitCommit string
}

// New 创建新的 API 服务器
func New(database *db.Database, chMgr *channel.Manager, fo *failover.FailoverManager, mr *router.ModelRouter, ph ProxyHandler, am *auth.Manager, proxyKey string, protoAdapter interface {
	HandleClaudeMessages(w http.ResponseWriter, r *http.Request)
	HandleResponsesAPI(w http.ResponseWriter, r *http.Request)
	HandleResponsesCompact(w http.ResponseWriter, r *http.Request)
	HandleGeminiGenerateContent(w http.ResponseWriter, r *http.Request)
	HandleGeminiStreamGenerateContent(w http.ResponseWriter, r *http.Request)
}, ver, buildTime, commit string) *Server {
	return &Server{
		db:           database,
		channelMgr:   chMgr,
		failover:     fo,
		modelRtr:     mr,
		proxyHdl:     ph,
		authMgr:      am,
		proxyAPIKey:  proxyKey,
		protoAdapter: protoAdapter,
		version:      ver,
		buildTime:    buildTime,
		gitCommit:    commit,
	}
}

// handleHealth 返回服务健康状态
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	channels := s.channelMgr.GetAllChannels()
	healthyCount := 0
	for _, ch := range channels {
		if s.failover.IsHealthy(ch.ID) {
			healthyCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "ok",
		"channels_total":   len(channels),
		"channels_healthy": healthyCount,
		"uptime_seconds":   time.Since(startTime).Seconds(),
	})
}

var startTime = time.Now()

// RegisterProxyRoutes 注册代理 API 路由（兼容 OpenAI 格式）
func (s *Server) RegisterProxyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/chat/completions", s.proxyMiddleware(s.handleChatCompletions))
	mux.HandleFunc("/v1/images/generations", s.proxyMiddleware(s.handleImageGeneration))
	mux.HandleFunc("/v1/models", s.proxyMiddleware(s.handleListModels))
	mux.HandleFunc("/v1/messages/count_tokens", s.proxyMiddleware(s.handleCountTokens))

	// Native protocol routes
	if s.protoAdapter != nil {
		mux.HandleFunc("/v1/messages", s.proxyMiddleware(s.protoAdapter.HandleClaudeMessages))
		mux.HandleFunc("/v1/responses", s.proxyMiddleware(s.protoAdapter.HandleResponsesAPI))
		mux.HandleFunc("/v1/responses/{response_id}/compact", s.proxyMiddleware(s.protoAdapter.HandleResponsesCompact))
		mux.HandleFunc("/v1beta/models/{model}:generateContent", s.proxyMiddleware(s.protoAdapter.HandleGeminiGenerateContent))
		mux.HandleFunc("/v1beta/models/{model}:streamGenerateContent", s.proxyMiddleware(s.protoAdapter.HandleGeminiStreamGenerateContent))
	}

	mux.HandleFunc("/", s.proxyMiddleware(s.handleFallback))
}

// proxyMiddleware 代理 API 鉴权中间件
func (s *Server) proxyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.proxyAPIKey == "" {
			next(w, r)
			return
		}

		key := extractAPIKey(r)
		if key == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "unauthorized",
				"message": i18n.T(getLang(r), "login_required"),
			})
			return
		}

		if subtle.ConstantTimeCompare([]byte(key), []byte(s.proxyAPIKey)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "forbidden",
				"message": i18n.T(getLang(r), "forbidden"),
			})
			return
		}

		next(w, r)
	}
}

// extractAPIKey 从请求中提取 API key
func extractAPIKey(r *http.Request) string {
	// X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	// Authorization: Bearer <key>
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// url query param (for SSE streams where headers can't be set)
	if key := r.URL.Query().Get("api_key"); key != "" {
		return key
	}

	return ""
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

// handleCountTokens 统一 token 计数接口
func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed, use POST",
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Try to detect the format and count tokens
	var countResp token.CountResponse

	// First try OpenAI format
	if result, err := token.CountTokensForOpenAI(body); err == nil {
		countResp = result
	} else {
		// Try Claude format
		if result, err := token.CountTokensForClaude(body); err == nil {
			countResp = result
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid request format, expected OpenAI or Claude message format",
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object":          "token_count",
		"model":           countResp.Model,
		"prompt_tokens":   countResp.PromptTokens,
		"total_tokens":    countResp.TotalTokens,
		"completion_tokens": countResp.CompletionTokens,
		"note":            "Token counts are estimates based on character heuristics",
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
	// 公开端点（无需认证）
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)

	// 需要认证的端点
	mux.HandleFunc("/api/auth/me", s.authMiddleware(s.handleAuthMe))
	mux.HandleFunc("/api/auth/password", s.authMiddleware(s.handleChangePassword))
	mux.HandleFunc("/api/channels", s.authMiddleware(s.handleChannels))
	mux.HandleFunc("/api/channels/", s.authMiddleware(s.handleChannelByID))
	mux.HandleFunc("/api/routes", s.authMiddleware(s.handleRoutes))
	mux.HandleFunc("/api/routes/", s.authMiddleware(s.handleRouteByID))
	mux.HandleFunc("/api/stats", s.authMiddleware(s.handleStats))
	mux.HandleFunc("/api/stats/traffic", s.authMiddleware(s.handleTrafficStats))
	mux.HandleFunc("/api/stats/protocol", s.authMiddleware(s.handleStatsByProtocol))
	mux.HandleFunc("/api/stats/models", s.authMiddleware(s.handleModelStats))
	mux.HandleFunc("/api/logs", s.authMiddleware(s.handleLogs))
	mux.HandleFunc("/api/reload", s.authMiddleware(s.handleReload))
	mux.HandleFunc("/api/channels/{id}/ping", s.authMiddleware(s.handlePingChannel))
	mux.HandleFunc("/api/channels/{id}/resume", s.authMiddleware(s.handleResumeChannel))
	mux.HandleFunc("/api/channels/{id}/keys", s.authMiddleware(s.handleChannelKeys))
	mux.HandleFunc("/api/channels/{id}/keys/{key_id}", s.authMiddleware(s.handleChannelKeyByID))
	mux.HandleFunc("/api/channels/{id}/test", s.authMiddleware(s.handleTestChannel))
	mux.HandleFunc("/api/channels/{id}/details", s.authMiddleware(s.handleChannelDetails))
	mux.HandleFunc("/api/channels/priorities", s.authMiddleware(s.handleBatchPriorities))
	mux.HandleFunc("/api/version", s.authMiddleware(s.handleVersion))
}

// getLang 从请求中获取语言偏好
func getLang(r *http.Request) string {
	// 优先从 Accept-Language header 获取
	lang := r.Header.Get("Accept-Language")
	if lang != "" {
		if strings.HasPrefix(lang, "zh") {
			return "zh"
		}
	}
	return "en"
}

// authMiddleware 管理端认证中间件
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lang := getLang(r)
		cookie, err := r.Cookie("admin_session")
		if err != nil {
			// 尝试从 Header 获取 token（用于 API 调用）
			token := r.Header.Get("X-Admin-Token")
			if token == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": i18n.T(lang, "login_required"),
				})
				return
			}
			sess, err := s.authMgr.Validate(token)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": i18n.T(lang, "invalid_credentials"),
				})
				return
			}
			r.Header.Set("X-Admin-User", sess.Username)
			next(w, r)
			return
		}

		sess, err := s.authMgr.Validate(cookie.Value)
		if err != nil {
			// 清除过期 cookie
			http.SetCookie(w, &http.Cookie{
				Name:     "admin_session",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "unauthorized",
				"message": i18n.T(lang, "unauthorized"),
			})
			return
		}

		r.Header.Set("X-Admin-User", sess.Username)
		next(w, r)
	}
}

// handleLogin 管理员登录
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, i18n.T(getLang(r), "method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": i18n.T(getLang(r), "bad_request")})
		return
	}

	if req.Username == "" || req.Password == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": i18n.T(getLang(r), "invalid_credentials")})
		return
	}

	token, err := s.authMgr.Login(req.Username, req.Password)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": i18n.T(getLang(r), "invalid_credentials")})
		return
	}

	// 设置 httpOnly cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"username": req.Username,
	})
}

// handleLogout 管理员登出
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("admin_session")
	if err == nil && cookie.Value != "" {
		s.authMgr.Logout(cookie.Value)
	}

	// 清除 cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAuthMe 返回当前登录用户信息
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"username": r.Header.Get("X-Admin-User"),
	})
}

// handleChangePassword 修改管理员密码
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	username := r.Header.Get("X-Admin-User")
	if err := s.authMgr.ChangePassword(username, req.OldPassword, req.NewPassword); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

// handleBatchPriorities 批量更新渠道优先级（拖拽排序）
func (s *Server) handleBatchPriorities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var updates []struct {
		ID       int64 `json:"id"`
		Priority int   `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pairs := make([]struct{ ID int64; Priority int }, len(updates))
	for i, u := range updates {
		pairs[i] = struct{ ID int64; Priority int }{ID: u.ID, Priority: u.Priority}
	}

	if err := s.db.BatchUpdatePriorities(pairs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.channelMgr.Reload()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleChannelDetails 获取渠道详情（配置、统计、日志）
func (s *Server) handleChannelDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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

	// Get channel stats
	var stats map[string]interface{}
	statsRow, err := s.db.GetChannelStats(id)
	if err == nil && len(statsRow) > 0 {
		stats = statsRow[0]
	}

	// Get recent logs for this channel
	logs, _ := s.db.GetRequestLogs(50, 0, id, 0, "")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"channel": ch,
		"stats":   stats,
		"logs":    logs,
	})
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

// handleTrafficStats 返回流量统计数据
func (s *Server) handleTrafficStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.ListAllChannelStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(stats)
}

// handleStatsByProtocol 返回按协议类型隔离的统计数据
func (s *Server) handleStatsByProtocol(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetStatsByProtocol()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if stats == nil {
		stats = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(stats)
}

// handleModelStats 返回按模型维度的历史统计
func (s *Server) handleModelStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetModelStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if stats == nil {
		stats = []map[string]interface{}{}
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

	logs, err := s.db.GetRequestLogs(limit, offset, 0, 0, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(logs)
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
		"success": resp.StatusCode < 400,
		"latency": latency,
		"status":  resp.StatusCode,
		"error":   "",
	})
}

// handleResumeChannel 恢复被禁用的渠道
func (s *Server) handleResumeChannel(w http.ResponseWriter, r *http.Request) {
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
	ch.Enabled = true
	if err := s.db.UpdateChannel(ch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.failover.ResetState(ch.ID)
	s.channelMgr.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "resumed"})
}

// handleChannelKeys 处理渠道 Key 的列表和创建
func (s *Server) handleChannelKeys(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	channelID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid channel id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		keys, err := s.db.ListChannelKeys(channelID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if keys == nil {
			keys = []models.ChannelKey{}
		}
		json.NewEncoder(w).Encode(keys)

	case http.MethodPost:
		var ck models.ChannelKey
		if err := json.NewDecoder(r.Body).Decode(&ck); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ck.ChannelID = channelID
		keyID, err := s.db.CreateChannelKey(&ck)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ck.ID = keyID
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ck)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleChannelKeyByID 处理单个 Key 的更新和删除
func (s *Server) handleChannelKeyByID(w http.ResponseWriter, r *http.Request) {
	keyIDStr := r.PathValue("key_id")
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		ck, err := s.db.GetChannelKey(keyID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(ck)

	case http.MethodPut:
		var req struct {
			Priority *int    `json:"priority"`
			Status   *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Priority != nil {
			if err := s.db.UpdateChannelKeyPriority(keyID, *req.Priority); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.Status != nil {
			if err := s.db.UpdateChannelKeyStatus(keyID, *req.Status); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		ck, _ := s.db.GetChannelKey(keyID)
		json.NewEncoder(w).Encode(ck)

	case http.MethodDelete:
		if err := s.db.DeleteChannelKey(keyID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// TestResult 表示单个模型的测试结果
type TestResult struct {
	Model   string `json:"model"`
	Success bool   `json:"success"`
	Latency int64  `json:"latency_ms"`
	Error   string `json:"error,omitempty"`
}

// handleTestChannel 对渠道进行能力测试
func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
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

	// Parse optional models to test from request body
	var req struct {
		Models []string `json:"models"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	testModels := req.Models
	if len(testModels) == 0 {
		testModels = []string{ch.Model}
	}

	results := make([]TestResult, 0, len(testModels))
	for _, model := range testModels {
		start := time.Now()
		client := &http.Client{Timeout: 10 * time.Second}
		testURL := ch.BaseURL + "/v1/models"
		testReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, testURL, nil)
		if err != nil {
			results = append(results, TestResult{Model: model, Success: false, Error: err.Error()})
			continue
		}

		// Use channel key or first active key
		apiKey := ch.APIKey
		if activeKey, err := s.db.GetActiveKeyForChannel(id); err == nil && activeKey != nil {
			apiKey = activeKey.APIKey
		}

		testReq.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := client.Do(testReq)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			results = append(results, TestResult{Model: model, Success: false, Latency: latency, Error: err.Error()})
			continue
		}
		resp.Body.Close()
		results = append(results, TestResult{
			Model:   model,
			Success: resp.StatusCode < 400,
			Latency: latency,
			Error:   "",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"channel_id":   id,
		"channel_name": ch.Name,
		"results":      results,
	})
}

// handleVersion 返回版本信息
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"version":    s.version,
		"build_time": s.buildTime,
		"commit":     s.gitCommit,
	})
}
