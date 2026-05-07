package db

import (
	"os"
	"path/filepath"
	"testing"

	"ai-proxy-gateway/internal/models"
)

func newTestDB(t *testing.T) (*Database, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	cleanup := func() {
		database.Close()
		os.Remove(dbPath)
	}
	return database, cleanup
}

func TestNewDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer database.Close()
	if database.Conn == nil {
		t.Error("expected non-nil database connection")
	}
}

func TestCreateAndGetChannel(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	ch := &models.Channel{
		Name:       "Test Channel",
		Type:       models.ChannelOpenAIChat,
		BaseURL:    "https://api.openai.com",
		APIKey:     "sk-test123",
		Model:      "gpt-4o",
		Priority:   10,
		Weight:     1,
		Enabled:    true,
		MaxRetries: 3,
		Timeout:    120,
	}

	id, err := database.CreateChannel(ch)
	if err != nil {
		t.Fatalf("CreateChannel() error = %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero channel ID")
	}

	got, err := database.GetChannel(id)
	if err != nil {
		t.Fatalf("GetChannel() error = %v", err)
	}
	if got.Name != ch.Name {
		t.Errorf("Name = %q, want %q", got.Name, ch.Name)
	}
	if got.Type != ch.Type {
		t.Errorf("Type = %s, want %s", got.Type, ch.Type)
	}
	if got.Model != ch.Model {
		t.Errorf("Model = %q, want %q", got.Model, ch.Model)
	}
	if got.Priority != ch.Priority {
		t.Errorf("Priority = %d, want %d", got.Priority, ch.Priority)
	}
}

func TestUpdateChannel(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	ch := &models.Channel{
		Name:    "Old Name",
		Type:    models.ChannelOpenAIChat,
		BaseURL: "https://api.openai.com",
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Enabled: true,
	}
	id, _ := database.CreateChannel(ch)

	ch.ID = id
	ch.Name = "New Name"
	ch.Model = "gpt-4"
	if err := database.UpdateChannel(ch); err != nil {
		t.Fatalf("UpdateChannel() error = %v", err)
	}

	got, _ := database.GetChannel(id)
	if got.Name != "New Name" {
		t.Errorf("Name = %q, want %q", got.Name, "New Name")
	}
	if got.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", got.Model, "gpt-4")
	}
}

func TestDeleteChannel(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	ch := &models.Channel{
		Name:    "To Delete",
		Type:    models.ChannelOpenAIChat,
		BaseURL: "https://api.openai.com",
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Enabled: true,
	}
	id, _ := database.CreateChannel(ch)

	if err := database.DeleteChannel(id); err != nil {
		t.Fatalf("DeleteChannel() error = %v", err)
	}

	_, err := database.GetChannel(id)
	if err == nil {
		t.Error("expected error when getting deleted channel")
	}
}

func TestListChannels(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	channels := []models.Channel{
		{Name: "Channel A", Type: models.ChannelOpenAIChat, BaseURL: "https://a.com", APIKey: "key-a", Model: "gpt-4o", Priority: 5, Enabled: true},
		{Name: "Channel B", Type: models.ChannelClaude, BaseURL: "https://b.com", APIKey: "key-b", Model: "claude-3", Priority: 10, Enabled: true},
		{Name: "Channel C", Type: models.ChannelOpenAIChat, BaseURL: "https://c.com", APIKey: "key-c", Model: "gpt-3.5", Priority: 3, Enabled: false},
	}
	for _, ch := range channels {
		database.CreateChannel(&ch)
	}

	list, err := database.ListChannels()
	if err != nil {
		t.Fatalf("ListChannels() error = %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 channels, got %d", len(list))
	}
	// Verify priority ordering (descending)
	if list[0].Priority < list[1].Priority {
		t.Error("channels should be ordered by priority descending")
	}
}

func TestListEnabledChannelsByType(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	channels := []models.Channel{
		{Name: "OpenAI 1", Type: models.ChannelOpenAIChat, BaseURL: "https://a.com", APIKey: "key", Model: "gpt-4o", Enabled: true},
		{Name: "OpenAI 2", Type: models.ChannelOpenAIChat, BaseURL: "https://b.com", APIKey: "key", Model: "gpt-4", Enabled: false},
		{Name: "Claude 1", Type: models.ChannelClaude, BaseURL: "https://c.com", APIKey: "key", Model: "claude-3", Enabled: true},
	}
	for _, ch := range channels {
		database.CreateChannel(&ch)
	}

	list, err := database.ListEnabledChannelsByType(models.ChannelOpenAIChat)
	if err != nil {
		t.Fatalf("ListEnabledChannelsByType() error = %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 enabled OpenAI channel, got %d", len(list))
	}
	if list[0].Name != "OpenAI 1" {
		t.Errorf("expected 'OpenAI 1', got %q", list[0].Name)
	}
}

func TestCreateAndListRoutes(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	route := &models.ModelRoute{
		Pattern:     "gpt-*",
		ChannelIDs:  []int64{1, 2, 3},
		LoadBalance: "round_robin",
		Priority:    10,
		Enabled:     true,
	}

	id, err := database.CreateRoute(route)
	if err != nil {
		t.Fatalf("CreateRoute() error = %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero route ID")
	}

	routes, err := database.ListRoutes()
	if err != nil {
		t.Fatalf("ListRoutes() error = %v", err)
	}
	if len(routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Pattern != "gpt-*" {
		t.Errorf("Pattern = %q, want %q", routes[0].Pattern, "gpt-*")
	}
	if len(routes[0].ChannelIDs) != 3 {
		t.Errorf("expected 3 channel IDs, got %d", len(routes[0].ChannelIDs))
	}
}

func TestInitChannelStats(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	ch := &models.Channel{
		Name:    "Stats Test",
		Type:    models.ChannelOpenAIChat,
		BaseURL: "https://api.openai.com",
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Enabled: true,
	}
	id, _ := database.CreateChannel(ch)

	if err := database.InitChannelStats(id); err != nil {
		t.Fatalf("InitChannelStats() error = %v", err)
	}
}

func TestUpdateStats(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	ch := &models.Channel{
		Name:    "Stats Channel",
		Type:    models.ChannelOpenAIChat,
		BaseURL: "https://api.openai.com",
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Enabled: true,
	}
	id, _ := database.CreateChannel(ch)
	database.InitChannelStats(id)

	database.UpdateStats(id, true, 100, "")
	database.UpdateStats(id, false, 200, "timeout")

	var totalReqs, successCount, failureCount int64
	err := database.Conn.QueryRow("SELECT total_requests, success_count, failure_count FROM channel_stats WHERE channel_id=?", id).
		Scan(&totalReqs, &successCount, &failureCount)
	if err != nil {
		t.Fatalf("query stats error = %v", err)
	}
	if totalReqs != 2 {
		t.Errorf("total_requests = %d, want 2", totalReqs)
	}
	if successCount != 1 {
		t.Errorf("success_count = %d, want 1", successCount)
	}
	if failureCount != 1 {
		t.Errorf("failure_count = %d, want 1", failureCount)
	}
}

func TestLogRequest(t *testing.T) {
	database, cleanup := newTestDB(t)
	defer cleanup()

	log := &models.RequestLog{
		RequestID:   "req-001",
		Model:       "gpt-4o",
		ChannelID:   1,
		ChannelName: "Test Channel",
		Status:      200,
		Latency:     150,
	}

	if err := database.LogRequest(log); err != nil {
		t.Fatalf("LogRequest() error = %v", err)
	}

	var count int
	err := database.Conn.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&count)
	if err != nil {
		t.Fatalf("count query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 log entry, got %d", count)
	}
}

func TestParseIDs(t *testing.T) {
	tests := []struct {
		input string
		want  []int64
	}{
		{"", nil},
		{"1", []int64{1}},
		{"1,2,3", []int64{1, 2, 3}},
		{"10,20", []int64{10, 20}},
		{" 1 , 2 , 3 ", []int64{1, 2, 3}},
	}
	for _, tt := range tests {
		got := parseIDs(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseIDs(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseIDs(%q)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
