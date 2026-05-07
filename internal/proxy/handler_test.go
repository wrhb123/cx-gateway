package proxy

import (
	"encoding/json"
	"testing"

	"ai-proxy-gateway/internal/models"
)

func TestTranslateToClaude(t *testing.T) {
	h := &Handler{}
	ch := &models.Channel{
		BaseURL: "https://api.anthropic.com",
		Model:   "claude-3-opus",
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"},
		},
		"max_tokens":  100,
		"temperature": 0.7,
		"stream":      false,
	})

	reqBody, targetURL, err := h.translateToClaude("gpt-4o", body, ch)
	if err != nil {
		t.Fatalf("translateToClaude() error = %v", err)
	}

	if targetURL != "https://api.anthropic.com/v1/messages" {
		t.Errorf("targetURL = %q, want %q", targetURL, "https://api.anthropic.com/v1/messages")
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(reqBody, &claudeReq); err != nil {
		t.Fatalf("unmarshal claude request error = %v", err)
	}

	if claudeReq["model"] != "claude-3-opus" {
		t.Errorf("claude model = %v, want claude-3-opus", claudeReq["model"])
	}
	if claudeReq["system"] != "You are helpful" {
		t.Errorf("claude system prompt = %v, want 'You are helpful'", claudeReq["system"])
	}
	if claudeReq["max_tokens"].(float64) != 100 {
		t.Errorf("claude max_tokens = %v, want 100", claudeReq["max_tokens"])
	}
	if claudeReq["temperature"].(float64) != 0.7 {
		t.Errorf("claude temperature = %v, want 0.7", claudeReq["temperature"])
	}

	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 1 {
		t.Errorf("expected 1 message (system extracted separately), got %d", len(messages))
	}
}

func TestTranslateToClaudeInvalidJSON(t *testing.T) {
	h := &Handler{}
	ch := &models.Channel{BaseURL: "https://api.anthropic.com", Model: "claude-3"}

	_, _, err := h.translateToClaude("gpt-4o", []byte("not json"), ch)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTranslateToClaudeNoSystemPrompt(t *testing.T) {
	h := &Handler{}
	ch := &models.Channel{
		BaseURL: "https://api.anthropic.com",
		Model:   "claude-3-sonnet",
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens": 50,
		"stream":     false,
	})

	reqBody, _, err := h.translateToClaude("gpt-4o", body, ch)
	if err != nil {
		t.Fatalf("translateToClaude() error = %v", err)
	}

	var claudeReq map[string]interface{}
	json.Unmarshal(reqBody, &claudeReq)

	if _, hasSystem := claudeReq["system"]; hasSystem {
		t.Error("should not have system field when no system message")
	}
}

func TestTranslateToGemini(t *testing.T) {
	h := &Handler{}
	ch := &models.Channel{
		Model: "gemini-pro",
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "system", "content": "ignored"},
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there"},
		},
		"max_tokens": 200,
		"stream":     false,
	})

	reqBody, targetURL, err := h.translateToGemini("gpt-4o", body, ch)
	if err != nil {
		t.Fatalf("translateToGemini() error = %v", err)
	}

	expectedURL := "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent"
	if targetURL != expectedURL {
		t.Errorf("targetURL = %q, want %q", targetURL, expectedURL)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(reqBody, &geminiReq); err != nil {
		t.Fatalf("unmarshal gemini request error = %v", err)
	}

	contents := geminiReq["contents"].([]interface{})
	if len(contents) != 2 {
		t.Errorf("expected 2 contents (system ignored), got %d", len(contents))
	}

	// Check first content is user
	first := contents[0].(map[string]interface{})
	if first["role"] != "user" {
		t.Errorf("first content role = %v, want 'user'", first["role"])
	}

	// Check second content is model (assistant mapped to model)
	second := contents[1].(map[string]interface{})
	if second["role"] != "model" {
		t.Errorf("second content role = %v, want 'model'", second["role"])
	}

	// Check generation config
	genConfig := geminiReq["generationConfig"].(map[string]interface{})
	if genConfig["maxOutputTokens"].(float64) != 200 {
		t.Errorf("maxOutputTokens = %v, want 200", genConfig["maxOutputTokens"])
	}
}

func TestTranslateToGeminiInvalidJSON(t *testing.T) {
	h := &Handler{}
	ch := &models.Channel{Model: "gemini-pro"}

	_, _, err := h.translateToGemini("gpt-4o", []byte("not json"), ch)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTranslateToGeminiDefaultModel(t *testing.T) {
	h := &Handler{}
	ch := &models.Channel{Model: ""}

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "test"}},
	})

	_, targetURL, err := h.translateToGemini("gpt-4o", body, ch)
	if err != nil {
		t.Fatalf("translateToGemini() error = %v", err)
	}

	expectedURL := "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent"
	if targetURL != expectedURL {
		t.Errorf("targetURL = %q, want %q", targetURL, expectedURL)
	}
}
