package session

import (
	"encoding/json"
	"sync"
	"time"
)

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Session represents a conversation session
type Session struct {
	ResponseID string    `json:"response_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Messages   []Message `json:"messages"`
	Model      string    `json:"model"`
}

// Manager manages conversation sessions
type Manager struct {
	mu      sync.RWMutex
	sessions map[string]*Session
	ttl     time.Duration
}

// New creates a new session manager
func New(ttl time.Duration) *Manager {
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	m := &Manager{
		sessions: make(map[string]*Session),
		ttl:     ttl,
	}

	// Start cleanup goroutine
	go m.cleanupLoop()
	return m
}

// CreateSession creates a new session and returns the response_id
func (m *Manager) CreateSession(model string, messages []Message) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	responseID := generateResponseID()
	now := time.Now()

	m.sessions[responseID] = &Session{
		ResponseID: responseID,
		CreatedAt:  now,
		UpdatedAt:  now,
		Messages:   messages,
		Model:      model,
	}

	return responseID
}

// GetSession retrieves a session by response_id
func (m *Manager) GetSession(responseID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[responseID]
	if !ok {
		return nil, false
	}

	// Check TTL
	if time.Since(sess.UpdatedAt) > m.ttl {
		return nil, false
	}

	return sess, true
}

// AddMessage adds a message to an existing session
func (m *Manager) AddMessage(responseID string, msg Message) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[responseID]
	if !ok {
		return false
	}

	sess.Messages = append(sess.Messages, msg)
	sess.UpdatedAt = time.Now()
	return true
}

// UpdateSession updates the session with new messages and model
func (m *Manager) UpdateSession(responseID string, messages []Message, model string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[responseID]
	if !ok {
		return false
	}

	sess.Messages = messages
	sess.Model = model
	sess.UpdatedAt = time.Now()
	return true
}

// DeleteSession deletes a session
func (m *Manager) DeleteSession(responseID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, responseID)
}

// ListSessions returns all active sessions
func (m *Manager) ListSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	now := time.Now()

	for _, sess := range m.sessions {
		if now.Sub(sess.UpdatedAt) <= m.ttl {
			sessions = append(sessions, sess)
		}
	}

	return sessions
}

// cleanupLoop periodically cleans up expired sessions
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanupExpired()
	}
}

// cleanupExpired removes all expired sessions
func (m *Manager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, sess := range m.sessions {
		if now.Sub(sess.UpdatedAt) > m.ttl {
			delete(m.sessions, id)
		}
	}
}

// generateResponseID generates a unique response ID
func generateResponseID() string {
	return "resp_" + time.Now().Format("20060102150405") + "_" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}

// MarshalJSON returns the JSON representation of a session (without internal fields)
func (s *Session) MarshalJSON() ([]byte, error) {
	type Alias Session
	return json.Marshal(&struct {
		*Alias
		MessageCount int `json:"message_count"`
	}{
		Alias:        (*Alias)(s),
		MessageCount: len(s.Messages),
	})
}
