package crypto

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJWTManagerLifecycle(t *testing.T) {
	// 1. Create a temp directory to hold generated RSA key pair files
	tmpDir, err := os.MkdirTemp("", "jwt_test_keys")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	privPath := filepath.Join(tmpDir, "app.rsa")
	pubPath := filepath.Join(tmpDir, "app.rsa.pub")

	// 2. Test auto keygen for 2048-bit RSA PEM keys
	err = EnsureRSAKeysExist(privPath, pubPath)
	if err != nil {
		t.Fatalf("EnsureRSAKeysExist returned error: %v", err)
	}

	// 3. Load generated private/public keys
	privKey, err := LoadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("LoadPrivateKey returned error: %v", err)
	}

	pubKey, err := LoadPublicKey(pubPath)
	if err != nil {
		t.Fatalf("LoadPublicKey returned error: %v", err)
	}

	// 4. Instantiate JWTManager and sign a test token
	jwtMgr := NewJWTManager(privKey, pubKey)

	expectedUserID := "user-uuid-12345"
	expectedTenantID := "tenant-uuid-abcde"
	expectedRole := "moderator"
	expectedDeviceID := "client-fingerprint-999"
	duration := 5 * time.Minute

	tokenStr, err := jwtMgr.GenerateAccessToken(
		expectedUserID,
		expectedTenantID,
		expectedRole,
		expectedDeviceID,
		duration,
	)
	if err != nil {
		t.Fatalf("Failed to generate access token: %v", err)
	}

	if tokenStr == "" {
		t.Errorf("Generated token is empty")
	}

	// 5. Verify the token signature and assert claims match expectations
	claims, err := jwtMgr.VerifyAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("Failed to verify access token: %v", err)
	}

	if claims.Subject != expectedUserID {
		t.Errorf("Expected Subject %s, got %s", expectedUserID, claims.Subject)
	}

	if claims.TenantID != expectedTenantID {
		t.Errorf("Expected TenantID %s, got %s", expectedTenantID, claims.TenantID)
	}

	if claims.Role != expectedRole {
		t.Errorf("Expected Role %s, got %s", expectedRole, claims.Role)
	}

	if claims.DeviceID != expectedDeviceID {
		t.Errorf("Expected DeviceID %s, got %s", expectedDeviceID, claims.DeviceID)
	}
}
