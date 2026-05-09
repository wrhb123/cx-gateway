package db

import (
	"os"
	"testing"

	"ai-proxy-gateway/internal/models"
)

func setupTestDB(t *testing.T) (*Database, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := New(dir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	// Create a test channel
	_, err = db.Conn.Exec(`
		INSERT INTO channels (name, type, base_url, api_key, model, priority, weight, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "test-openai", "openai_chat", "https://api.openai.com", "sk-test", "gpt-4", 1, 1, 1)
	if err != nil {
		t.Fatalf("failed to create test channel: %v", err)
	}

	// Init stats
	db.InitChannelStats(1)

	// Add some request logs
	db.LogRequest(&models.RequestLog{Model: "gpt-4", ChannelID: 1, ChannelName: "test-openai", Status: 200, Latency: 100})
	db.LogRequest(&models.RequestLog{Model: "gpt-4", ChannelID: 1, ChannelName: "test-openai", Status: 200, Latency: 150})
	db.LogRequest(&models.RequestLog{Model: "claude-3", ChannelID: 1, ChannelName: "test-openai", Status: 500, Latency: 200})

	return db, func() {
		db.Close()
		os.RemoveAll(dir)
	}
}

func TestGetStatsByProtocol(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Update stats for the test channel
	db.UpdateStats(1, true, 100, "")

	stats, err := db.GetStatsByProtocol()
	if err != nil {
		t.Fatalf("GetStatsByProtocol() error = %v", err)
	}

	if len(stats) == 0 {
		t.Fatal("expected at least one protocol stat")
	}

	// Check the openai_chat protocol exists
	found := false
	for _, s := range stats {
		if s["protocol"] == "openai_chat" {
			found = true
			if s["channel_count"].(int64) != 1 {
				t.Errorf("channel_count = %d, want 1", s["channel_count"])
			}
			break
		}
	}

	if !found {
		t.Error("expected to find openai_chat protocol")
	}
}

func TestGetModelStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	stats, err := db.GetModelStats()
	if err != nil {
		t.Fatalf("GetModelStats() error = %v", err)
	}

	if len(stats) == 0 {
		t.Fatal("expected at least one model stat")
	}

	// Check gpt-4 model exists
	found := false
	for _, s := range stats {
		if s["model"] == "gpt-4" {
			found = true
			if s["total_requests"].(int64) != 2 {
				t.Errorf("total_requests for gpt-4 = %d, want 2", s["total_requests"])
			}
			break
		}
	}

	if !found {
		t.Error("expected to find gpt-4 model")
	}
}
