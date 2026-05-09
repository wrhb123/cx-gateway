package i18n

import (
	"testing"
)

func TestT_Chinese(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"unauthorized", "未授权"},
		{"forbidden", "禁止访问"},
		{"not_found", "资源不存在"},
		{"login_required", "请先登录"},
		{"invalid_credentials", "用户名或密码错误"},
	}

	for _, tt := range tests {
		result := T("zh", tt.key)
		if result != tt.expected {
			t.Errorf("T(zh, %q) = %q, want %q", tt.key, result, tt.expected)
		}
	}
}

func TestT_English(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"unauthorized", "Unauthorized"},
		{"forbidden", "Forbidden"},
		{"not_found", "Not Found"},
		{"login_required", "Login Required"},
		{"invalid_credentials", "Invalid Username or Password"},
	}

	for _, tt := range tests {
		result := T("en", tt.key)
		if result != tt.expected {
			t.Errorf("T(en, %q) = %q, want %q", tt.key, result, tt.expected)
		}
	}
}

func TestT_Fallback(t *testing.T) {
	// Unknown language should fallback to English
	result := T("fr", "not_found")
	expected := "Not Found"
	if result != expected {
		t.Errorf("T(fr, %q) = %q, want %q", "not_found", result, expected)
	}

	// Unknown key should return key itself
	result = T("zh", "unknown_key_xyz")
	expected = "unknown_key_xyz"
	if result != expected {
		t.Errorf("T(zh, %q) = %q, want %q", "unknown_key_xyz", result, expected)
	}
}

func TestErrorCode(t *testing.T) {
	code, msg, status := ErrorCode("zh", "unauthorized", 401)

	if code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", code)
	}

	if msg != "未授权" {
		t.Errorf("msg = %q, want 未授权", msg)
	}

	if status != 401 {
		t.Errorf("status = %d, want 401", status)
	}
}
