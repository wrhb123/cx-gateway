package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/failover"
	"ai-proxy-gateway/internal/models"
	"ai-proxy-gateway/internal/router"
)

// Handler 是核心的代理处理器
type Handler struct {
	channelMgr *channel.Manager
	failover   *failover.FailoverManager
	modelRtr   *router.ModelRouter
	client     *http.Client
}

// New 创建新的代理处理器
func New(chMgr *channel.Manager, fo *failover.FailoverManager, mr *router.ModelRouter) *Handler {
	return &Handler{
		channelMgr: chMgr,
		failover:   fo,
		modelRtr:   mr,
		client: &http.Client{
			Timeout: 300 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ProxyRequest 处理传入的 API 请求
func (h *Handler) ProxyRequest(w http.ResponseWriter, r *http.Request, model string, body []byte, chType models.ChannelType) {
	var channelIDs []int64
	strategy := "round_robin"

	routeIDs, routeStrategy := h.modelRtr.Resolve(model)
	if len(routeIDs) > 0 {
		channelIDs = routeIDs
		if routeStrategy != "" {
			strategy = routeStrategy
		}
	}

	if len(channelIDs) == 0 {
		ch, err := h.channelMgr.SelectChannel(chType, strategy)
		if err != nil {
			http.Error(w, `{"error":"no available channel"}`, http.StatusServiceUnavailable)
			return
		}
		channelIDs = []int64{ch.ID}
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

		resp, err := h.forwardRequest(ch, model, body, r)
		if err != nil {
			h.failover.RecordFailure(chID)
			lastErr = err
			continue
		}

		h.failover.RecordSuccess(chID)

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		resp.Body.Close()
		return
	}

	if lastErr != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, lastErr.Error()), http.StatusBadGateway)
		return
	}
	http.Error(w, `{"error":"all channels failed"}`, http.StatusServiceUnavailable)
}

// forwardRequest 转发请求到目标渠道
func (h *Handler) forwardRequest(ch *models.Channel, model string, body []byte, r *http.Request) (*http.Response, error) {
	var reqBody []byte
	var targetURL string
	var req *http.Request
	var err error

	switch ch.Type {
	case models.ChannelClaude:
		reqBody, targetURL, err = h.translateToClaude(model, body, ch)
	case models.ChannelGemini:
		reqBody, targetURL, err = h.translateToGemini(model, body, ch)
	case models.ChannelOpenAIChat, models.ChannelCodex:
		reqBody = body
		targetURL = fmt.Sprintf("%s/v1/chat/completions", ch.BaseURL)
	case models.ChannelOpenAIImage:
		reqBody = body
		targetURL = fmt.Sprintf("%s/v1/images/generations", ch.BaseURL)
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", ch.Type)
	}

	if err != nil {
		return nil, err
	}

	req, err = http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	switch ch.Type {
	case models.ChannelClaude:
		req.Header.Set("x-api-key", ch.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case models.ChannelGemini:
		targetURL = targetURL + "?key=" + ch.APIKey
	case models.ChannelOpenAIChat, models.ChannelCodex, models.ChannelOpenAIImage:
		req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	}

	return h.client.Do(req)
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

	resp, err := h.forwardRequest(ch, "", body, r)
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
