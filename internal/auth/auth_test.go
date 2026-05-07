package auth

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	mgr := New("admin", "secret", 24*time.Hour)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestLogin(t *testing.T) {
	mgr := New("admin", "secret", 24*time.Hour)

	token, err := mgr.Login("admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	mgr := New("admin", "secret", 24*time.Hour)

	_, err := mgr.Login("admin", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}

	_, err = mgr.Login("wrong", "secret")
	if err == nil {
		t.Fatal("expected error for wrong username")
	}
}

func TestValidate(t *testing.T) {
	mgr := New("admin", "secret", 24*time.Hour)

	token, err := mgr.Login("admin", "secret")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	sess, err := mgr.Validate(token)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if sess.Username != "admin" {
		t.Errorf("username = %q, want 'admin'", sess.Username)
	}
}

func TestValidateInvalidToken(t *testing.T) {
	mgr := New("admin", "secret", 24*time.Hour)

	_, err := mgr.Validate("invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestLogout(t *testing.T) {
	mgr := New("admin", "secret", 24*time.Hour)

	token, err := mgr.Login("admin", "secret")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	mgr.Logout(token)

	_, err = mgr.Validate(token)
	if err == nil {
		t.Fatal("expected error after logout")
	}
}

func TestSessionExpiry(t *testing.T) {
	mgr := New("admin", "secret", 50*time.Millisecond)

	token, err := mgr.Login("admin", "secret")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// Should be valid immediately
	_, err = mgr.Validate(token)
	if err != nil {
		t.Fatalf("session should be valid: %v", err)
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	_, err = mgr.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired session")
	}
}

func TestCleanupExpired(t *testing.T) {
	mgr := New("admin", "secret", 50*time.Millisecond)

	token, err := mgr.Login("admin", "secret")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	mgr.CleanupExpired()

	_, err = mgr.Validate(token)
	if err == nil {
		t.Fatal("expected error after cleanup of expired session")
	}
}

func TestChangePassword(t *testing.T) {
	mgr := New("admin", "oldpass", 24*time.Hour)

	err := mgr.ChangePassword("admin", "oldpass", "newpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Old password should fail
	_, err = mgr.Login("admin", "oldpass")
	if err == nil {
		t.Fatal("expected error for old password")
	}

	// New password should work
	_, err = mgr.Login("admin", "newpass")
	if err != nil {
		t.Fatalf("expected login with new password to work: %v", err)
	}
}

func TestChangePasswordInvalid(t *testing.T) {
	mgr := New("admin", "secret", 24*time.Hour)

	err := mgr.ChangePassword("admin", "wrong", "newpass")
	if err == nil {
		t.Fatal("expected error for wrong old password")
	}
}
