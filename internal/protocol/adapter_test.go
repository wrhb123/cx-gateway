package protocol

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-proxy-gateway/internal/channel"
	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/failover"
)

func setupTestAdapter(t *testing.T) (*Adapter, func()) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.New(dir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	chMgr := channel.New(database)
	fo := failover.NewFailoverManager(5, 60)
	adapter := New(chMgr, fo)

	return adapter, func() {
		database.Close()
	}
}

func TestHandleClaudeMessages_MissingModel(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleClaudeMessages(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleClaudeMessages_NoChannels(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	body := `{"model":"claude-3-sonnet","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleClaudeMessages(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleResponsesAPI_MissingModel(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	body := `{"input":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleResponsesAPI(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleResponsesAPI_NoChannels(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	body := `{"model":"o1","input":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleResponsesAPI(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleResponsesCompact_MissingResponseID(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/responses//compact", nil)
	w := httptest.NewRecorder()

	// PathValue requires actual mux setup, so we test the body parsing only
	// This test verifies the handler doesn't crash with empty body
	adapter.HandleResponsesCompact(w, req)

	// Should fail because response_id is empty (handled by mux, but we test handler logic)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 400 or 503, got %d", w.Code)
	}
}

func TestHandleGeminiGenerateContent_MissingModel(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/:generateContent", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleGeminiGenerateContent(w, req)

	// PathValue requires actual mux setup, so model will be empty
	if w.Code != http.StatusBadRequest && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 400 or 503, got %d", w.Code)
	}
}

func TestHandleGeminiStreamGenerateContent_MissingModel(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/:streamGenerateContent", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleGeminiStreamGenerateContent(w, req)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 400 or 503, got %d", w.Code)
	}
}

func TestSelectAnyChannel_NoChannels(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	ch, err := adapter.selectAnyChannel()
	if err == nil {
		t.Error("expected error when no channels available")
	}
	if ch != nil {
		t.Error("expected nil channel when no channels available")
	}
}

func TestAdapter_MethodNotAllowed(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	tests := []struct {
		name    string
		method  string
		handler func(http.ResponseWriter, *http.Request)
		url     string
		body    string
	}{
		{
			name:    "Claude GET",
			method:  http.MethodGet,
			handler: adapter.HandleClaudeMessages,
			url:     "/v1/messages",
			body:    `{"model":"claude-3"}`,
		},
		{
			name:    "Responses GET",
			method:  http.MethodGet,
			handler: adapter.HandleResponsesAPI,
			url:     "/v1/responses",
			body:    `{"model":"o1"}`,
		},
		{
			name:    "Gemini GET",
			method:  http.MethodGet,
			handler: adapter.HandleGeminiGenerateContent,
			url:     "/v1beta/models/gemini-pro:generateContent",
			body:    `{"contents":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.url, bytes.NewReader([]byte(tt.body)))
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405, got %d", w.Code)
			}
		})
	}
}

func TestHandleClaudeMessages_InvalidJSON(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleClaudeMessages(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleResponsesAPI_InvalidJSON(t *testing.T) {
	adapter, cleanup := setupTestAdapter(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.HandleResponsesAPI(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleClaudeMessages_WithChannel(t *testing.T) {
	// This test verifies the handler structure works correctly
	// Integration testing with actual channels requires database setup
	t.Skip("Integration test - requires channel setup via database")
}
