package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/failover"
	"ai-proxy-gateway/internal/models"
)

// Adapter handles native protocol requests (Claude, Responses, Gemini)
type Adapter struct {
	channelMgr *channel.Manager
	failover   *failover.FailoverManager
	db         *db.Database
	client     *http.Client
}

// New creates a new protocol adapter
func New(chMgr *channel.Manager, fo *failover.FailoverManager, database *db.Database) *Adapter {
	return &Adapter{
		channelMgr: chMgr,
		failover:   fo,
		db:         database,
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

// HandleClaudeMessages handles POST /v1/messages (Claude native protocol)
func (a *Adapter) HandleClaudeMessages(w http.ResponseWriter, r *http.Request) {
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
		Model       string `json:"model"`
		MaxTokens   int    `json:"max_tokens"`
		Stream      bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	ch, err := a.selectChannel(models.ChannelClaude, req.Model)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
		return
	}

	a.forwardToClaude(ch, body, r, w)
}

// HandleResponsesAPI handles POST /v1/responses (OpenAI Responses API)
func (a *Adapter) HandleResponsesAPI(w http.ResponseWriter, r *http.Request) {
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
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	ch, err := a.selectChannel(models.ChannelCodex, req.Model)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
		return
	}

	a.forwardToResponsesAPI(ch, body, r, w)
}

// HandleResponsesCompact handles POST /v1/responses/{response_id}/compact
func (a *Adapter) HandleResponsesCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	responseID := r.PathValue("response_id")
	if responseID == "" {
		http.Error(w, "response_id is required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		body = []byte{}
	}
	defer r.Body.Close()

	// Use default model for compact - it doesn't need model selection
	ch, err := a.selectAnyChannel()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
		return
	}

	a.forwardToResponsesCompact(ch, responseID, body, r, w)
}

// HandleGeminiGenerateContent handles POST /v1beta/models/{model}:generateContent
func (a *Adapter) HandleGeminiGenerateContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	geminiModel := r.PathValue("model")
	if geminiModel == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	ch, err := a.selectChannel(models.ChannelGemini, geminiModel)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
		return
	}

	a.forwardToGemini(ch, geminiModel, body, r, w)
}

// HandleGeminiStreamGenerateContent handles POST /v1beta/models/{model}:streamGenerateContent
func (a *Adapter) HandleGeminiStreamGenerateContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	geminiModel := r.PathValue("model")
	if geminiModel == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	ch, err := a.selectChannel(models.ChannelGemini, geminiModel)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
		return
	}

	a.forwardToGeminiStream(ch, geminiModel, body, r, w)
}

// forwardToClaude forwards request to Claude API
func (a *Adapter) forwardToClaude(ch *models.Channel, body []byte, r *http.Request, w http.ResponseWriter) {
	targetURL := fmt.Sprintf("%s/v1/messages", ch.BaseURL)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	apiKey := ch.APIKey
	if activeKey, err := a.db.GetActiveKeyForChannel(ch.ID); err == nil && activeKey != nil {
		apiKey = activeKey.APIKey
		a.db.IncrementKeyUsage(activeKey.ID)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		a.failover.RecordFailure(ch.ID)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	a.failover.RecordSuccess(ch.ID)

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// forwardToResponsesAPI forwards request to OpenAI Responses API
func (a *Adapter) forwardToResponsesAPI(ch *models.Channel, body []byte, r *http.Request, w http.ResponseWriter) {
	targetURL := fmt.Sprintf("%s/v1/responses", ch.BaseURL)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	apiKey := ch.APIKey
	if activeKey, err := a.db.GetActiveKeyForChannel(ch.ID); err == nil && activeKey != nil {
		apiKey = activeKey.APIKey
		a.db.IncrementKeyUsage(activeKey.ID)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		a.failover.RecordFailure(ch.ID)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	a.failover.RecordSuccess(ch.ID)

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// forwardToResponsesCompact forwards request to Responses compact endpoint
func (a *Adapter) forwardToResponsesCompact(ch *models.Channel, responseID string, body []byte, r *http.Request, w http.ResponseWriter) {
	targetURL := fmt.Sprintf("%s/v1/responses/%s/compact", ch.BaseURL, responseID)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	apiKey := ch.APIKey
	if activeKey, err := a.db.GetActiveKeyForChannel(ch.ID); err == nil && activeKey != nil {
		apiKey = activeKey.APIKey
		a.db.IncrementKeyUsage(activeKey.ID)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		a.failover.RecordFailure(ch.ID)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	a.failover.RecordSuccess(ch.ID)

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// forwardToGemini forwards request to Gemini API
func (a *Adapter) forwardToGemini(ch *models.Channel, geminiModel string, body []byte, r *http.Request, w http.ResponseWriter) {
	apiKey := ch.APIKey
	if activeKey, err := a.db.GetActiveKeyForChannel(ch.ID); err == nil && activeKey != nil {
		apiKey = activeKey.APIKey
		a.db.IncrementKeyUsage(activeKey.ID)
	}
	targetURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", geminiModel, apiKey)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		a.failover.RecordFailure(ch.ID)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	a.failover.RecordSuccess(ch.ID)

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// forwardToGeminiStream forwards request to Gemini streaming API
func (a *Adapter) forwardToGeminiStream(ch *models.Channel, geminiModel string, body []byte, r *http.Request, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	apiKey := ch.APIKey
	if activeKey, err := a.db.GetActiveKeyForChannel(ch.ID); err == nil && activeKey != nil {
		apiKey = activeKey.APIKey
		a.db.IncrementKeyUsage(activeKey.ID)
	}
	targetURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", geminiModel, apiKey)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		a.failover.RecordFailure(ch.ID)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	a.failover.RecordSuccess(ch.ID)

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// selectChannel selects a channel for the given type and model
func (a *Adapter) selectChannel(chType models.ChannelType, model string) (*models.Channel, error) {
	ch, err := a.channelMgr.SelectChannel(chType, "round_robin")
	if err != nil {
		return nil, err
	}
	if !ch.Enabled || !a.failover.IsHealthy(ch.ID) {
		return nil, fmt.Errorf("no available channel for type %s", chType)
	}
	return ch, nil
}

// selectAnyChannel selects any available channel (for operations that don't need specific type)
func (a *Adapter) selectAnyChannel() (*models.Channel, error) {
	allChannels := a.channelMgr.GetAllChannels()
	for _, ch := range allChannels {
		if ch.Enabled && a.failover.IsHealthy(ch.ID) {
			return ch, nil
		}
	}
	return nil, fmt.Errorf("no available channels")
}
