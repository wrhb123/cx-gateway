package failover

import (
	"testing"
	"time"
)

func TestCircuitBreakerInitialState(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	if cb.GetState() != "closed" {
		t.Errorf("expected initial state 'closed', got '%s'", cb.GetState())
	}
}

func TestCircuitBreakerClosedAllowsRequests(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	if !cb.AllowRequest() {
		t.Error("closed circuit breaker should allow requests")
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.GetState() != "open" {
		t.Errorf("expected state 'open' after %d failures, got '%s'", 3, cb.GetState())
	}
}

func TestCircuitBreakerOpenBlocksRequests(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.AllowRequest() {
		t.Error("open circuit breaker should block requests")
	}
}

func TestCircuitBreakerHalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.GetState() != "open" {
		t.Fatalf("expected state 'open', got '%s'", cb.GetState())
	}
	time.Sleep(100 * time.Millisecond)
	if !cb.AllowRequest() {
		t.Error("circuit breaker should allow request after timeout")
	}
	if cb.GetState() != "half-open" {
		t.Errorf("expected state 'half-open', got '%s'", cb.GetState())
	}
}

func TestCircuitBreakerHalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	time.Sleep(100 * time.Millisecond)
	cb.AllowRequest()
	if cb.GetState() != "half-open" {
		t.Fatalf("expected state 'half-open', got '%s'", cb.GetState())
	}
	cb.RecordSuccess()
	if cb.GetState() != "closed" {
		t.Errorf("expected state 'closed' after success in half-open, got '%s'", cb.GetState())
	}
}

func TestCircuitBreakerHalfOpenToOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	time.Sleep(100 * time.Millisecond)
	cb.AllowRequest()
	cb.RecordFailure()
	if cb.GetState() != "open" {
		t.Errorf("expected state 'open' after failure in half-open, got '%s'", cb.GetState())
	}
}

func TestCircuitBreakerSuccessDoesNotChangeClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	cb.RecordSuccess()
	if cb.GetState() != "closed" {
		t.Errorf("expected state 'closed' after success, got '%s'", cb.GetState())
	}
}

func TestFailoverManagerGetBreaker(t *testing.T) {
	fm := NewFailoverManager(5, 60)
	cb1 := fm.GetBreaker(1)
	cb2 := fm.GetBreaker(1)
	if cb1 != cb2 {
		t.Error("GetBreaker should return same breaker for same channel ID")
	}
}

func TestFailoverManagerDifferentBreakers(t *testing.T) {
	fm := NewFailoverManager(5, 60)
	cb1 := fm.GetBreaker(1)
	cb2 := fm.GetBreaker(2)
	if cb1 == cb2 {
		t.Error("GetBreaker should return different breakers for different channel IDs")
	}
}

func TestFailoverManagerIsHealthy(t *testing.T) {
	fm := NewFailoverManager(3, 1)
	if !fm.IsHealthy(1) {
		t.Error("new channel should be healthy")
	}
}

func TestFailoverManagerRecordSuccess(t *testing.T) {
	fm := NewFailoverManager(3, 1)
	fm.RecordSuccess(1)
	if fm.GetState(1) != "closed" {
		t.Errorf("expected 'closed' after success, got '%s'", fm.GetState(1))
	}
}

func TestFailoverManagerRecordFailure(t *testing.T) {
	fm := NewFailoverManager(3, 1)
	for i := 0; i < 3; i++ {
		fm.RecordFailure(1)
	}
	if fm.GetState(1) != "open" {
		t.Errorf("expected 'open' after threshold failures, got '%s'", fm.GetState(1))
	}
	if fm.IsHealthy(1) {
		t.Error("channel should not be healthy when circuit is open")
	}
}
