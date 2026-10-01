package pam

import (
	"errors"
	"os/user"
	"testing"
)

func TestCheckAccountInput(t *testing.T) {
	if err := CheckAccount(Service(), "", ""); !errors.Is(err, ErrAccount) {
		t.Fatalf("empty user: %v", err)
	}
	if err := CheckAccount(Service(), "a\x00b", ""); !errors.Is(err, ErrAccount) {
		t.Fatalf("NUL in user: %v", err)
	}
	if _, err := OpenSession(Service(), "", ""); !errors.Is(err, ErrAccount) {
		t.Fatalf("empty user: %v", err)
	}
}

// The account check works for the current user without root (the dev
// daemon re-checks its own user this way).
func TestCheckAccountCurrentUser(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	err = CheckAccount(Service(), u.Username, "")
	if errors.Is(err, ErrAccount) {
		t.Fatalf("PAM refuses the current user's account: %v", err)
	}
	if err != nil {
		t.Logf("PAM unavailable here: %v", err)
	}
}

func TestSessionCloseIdempotent(t *testing.T) {
	var s Session
	if err := s.Close(); err != nil || s.Env() != nil {
		t.Fatal("zero session")
	}
}
