package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// LoadPrivateKey loads an RSA private key from the specified PEM file path.
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	keyBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("invalid private key pem format")
	}

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return privKey, nil
}

// LoadPublicKey loads an RSA public key from the specified PEM file path.
func LoadPublicKey(path string) (*rsa.PublicKey, error) {
	keyBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key file: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("invalid public key pem format")
	}

	pubKeyInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	pubKey, ok := pubKeyInterface.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not of type rsa")
	}

	return pubKey, nil
}

// EnsureRSAKeysExist checks for key existence, and automatically generates a new 2048-bit RSA PEM key pair if missing.
func EnsureRSAKeysExist(privatePath, publicPath string) error {
	// 1. Check if the private key file already exists
	if _, err := os.Stat(privatePath); err == nil {
		return nil // Key already exists, skip generation
	}

	fmt.Printf("-> RSA keypair not found. Automatically generating a new 2048-bit keypair at %s...\n", privatePath)

	// Create parent directories if they do not exist
	privDir := filepath.Dir(privatePath)
	pubDir := filepath.Dir(publicPath)
	if err := os.MkdirAll(privDir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for private key: %w", err)
	}
	if err := os.MkdirAll(pubDir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for public key: %w", err)
	}

	// 2. Generate a new 2048-bit RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate RSA key pair: %w", err)
	}

	// PEM encode the private key
	privBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privFile, err := os.OpenFile(privatePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open private key file for writing: %w", err)
	}
	defer privFile.Close()

	if err := pem.Encode(privFile, privBlock); err != nil {
		return fmt.Errorf("failed to encode private key to pem: %w", err)
	}

	// 3. Extract and PEM encode the public key
	publicKey := &privateKey.PublicKey
	pubASN1, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}

	pubBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubASN1,
	}
	pubFile, err := os.OpenFile(publicPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open public key file for writing: %w", err)
	}
	defer pubFile.Close()

	if err := pem.Encode(pubFile, pubBlock); err != nil {
		return fmt.Errorf("failed to encode public key to pem: %w", err)
	}

	fmt.Println("-> Successfully generated and encoded RSA RS256 keypair!")
	return nil
}
