package token

import (
	"testing"
)

func TestCountTokens_OpenAIFormat(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello, how are you?"}
		]
	}`)

	result, err := CountTokensForOpenAI(body)
	if err != nil {
		t.Fatalf("CountTokensForOpenAI() error = %v", err)
	}

	if result.Model != "gpt-4o" {
		t.Errorf("model = %q, want %q", result.Model, "gpt-4o")
	}

	if result.PromptTokens <= 0 {
		t.Errorf("prompt_tokens = %d, want > 0", result.PromptTokens)
	}

	if result.TotalTokens <= 0 {
		t.Errorf("total_tokens = %d, want > 0", result.TotalTokens)
	}
}

func TestCountTokens_ClaudeFormat(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-opus",
		"system": "You are helpful",
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)

	result, err := CountTokensForClaude(body)
	if err != nil {
		t.Fatalf("CountTokensForClaude() error = %v", err)
	}

	if result.Model != "claude-3-opus" {
		t.Errorf("model = %q, want %q", result.Model, "claude-3-opus")
	}

	if result.PromptTokens <= 0 {
		t.Errorf("prompt_tokens = %d, want > 0", result.PromptTokens)
	}
}

func TestCountTokens_EmptyMessages(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": []
	}`)

	result, err := CountTokensForOpenAI(body)
	if err != nil {
		t.Fatalf("CountTokensForOpenAI() error = %v", err)
	}

	if result.PromptTokens != 2 {
		t.Errorf("prompt_tokens = %d, want 2 (just overhead)", result.PromptTokens)
	}
}

func TestCountTokens_ChineseText(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "你好世界，这是一个测试"}
		]
	}`)

	result, err := CountTokensForOpenAI(body)
	if err != nil {
		t.Fatalf("CountTokensForOpenAI() error = %v", err)
	}

	if result.PromptTokens <= 0 {
		t.Errorf("prompt_tokens = %d, want > 0", result.PromptTokens)
	}
}

func TestCountTokens_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)

	_, err := CountTokensForOpenAI(body)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestModelInfo(t *testing.T) {
	info := ModelInfo("gpt-4o")

	if info["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", info["model"])
	}

	if info["tokenizer"] != "estimated" {
		t.Errorf("tokenizer = %v, want estimated", info["tokenizer"])
	}
}

func TestFormatEstimation(t *testing.T) {
	result := FormatEstimation(100)
	expected := "~100 tokens (estimated)"
	if result != expected {
		t.Errorf("FormatEstimation(100) = %q, want %q", result, expected)
	}
}
