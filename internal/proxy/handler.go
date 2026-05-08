package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/failover"
	"ai-proxy-gateway/internal/models"
	"ai-proxy-gateway/internal/router"
	"ai-proxy-gateway/internal/token"
)

// Handler 是核心的代理处理器
type Handler struct {
	channelMgr *channel.Manager
	failover   *failover.FailoverManager
	modelRtr   *router.ModelRouter
	db         *db.Database
	client     *http.Client

	// per-channel proxy clients, keyed by proxy_url+proxy_type
	proxyClientsMu sync.Mutex
	proxyClients   map[string]*http.Client
}

// New 创建新的代理处理器
func New(chMgr *channel.Manager, fo *failover.FailoverManager, mr *router.ModelRouter, database *db.Database) *Handler {
	return &Handler{
		channelMgr: chMgr,
		failover:   fo,
		modelRtr:   mr,
		db:         database,
		client: &http.Client{
			Timeout: 300 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		proxyClients: make(map[string]*http.Client),
	}
}

// getClient returns the appropriate http.Client for a channel.
// If the channel has a proxy configured, it returns a cached client using that proxy.
func (h *Handler) getClient(ch *models.Channel) *http.Client {
	if ch.ProxyURL == "" {
		return h.client
	}

	cacheKey := ch.ProxyType + "://" + ch.ProxyURL
	h.proxyClientsMu.Lock()
	if c, ok := h.proxyClients[cacheKey]; ok {
		h.proxyClientsMu.Unlock()
		return c
	}
	h.proxyClientsMu.Unlock()

	proxyURL, err := url.Parse(ch.ProxyURL)
	if err != nil {
		return h.client
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	c := &http.Client{
		Timeout:   300 * time.Second,
		Transport: transport,
	}

	h.proxyClientsMu.Lock()
	h.proxyClients[cacheKey] = c
	h.proxyClientsMu.Unlock()
	return c
}

// ProxyRequest 处理传入的 API 请求
func (h *Handler) ProxyRequest(w http.ResponseWriter, r *http.Request, model string, body []byte, chType models.ChannelType) {
	var channelIDs []int64
	strategy := "round_robin"
	routePrefix := ""

	routeIDs, routeStrategy, prefix := h.modelRtr.Resolve(model)
	if len(routeIDs) > 0 {
		channelIDs = routeIDs
		if routeStrategy != "" {
			strategy = routeStrategy
		}
		routePrefix = prefix
	}

	if len(channelIDs) == 0 {
		ch, err := h.channelMgr.SelectChannel(chType, strategy)
		if err != nil {
			http.Error(w, `{"error":"no available channel"}`, http.StatusServiceUnavailable)
			return
		}
		channelIDs = []int64{ch.ID}
	}

	clientIP := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		clientIP = xff
	}

	var lastErr error
	for _, chID := range channelIDs {
		ch, ok := h.channelMgr.GetChannel(chID)
		if !ok || !ch.Enabled {
			continue
		}

		if !h.failover.IsHealthy(chID) {
			continue
		}

		start := time.Now()
		resp, err := h.forwardRequest(ch, model, body, r, routePrefix)
		latency := time.Since(start).Milliseconds()

		// Record request log
		log := &models.RequestLog{
			RequestID:   r.Header.Get("X-Request-ID"),
			Model:       model,
			ChannelID:   chID,
			ChannelName: ch.Name,
			Status:      resp.StatusCode,
			Latency:     latency,
			Source:      clientIP,
			Interface:   "proxy",
		}
		// Record key mask if available
		if activeKey, kerr := h.db.GetActiveKeyForChannel(chID); kerr == nil && activeKey != nil {
			if len(activeKey.APIKey) > 8 {
				log.KeyMask = activeKey.APIKey[:8] + "..."
			}
			h.db.IncrementKeyUsage(activeKey.ID, latency)
		} else if len(ch.APIKey) > 8 {
			log.KeyMask = ch.APIKey[:8] + "..."
		}
		h.db.LogRequest(log)

		if err != nil {
			h.failover.RecordFailure(chID)
			lastErr = err
			continue
		}

		h.failover.RecordSuccess(chID)

		// Read response body and extract token usage
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err == nil {
			tokens := extractTokenUsage(respBody, model, ch.Type)
			log.PromptTokens = tokens.PromptTokens
			log.CompletionTokens = tokens.CompletionTokens
			log.TotalTokens = tokens.TotalTokens
		}

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	if lastErr != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, lastErr.Error()), http.StatusBadGateway)
		return
	}
	http.Error(w, `{"error":"all channels failed"}`, http.StatusServiceUnavailable)
}

// forwardRequest 转发请求到目标渠道
func (h *Handler) forwardRequest(ch *models.Channel, model string, body []byte, r *http.Request, routePrefix string) (*http.Response, error) {
	// Check model allowlist
	if ch.SupportedModels != "" {
		var allowed []string
		if err := json.Unmarshal([]byte(ch.SupportedModels), &allowed); err == nil && len(allowed) > 0 {
			if !matchAny(allowed, model) {
				return nil, fmt.Errorf("model %s not allowed on channel %s", model, ch.Name)
			}
		}
	}

	var reqBody []byte
	var targetURL string
	var err error

	prefix := routePrefix
	if prefix == "" {
		prefix = "v1"
	}

	switch ch.Type {
	case models.ChannelClaude:
		reqBody, targetURL, err = h.translateToClaude(model, body, ch)
	case models.ChannelGemini:
		reqBody, targetURL, err = h.translateToGemini(model, body, ch)
	case models.ChannelOpenAIChat, models.ChannelCodex:
		reqBody = body
		targetURL = fmt.Sprintf("%s/%s/chat/completions", ch.BaseURL, prefix)
	case models.ChannelOpenAIImage:
		reqBody = body
		targetURL = fmt.Sprintf("%s/%s/images/generations", ch.BaseURL, prefix)
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", ch.Type)
	}

	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Get API key: try active channel_keys first, fall back to channel.APIKey
	apiKey := ch.APIKey
	if activeKey, err := h.db.GetActiveKeyForChannel(ch.ID); err == nil && activeKey != nil {
		apiKey = activeKey.APIKey
	}

	switch ch.Type {
	case models.ChannelClaude:
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case models.ChannelGemini:
		targetURL = targetURL + "?key=" + apiKey
	case models.ChannelOpenAIChat, models.ChannelCodex, models.ChannelOpenAIImage:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Apply custom headers
	if ch.CustomHeaders != "" {
		var customHeaders map[string]string
		if err := json.Unmarshal([]byte(ch.CustomHeaders), &customHeaders); err == nil {
			for k, v := range customHeaders {
				req.Header.Set(k, v)
			}
		}
	}

	return h.getClient(ch).Do(req)
}

// matchAny 检查模型是否匹配任意一个模式（支持通配符）
func matchAny(patterns []string, model string) bool {
	for _, p := range patterns {
		matched, _ := filepath.Match(p, model)
		if matched {
			return true
		}
		if strings.HasPrefix(p, "*") && strings.HasSuffix(model, strings.TrimPrefix(p, "*")) {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(model, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

// translateToClaude 将 OpenAI 格式转换为 Claude API 格式
func (h *Handler) translateToClaude(model string, body []byte, ch *models.Channel) ([]byte, string, error) {
	var openaiReq struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		MaxTokens   int       `json:"max_tokens"`
		Temperature float64   `json:"temperature"`
		Stream      bool      `json:"stream"`
	}
	if err := json.Unmarshal(body, &openaiReq); err != nil {
		return nil, "", err
	}

	var claudeMessages []ClaudeMessage
	var systemPrompt string
	for _, msg := range openaiReq.Messages {
		switch msg.Role {
		case "system":
			systemPrompt = msg.Content
		case "user":
			claudeMessages = append(claudeMessages, ClaudeMessage{Role: "user", Content: msg.Content})
		case "assistant":
			claudeMessages = append(claudeMessages, ClaudeMessage{Role: "assistant", Content: msg.Content})
		}
	}

	claudeReq := map[string]interface{}{
		"model":      ch.Model,
		"messages":   claudeMessages,
		"max_tokens": openaiReq.MaxTokens,
		"stream":     openaiReq.Stream,
	}
	if systemPrompt != "" {
		claudeReq["system"] = systemPrompt
	}
	if openaiReq.Temperature > 0 {
		claudeReq["temperature"] = openaiReq.Temperature
	}

	claudeBody, _ := json.Marshal(claudeReq)
	targetURL := fmt.Sprintf("%s/v1/messages", ch.BaseURL)
	return claudeBody, targetURL, nil
}

// translateToGemini 将 OpenAI 格式转换为 Gemini API 格式
func (h *Handler) translateToGemini(model string, body []byte, ch *models.Channel) ([]byte, string, error) {
	var openaiReq struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		MaxTokens   int       `json:"max_tokens"`
		Temperature float64   `json:"temperature"`
		Stream      bool      `json:"stream"`
	}
	if err := json.Unmarshal(body, &openaiReq); err != nil {
		return nil, "", err
	}

	var contents []GeminiContent
	for _, msg := range openaiReq.Messages {
		if msg.Role == "system" {
			continue
		}
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: msg.Content}},
		})
	}

	geminiReq := map[string]interface{}{
		"contents": contents,
	}
	if openaiReq.MaxTokens > 0 {
		geminiReq["generationConfig"] = map[string]interface{}{
			"maxOutputTokens": openaiReq.MaxTokens,
		}
	}

	geminiBody, _ := json.Marshal(geminiReq)
	geminiModel := ch.Model
	if geminiModel == "" {
		geminiModel = "gemini-pro"
	}
	targetURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", geminiModel)
	return geminiBody, targetURL, nil
}

// Message 表示 OpenAI 消息格式
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ClaudeMessage 表示 Claude 消息格式
type ClaudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GeminiContent 表示 Gemini 内容格式
type GeminiContent struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart 表示 Gemini 内容的一个片段
type GeminiPart struct {
	Text string `json:"text"`
}

// StreamResponse 将流式响应写入客户端
func (h *Handler) StreamResponse(w http.ResponseWriter, r *http.Request, ch *models.Channel, body []byte) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	resp, err := h.forwardRequest(ch, "", body, r, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}

// extractTokenUsage parses token counts from the upstream response body.
// For non-streaming OpenAI/Codex responses, tokens are in the "usage" field.
// For streaming responses, tokens may be in the final chunk or estimated.
// For Claude responses, tokens are in the "usage" field with different key names.
func extractTokenUsage(respBody []byte, model string, chType models.ChannelType) *token.TokenResult {
	// Try to parse as JSON first
	var resp map[string]interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		// Not JSON or parse error, estimate from body length
		return token.NewResult(model, len(respBody))
	}

	// Check for usage field
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		var prompt, completion, total int
		if v, ok := usage["prompt_tokens"].(float64); ok {
			prompt = int(v)
		}
		if v, ok := usage["completion_tokens"].(float64); ok {
			completion = int(v)
		}
		if v, ok := usage["total_tokens"].(float64); ok {
			total = int(v)
		}
		if prompt == 0 && completion == 0 && total == 0 {
			// Claude format: input_tokens, output_tokens
			if v, ok := usage["input_tokens"].(float64); ok {
				prompt = int(v)
			}
			if v, ok := usage["output_tokens"].(float64); ok {
				completion = int(v)
			}
			if total == 0 {
				total = prompt + completion
			}
		}
		if total > 0 {
			return &token.TokenResult{
				Model:            model,
				PromptTokens:     prompt,
				CompletionTokens: completion,
				TotalTokens:      total,
			}
		}
	}

	// No usage field found, estimate from body length
	return token.NewResult(model, len(respBody))
}
