package crypto

import (
	"strings"
	"testing"
)

// TestArgon2PasswordHashing verifies the correctness of Argon2id password hashing and verification.
func TestArgon2PasswordHashing(t *testing.T) {
	password := "supersecret12345"

	// 1. Test hashing generation
	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	// Verify standard Argon2id prefix
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Errorf("Expected hash prefix '$argon2id$', got: '%s'", encoded)
	}

	// 2. Test positive match (Correct password)
	matched := ComparePassword(password, encoded)
	if !matched {
		t.Errorf("Expected correct password to match its own hash")
	}

	// 3. Test negative match (Wrong password)
	matchedWrong := ComparePassword("wrongpassword", encoded)
	if matchedWrong {
		t.Errorf("Expected wrong password to fail matching")
	}
}
