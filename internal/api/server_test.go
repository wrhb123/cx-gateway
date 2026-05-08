package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-proxy-gateway/internal/auth"
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

type testServer struct {
	srv       *Server
	authMgr   *auth.Manager
	authToken string
	cleanup   func()
}

func newTestServer(t *testing.T) *testServer {
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
	authMgr := auth.New("admin", "admin", 24*time.Hour)

	token, err := authMgr.Login("admin", "admin")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	srv := New(database, chMgr, fo, modelRtr, proxyHdl, authMgr, "", nil, "", "", "")

	return &testServer{
		srv:       srv,
		authMgr:   authMgr,
		authToken: token,
		cleanup: func() {
			database.Close()
			os.Remove(dbPath)
		},
	}
}

func (ts *testServer) newAdminRequest(method, url, body string) *http.Request {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "admin_session",
		Value:    ts.authToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return req
}

// ============ Proxy Route Tests ============

func TestHandleFallback(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

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
	ts := newTestServer(t)
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

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
	ts := newTestServer(t)
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleChatCompletionsInvalidJSON(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	body := strings.NewReader("not json")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleChatCompletionsMissingModel(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	body := strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleChatCompletionsSuccess(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

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
	ts := newTestServer(t)
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/v1/images/generations", nil)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// ============ Admin Auth Tests ============

func TestAuthLogin(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["username"] != "admin" {
		t.Errorf("username = %q, want 'admin'", resp["username"])
	}
}

func TestAuthLoginInvalid(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMe(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodGet, "/api/auth/me", "")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["username"] != "admin" {
		t.Errorf("username = %q, want 'admin'", resp["username"])
	}
}

func TestAuthRequired(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (unauthorized)", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthLogout(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  "admin_session",
		Value: ts.authToken,
	})
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// After logout, the token should be invalid
	req2 := ts.newAdminRequest(http.MethodGet, "/api/auth/me", "")
	// Manually replace the cookie with the now-invalidated token
	req2.Header.Del("Cookie")
	req2.AddCookie(&http.Cookie{
		Name:     "admin_session",
		Value:    ts.authToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	rec2 := httptest.NewRecorder()
	adminMux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("after logout, status = %d, want %d", rec2.Code, http.StatusUnauthorized)
	}
}

// ============ Admin API Tests ============

func TestHandleChannelsEmpty(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodGet, "/api/channels", "")
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	chData := `{"name":"Test Channel","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","priority":10,"weight":1,"enabled":true}`
	req := ts.newAdminRequest(http.MethodPost, "/api/channels", chData)
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	chData := `{"name":"Get Test","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","enabled":true}`
	req := ts.newAdminRequest(http.MethodPost, "/api/channels", chData)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	req = ts.newAdminRequest(http.MethodGet, "/api/channels/1", "")
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	chData := `{"name":"Delete Test","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","enabled":true}`
	req := ts.newAdminRequest(http.MethodPost, "/api/channels", chData)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	req = ts.newAdminRequest(http.MethodDelete, "/api/channels/1", "")
	rec = httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestHandleUpdateChannel(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	chData := `{"name":"Original","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-test","model":"gpt-4o","enabled":true}`
	req := ts.newAdminRequest(http.MethodPost, "/api/channels", chData)
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	updateData := `{"id":1,"name":"Updated","type":"openai_chat","base_url":"https://api.openai.com","api_key":"sk-new","model":"gpt-4","enabled":true,"priority":0,"weight":1,"max_retries":3,"timeout":120}`
	req = ts.newAdminRequest(http.MethodPut, "/api/channels/1", updateData)
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodGet, "/api/channels/999", "")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleChannelByIDInvalidID(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodGet, "/api/channels/abc", "")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateRoute(t *testing.T) {
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	routeData := `{"pattern":"gpt-*","channel_ids":[1,2],"load_balance":"round_robin","priority":10,"enabled":true}`
	req := ts.newAdminRequest(http.MethodPost, "/api/routes", routeData)
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodGet, "/api/routes", "")
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodGet, "/api/stats", "")
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodPost, "/api/reload", "")
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
	ts := newTestServer(t)
	defer ts.cleanup()

	adminMux := http.NewServeMux()
	ts.srv.RegisterAdminRoutes(adminMux)

	req := ts.newAdminRequest(http.MethodGet, "/api/reload", "")
	rec := httptest.NewRecorder()
	adminMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// ============ Proxy API Key Auth Tests ============

func TestProxyAPIKeyRequired(t *testing.T) {
	ts := newTestServer(t)
	ts.srv.proxyAPIKey = "test-secret-key"
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestProxyAPIKeyValid(t *testing.T) {
	ts := newTestServer(t)
	ts.srv.proxyAPIKey = "test-secret-key"
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-secret-key")
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestProxyAPIKeyInvalid(t *testing.T) {
	ts := newTestServer(t)
	ts.srv.proxyAPIKey = "test-secret-key"
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestProxyAPIKeyViaHeader(t *testing.T) {
	ts := newTestServer(t)
	ts.srv.proxyAPIKey = "test-secret-key"
	defer ts.cleanup()

	proxyMux := http.NewServeMux()
	ts.srv.RegisterProxyRoutes(proxyMux)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "test-secret-key")
	rec := httptest.NewRecorder()
	proxyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
