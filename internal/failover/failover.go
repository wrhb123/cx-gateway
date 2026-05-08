package failover

import (
	"sync"
	"time"
)

// CircuitBreaker 实现熔断器模式
type CircuitBreaker struct {
	mu           sync.Mutex
	failureCount int
	successCount int
	state        string // closed, open, half-open
	lastFailure  time.Time
	threshold    int
	resetTimeout time.Duration
}

// NewCircuitBreaker 创建新的熔断器
func NewCircuitBreaker(threshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:        "closed",
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

// AllowRequest 检查是否允许发起请求
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "closed":
		return true
	case "open":
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	}
	return false
}

// RecordSuccess 记录一次成功的请求
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successCount++
	if cb.state == "half-open" {
		cb.state = "closed"
		cb.failureCount = 0
	}
}

// RecordFailure 记录一次失败的请求
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailure = time.Now()

	if cb.failureCount >= cb.threshold {
		cb.state = "open"
	}
}

// GetState 返回当前熔断器状态
func (cb *CircuitBreaker) GetState() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Reset 重置熔断器到初始状态
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = "closed"
	cb.failureCount = 0
	cb.successCount = 0
}

// FailoverManager 管理所有渠道的熔断器
type FailoverManager struct {
	mu            sync.RWMutex
	breakers      map[int64]*CircuitBreaker
	defaultConfig *CircuitBreaker
}

// NewFailoverManager 创建新的故障转移管理器
func NewFailoverManager(threshold int, resetSeconds int) *FailoverManager {
	return &FailoverManager{
		breakers:      make(map[int64]*CircuitBreaker),
		defaultConfig: NewCircuitBreaker(threshold, time.Duration(resetSeconds)*time.Second),
	}
}

// GetBreaker 获取指定渠道的熔断器
func (fm *FailoverManager) GetBreaker(channelID int64) *CircuitBreaker {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if cb, ok := fm.breakers[channelID]; ok {
		return cb
	}

	cb := NewCircuitBreaker(fm.defaultConfig.threshold, fm.defaultConfig.resetTimeout)
	fm.breakers[channelID] = cb
	return cb
}

// IsHealthy 检查渠道是否健康可用
func (fm *FailoverManager) IsHealthy(channelID int64) bool {
	cb := fm.GetBreaker(channelID)
	return cb.AllowRequest()
}

// RecordSuccess 记录渠道请求成功
func (fm *FailoverManager) RecordSuccess(channelID int64) {
	cb := fm.GetBreaker(channelID)
	cb.RecordSuccess()
}

// RecordFailure 记录渠道请求失败
func (fm *FailoverManager) RecordFailure(channelID int64) {
	cb := fm.GetBreaker(channelID)
	cb.RecordFailure()
}

// GetState 返回渠道熔断器的当前状态
func (fm *FailoverManager) GetState(channelID int64) string {
	cb := fm.GetBreaker(channelID)
	return cb.GetState()
}

// ResetState 重置渠道熔断器状态
func (fm *FailoverManager) ResetState(channelID int64) {
	cb := fm.GetBreaker(channelID)
	cb.Reset()
}
