package channel

import (
	"os"
	"path/filepath"
	"testing"

	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/models"
)

func newTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	mgr := New(database)
	cleanup := func() {
		database.Close()
		os.Remove(dbPath)
	}
	return mgr, cleanup
}

func addTestChannels(t *testing.T, mgr *Manager) {
	t.Helper()
	channels := []models.Channel{
		{Name: "OpenAI-1", Type: models.ChannelOpenAIChat, BaseURL: "https://a.com", APIKey: "key1", Model: "gpt-4o", Priority: 10, Weight: 3, Enabled: true},
		{Name: "OpenAI-2", Type: models.ChannelOpenAIChat, BaseURL: "https://b.com", APIKey: "key2", Model: "gpt-4", Priority: 5, Weight: 1, Enabled: true},
		{Name: "OpenAI-3", Type: models.ChannelOpenAIChat, BaseURL: "https://c.com", APIKey: "key3", Model: "gpt-3.5", Priority: 1, Weight: 1, Enabled: false},
		{Name: "Claude-1", Type: models.ChannelClaude, BaseURL: "https://d.com", APIKey: "key4", Model: "claude-3", Priority: 10, Weight: 1, Enabled: true},
	}
	for _, ch := range channels {
		id, err := mgr.db.CreateChannel(&ch)
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}
		mgr.db.InitChannelStats(id)
	}
	mgr.Reload()
}

func TestManagerGetChannel(t *testing.T) {
	mgr, cleanup := newTestManager(t)
	defer cleanup()
	addTestChannels(t, mgr)

	ch, ok := mgr.GetChannel(1)
	if !ok {
		t.Error("expected to find channel 1")
	}
	if ch.Name != "OpenAI-1" {
		t.Errorf("Name = %q, want %q", ch.Name, "OpenAI-1")
	}

	_, ok = mgr.GetChannel(999)
	if ok {
		t.Error("expected not to find channel 999")
	}
}

func TestManagerSelectChannelRoundRobin(t *testing.T) {
	mgr, cleanup := newTestManager(t)
	defer cleanup()
	addTestChannels(t, mgr)

	// Select from enabled OpenAI channels (IDs 1 and 2)
	ch1, err := mgr.SelectChannel(models.ChannelOpenAIChat, "round_robin")
	if err != nil {
		t.Fatalf("SelectChannel() error = %v", err)
	}
	ch2, err := mgr.SelectChannel(models.ChannelOpenAIChat, "round_robin")
	if err != nil {
		t.Fatalf("SelectChannel() error = %v", err)
	}
	// Round robin should return different channels
	if ch1.ID == ch2.ID {
		t.Errorf("round robin should select different channels, got same ID %d", ch1.ID)
	}
}

func TestManagerSelectChannelNoChannel(t *testing.T) {
	mgr, cleanup := newTestManager(t)
	defer cleanup()

	_, err := mgr.SelectChannel(models.ChannelOpenAIChat, "round_robin")
	if err != ErrNoChannelAvailable {
		t.Errorf("expected ErrNoChannelAvailable, got %v", err)
	}
}

func TestManagerSelectChannelDisabledType(t *testing.T) {
	mgr, cleanup := newTestManager(t)
	defer cleanup()
	addTestChannels(t, mgr)

	// Gemini channel doesn't exist
	_, err := mgr.SelectChannel(models.ChannelGemini, "round_robin")
	if err != ErrNoChannelAvailable {
		t.Errorf("expected ErrNoChannelAvailable for non-existent type, got %v", err)
	}
}

func TestManagerGetAllChannels(t *testing.T) {
	mgr, cleanup := newTestManager(t)
	defer cleanup()
	addTestChannels(t, mgr)

	all := mgr.GetAllChannels()
	if len(all) != 4 {
		t.Errorf("expected 4 channels, got %d", len(all))
	}
}

func TestManagerReload(t *testing.T) {
	mgr, cleanup := newTestManager(t)
	defer cleanup()
	addTestChannels(t, mgr)

	// Verify initial state
	all := mgr.GetAllChannels()
	if len(all) != 4 {
		t.Errorf("expected 4 channels, got %d", len(all))
	}

	// Add a new channel directly to DB
	newCh := models.Channel{
		Name:    "New Channel",
		Type:    models.ChannelGemini,
		BaseURL: "https://e.com",
		APIKey:  "key5",
		Model:   "gemini-pro",
		Enabled: true,
	}
	id, _ := mgr.db.CreateChannel(&newCh)
	mgr.db.InitChannelStats(id)

	// Before reload, new channel not in memory
	all = mgr.GetAllChannels()
	if len(all) != 4 {
		t.Errorf("before reload: expected 4 channels, got %d", len(all))
	}

	// After reload, new channel should be present
	mgr.Reload()
	all = mgr.GetAllChannels()
	if len(all) != 5 {
		t.Errorf("after reload: expected 5 channels, got %d", len(all))
	}
}

func TestManagerSelectChannelDefaultStrategy(t *testing.T) {
	mgr, cleanup := newTestManager(t)
	defer cleanup()
	addTestChannels(t, mgr)

	// With unknown strategy, should return first channel
	ch, err := mgr.SelectChannel(models.ChannelOpenAIChat, "unknown")
	if err != nil {
		t.Fatalf("SelectChannel() error = %v", err)
	}
	if ch.ID != 1 {
		t.Errorf("expected channel ID 1 with unknown strategy, got %d", ch.ID)
	}
}
