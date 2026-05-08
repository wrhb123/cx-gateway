package session

import (
	"testing"
	"time"
)

func TestCreateSession(t *testing.T) {
	mgr := New(24 * time.Hour)

	messages := []Message{
		{Role: "user", Content: "Hello"},
	}

	responseID := mgr.CreateSession("gpt-4o", messages)

	if responseID == "" {
		t.Fatal("expected non-empty responseID")
	}

	sess, ok := mgr.GetSession(responseID)
	if !ok {
		t.Fatal("expected to find session")
	}

	if sess.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", sess.Model)
	}

	if len(sess.Messages) != 1 {
		t.Errorf("message count = %d, want 1", len(sess.Messages))
	}
}

func TestGetSessionNotFound(t *testing.T) {
	mgr := New(24 * time.Hour)

	_, ok := mgr.GetSession("nonexistent")
	if ok {
		t.Error("expected session not found")
	}
}

func TestAddMessage(t *testing.T) {
	mgr := New(24 * time.Hour)

	messages := []Message{
		{Role: "user", Content: "Hello"},
	}

	responseID := mgr.CreateSession("gpt-4o", messages)

	ok := mgr.AddMessage(responseID, Message{Role: "assistant", Content: "Hi there"})
	if !ok {
		t.Fatal("expected to add message")
	}

	sess, _ := mgr.GetSession(responseID)
	if len(sess.Messages) != 2 {
		t.Errorf("message count = %d, want 2", len(sess.Messages))
	}

	if sess.Messages[1].Role != "assistant" {
		t.Errorf("second message role = %q, want assistant", sess.Messages[1].Role)
	}
}

func TestAddMessageNotFound(t *testing.T) {
	mgr := New(24 * time.Hour)

	ok := mgr.AddMessage("nonexistent", Message{Role: "user", Content: "test"})
	if ok {
		t.Error("expected to fail adding message to nonexistent session")
	}
}

func TestUpdateSession(t *testing.T) {
	mgr := New(24 * time.Hour)

	responseID := mgr.CreateSession("gpt-4o", []Message{{Role: "user", Content: "Hello"}})

	newMessages := []Message{
		{Role: "user", Content: "New message"},
		{Role: "assistant", Content: "New response"},
	}

	ok := mgr.UpdateSession(responseID, newMessages, "claude-3-opus")
	if !ok {
		t.Fatal("expected to update session")
	}

	sess, _ := mgr.GetSession(responseID)
	if sess.Model != "claude-3-opus" {
		t.Errorf("model = %q, want claude-3-opus", sess.Model)
	}

	if len(sess.Messages) != 2 {
		t.Errorf("message count = %d, want 2", len(sess.Messages))
	}
}

func TestDeleteSession(t *testing.T) {
	mgr := New(24 * time.Hour)

	responseID := mgr.CreateSession("gpt-4o", []Message{{Role: "user", Content: "Hello"}})

	mgr.DeleteSession(responseID)

	_, ok := mgr.GetSession(responseID)
	if ok {
		t.Error("expected session to be deleted")
	}
}

func TestListSessions(t *testing.T) {
	mgr := New(24 * time.Hour)

	mgr.CreateSession("gpt-4o", []Message{{Role: "user", Content: "Hello 1"}})
	mgr.CreateSession("claude-3", []Message{{Role: "user", Content: "Hello 2"}})

	sessions := mgr.ListSessions()
	if len(sessions) != 2 {
		t.Errorf("session count = %d, want 2", len(sessions))
	}
}

func TestSessionExpired(t *testing.T) {
	// Create a session manager with 1ms TTL
	mgr := New(1 * time.Millisecond)

	responseID := mgr.CreateSession("gpt-4o", []Message{{Role: "user", Content: "Hello"}})

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	_, ok := mgr.GetSession(responseID)
	if ok {
		t.Error("expected session to be expired")
	}
}

func TestSessionCleanup(t *testing.T) {
	mgr := New(1 * time.Millisecond)

	mgr.CreateSession("gpt-4o", []Message{{Role: "user", Content: "Hello"}})

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Manually trigger cleanup
	mgr.cleanupExpired()

	sessions := mgr.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("session count after cleanup = %d, want 0", len(sessions))
	}
}

func TestGenerateResponseID(t *testing.T) {
	id1 := generateResponseID()
	id2 := generateResponseID()

	if id1 == id2 {
		t.Error("expected unique response IDs")
	}

	if len(id1) < 10 {
		t.Errorf("response ID too short: %s", id1)
	}
}

func TestSessionJSON(t *testing.T) {
	mgr := New(24 * time.Hour)

	messages := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}

	responseID := mgr.CreateSession("gpt-4o", messages)
	sess, _ := mgr.GetSession(responseID)

	data, err := sess.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Verify the JSON contains message_count
	jsonStr := string(data)
	if !contains(jsonStr, "message_count") {
		t.Errorf("JSON missing message_count: %s", jsonStr)
	}

	if !contains(jsonStr, "response_id") {
		t.Errorf("JSON missing response_id: %s", jsonStr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	if start+len(substr) > len(s) {
		return false
	}
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
