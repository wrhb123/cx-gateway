package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

// Session 表示一个已登录的管理会话
type Session struct {
	Token     string
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Manager 管理管理员会话
type Manager struct {
	mu         sync.RWMutex
	sessions   map[string]*Session
	adminUser  string
	adminHash  string // SHA-256 hex of password
	sessionTTL time.Duration
}

// New 创建认证管理器
func New(username, password string, ttl time.Duration) *Manager {
	if username == "" {
		username = "admin"
	}
	if password == "" {
		password = "admin"
	}
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	hash := sha256.Sum256([]byte(password))
	return &Manager{
		sessions:   make(map[string]*Session),
		adminUser:  username,
		adminHash:  hex.EncodeToString(hash[:]),
		sessionTTL: ttl,
	}
}

// Login 验证用户名密码，成功返回会话 token
func (m *Manager) Login(username, password string) (string, error) {
	pwHash := sha256.Sum256([]byte(password))
	pwHex := hex.EncodeToString(pwHash[:])

	if subtle.ConstantTimeCompare([]byte(username), []byte(m.adminUser)) != 1 ||
		subtle.ConstantTimeCompare([]byte(pwHex), []byte(m.adminHash)) != 1 {
		return "", ErrInvalidCredentials
	}

	token, err := generateToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	sess := &Session{
		Token:     token,
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(m.sessionTTL),
	}

	m.mu.Lock()
	m.sessions[token] = sess
	m.mu.Unlock()

	return token, nil
}

// Logout 注销指定会话
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// Validate 验证会话 token，返回会话信息
func (m *Manager) Validate(token string) (*Session, error) {
	m.mu.RLock()
	sess, ok := m.sessions[token]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrSessionNotFound
	}

	if time.Now().After(sess.ExpiresAt) {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		return nil, ErrSessionExpired
	}

	return sess, nil
}

// CleanupExpired 清理过期会话
func (m *Manager) CleanupExpired() {
	now := time.Now()
	m.mu.Lock()
	for token, sess := range m.sessions {
		if now.After(sess.ExpiresAt) {
			delete(m.sessions, token)
		}
	}
	m.mu.Unlock()
}

// ChangePassword 修改管理员密码
func (m *Manager) ChangePassword(username, oldPassword, newPassword string) error {
	oldHash := sha256.Sum256([]byte(oldPassword))
	oldHex := hex.EncodeToString(oldHash[:])

	if subtle.ConstantTimeCompare([]byte(username), []byte(m.adminUser)) != 1 ||
		subtle.ConstantTimeCompare([]byte(oldHex), []byte(m.adminHash)) != 1 {
		return ErrInvalidCredentials
	}

	newHash := sha256.Sum256([]byte(newPassword))
	m.mu.Lock()
	m.adminHash = hex.EncodeToString(newHash[:])
	m.mu.Unlock()

	return nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var (
	ErrInvalidCredentials = &AuthError{"invalid username or password"}
	ErrSessionNotFound    = &AuthError{"session not found"}
	ErrSessionExpired     = &AuthError{"session expired"}
)

// AuthError 认证错误
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}
