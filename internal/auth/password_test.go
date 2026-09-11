package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	encoded, err := HashPassword("uma-senha-segura-123")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(encoded, "uma-senha-segura-123") {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword(encoded, "senha-incorreta") {
		t.Fatal("incorrect password verified")
	}
}

func TestPasswordMinimumLength(t *testing.T) {
	if _, err := HashPassword("curta"); err == nil {
		t.Fatal("expected short password to be rejected")
	}
}
