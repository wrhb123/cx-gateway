package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/failover"
	"ai-proxy-gateway/internal/models"
	mr "ai-proxy-gateway/internal/router"
)

// mockProxyHandler is a mock proxy handler that avoids real network calls
type mockProxyHandler struct {
	channelMgr *channel.Manager
	failover   *failover.FailoverManager
	modelRtr   *mr.ModelRouter
}

func (h *mockProxyHandler) ProxyRequest(w http.ResponseWriter, r *http.Request, model string, body []byte, chType models.ChannelType) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"model":   model,
		"status":  "proxied",
		"channel": string(chType),
	})
}

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	chMgr := channel.New(database)
	fo := failover.NewFailoverManager(5, 60)
	modelRtr := mr.New(database)
	proxyHdl := &mockProxyHandler{channelMgr: chMgr, failover: fo, modelRtr: modelRtr}

	srv := New(database, chMgr, fo, modelRtr, proxyHdl)

	cleanup := func() {
		database.Close()
		os.Remove(dbPath)
	}
	return srv, cleanup
}

func TestHandleFallback(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["service"] != "ai-proxy-gateway" {
		t.Errorf("service = %q, want 'ai-proxy-gateway'", resp["service"])
	}
}

func TestHandleListModels(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["object"] != "list" {
		t.Errorf("object = %v, want 'list'", resp["object"])
	}
}

func TestHandleChatCompletionsInvalidMethod(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleChatCompletionsInvalidJSON(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	body := strings.NewReader("not json")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleChatCompletionsMissingModel(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	body := strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleChatCompletionsSuccess(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	body := strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["model"] != "gpt-4o" {
		t.Errorf("model = %q, want 'gpt-4o'", resp["model"])
	}
}

func TestHandleImageGenerationInvalidMethod(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	proxyMux := http.NewServeMux()
	srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/v1/images/generations", nil)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// Admin API tests
func TestHandleChannelsEmpty(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var channels []models.Channel
	json.NewDecoder(rec.Body).Decode(&channels)
	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(channels))
	}
}

func TestHandleCreateChannel(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	chData := `{"name":"Test Channel","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","priority":10,"weight":1,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/channels", strings.NewReader(chData))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var ch models.Channel
	json.NewDecoder(rec.Body).Decode(&ch)
	if ch.Name != "Test Channel" {
		t.Errorf("Name = %q, want 'Test Channel'", ch.Name)
	}
}

func TestHandleGetChannelByID(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	// Create a channel first
	chData := `{"name":"Get Test","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/channels", strings.NewReader(chData))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	var created models.Channel
	json.NewDecoder(rec.Body).Decode(&created)

	// Get the channel
	req = httptest.NewRequest(http.MethodGet, "/api/channels/1", nil)
	rec = httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got models.Channel
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Name != "Get Test" {
		t.Errorf("Name = %q, want 'Get Test'", got.Name)
	}
}

func TestHandleDeleteChannel(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	// Create a channel
	chData := `{"name":"Delete Test","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/channels", strings.NewReader(chData))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	// Delete the channel
	req = httptest.NewRequest(http.MethodDelete, "/api/channels/1", nil)
	rec = httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestHandleUpdateChannel(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	// Create a channel
	chData := `{"name":"Original","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/channels", strings.NewReader(chData))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	// Update the channel
	updateData := `{"id":1,"name":"Updated","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-new","model":"gpt-4","enabled":true,"priority":0,"weight":1,"max_retries":3,"timeout":120}`
	req = httptest.NewRequest(http.MethodPut, "/api/channels/1", strings.NewReader(updateData))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got models.Channel
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Name != "Updated" {
		t.Errorf("Name = %q, want 'Updated'", got.Name)
	}
}

func TestHandleChannelByIDNotFound(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/999", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleChannelByIDInvalidID(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/abc", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateRoute(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	routeData := `{"pattern":"gpt-*","channel_ids":[1,2],"load_balance":"round_robin","priority":10,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/routes", strings.NewReader(routeData))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var route models.ModelRoute
	json.NewDecoder(rec.Body).Decode(&route)
	if route.Pattern != "gpt-*" {
		t.Errorf("Pattern = %q, want 'gpt-*'", route.Pattern)
	}
}

func TestHandleListRoutesEmpty(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodGet, "/api/routes", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var routes []models.ModelRoute
	json.NewDecoder(rec.Body).Decode(&routes)
	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestHandleStats(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var stats []map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&stats)
	if len(stats) != 0 {
		t.Errorf("expected 0 stats, got %d", len(stats))
	}
}

func TestHandleReload(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodPost, "/api/reload", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "reloaded" {
		t.Errorf("status = %q, want 'reloaded'", resp["status"])
	}
}

func TestHandleReloadInvalidMethod(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	adminMux := http.NewServeMux()
	srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
