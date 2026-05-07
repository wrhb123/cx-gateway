package models

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ServerPort != 8080 {
		t.Errorf("expected ServerPort 8080, got %d", cfg.ServerPort)
	}
	if cfg.AdminPort != 8081 {
		t.Errorf("expected AdminPort 8081, got %d", cfg.AdminPort)
	}
	if cfg.AdminUsername != "admin" {
		t.Errorf("expected AdminUsername 'admin', got %s", cfg.AdminUsername)
	}
	if cfg.AdminPassword != "admin" {
		t.Errorf("expected AdminPassword 'admin', got %s", cfg.AdminPassword)
	}
	if cfg.DatabasePath != "data/proxy.db" {
		t.Errorf("expected DatabasePath 'data/proxy.db', got %s", cfg.DatabasePath)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel 'info', got %s", cfg.LogLevel)
	}
	if cfg.MaxConcurrent != 1000 {
		t.Errorf("expected MaxConcurrent 1000, got %d", cfg.MaxConcurrent)
	}
	if cfg.RequestTimeout != 300 {
		t.Errorf("expected RequestTimeout 300, got %d", cfg.RequestTimeout)
	}
}

func TestDefaultConfigNotShared(t *testing.T) {
	cfg1 := DefaultConfig()
	cfg2 := DefaultConfig()
	cfg1.ServerPort = 9999
	if cfg2.ServerPort == 9999 {
		t.Error("DefaultConfig should return independent instances")
	}
}

func TestChannelTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		chType   ChannelType
		expected string
	}{
		{"Claude", ChannelClaude, "claude"},
		{"OpenAIChat", ChannelOpenAIChat, "openai_chat"},
		{"OpenAIImage", ChannelOpenAIImage, "openai_image"},
		{"Codex", ChannelCodex, "codex"},
		{"Gemini", ChannelGemini, "gemini"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.chType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.chType)
			}
		})
	}
}
