package router

import (
	"testing"

	"ai-proxy-gateway/internal/models"
)

func TestInferChannelType_Claude(t *testing.T) {
	tests := []string{"claude", "claude-3-opus", "claude-sonnet-4-20250514", "claude-3-haiku"}
	for _, name := range tests {
		if got := InferChannelType(name); got != models.ChannelClaude {
			t.Errorf("InferChannelType(%q) = %s, want %s", name, got, models.ChannelClaude)
		}
	}
}

func TestInferChannelType_Gemini(t *testing.T) {
	tests := []string{"gem", "gemini-pro", "gemini-1.5-flash", "gemini-2.0"}
	for _, name := range tests {
		if got := InferChannelType(name); got != models.ChannelGemini {
			t.Errorf("InferChannelType(%q) = %s, want %s", name, got, models.ChannelGemini)
		}
	}
}

func TestInferChannelType_OpenAIImage(t *testing.T) {
	tests := []string{"dall-e-2", "dall-e-3", "dall-e-4", "dall-e-5"}
	for _, name := range tests {
		if got := InferChannelType(name); got != models.ChannelOpenAIImage {
			t.Errorf("InferChannelType(%q) = %s, want %s", name, got, models.ChannelOpenAIImage)
		}
	}
}

func TestInferChannelType_OpenAIChat(t *testing.T) {
	tests := []string{"gpt-4o", "gpt-4", "gpt-3.5-turbo", "o1", "o3-mini", "text-davinci-003", "unknown-model"}
	for _, name := range tests {
		if got := InferChannelType(name); got != models.ChannelOpenAIChat {
			t.Errorf("InferChannelType(%q) = %s, want %s", name, got, models.ChannelOpenAIChat)
		}
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"gpt-*", "gpt-4o", true},
		{"gpt-*", "gpt-4", true},
		{"gpt-*", "claude-3", false},
		{"claude-*", "claude-3-opus", true},
		{"*", "anything", true},
		{"gpt-4?", "gpt-4o", true},
		{"gpt-4?", "gpt-4", false},
		{"", "gpt-4", false},
	}
	for _, tt := range tests {
		got := match(tt.pattern, tt.name)
		if got != tt.want {
			t.Errorf("match(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}
