package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const keyringService = "knot-agent"
const keyringUser = "wrap-key"

// Seal encrypts plaintext with a machine-local wrapping key.
func Seal(dataDir string, plaintext []byte) (string, error) {
	key, err := wrappingKey(dataDir)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Open decrypts a sealed blob.
func Open(dataDir, sealed string) ([]byte, error) {
	key, err := wrappingKey(dataDir)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

func wrappingKey(dataDir string) ([]byte, error) {
	if secret, err := keyring.Get(keyringService, keyringUser); err == nil && secret != "" {
		b, err := base64.RawURLEncoding.DecodeString(secret)
		if err == nil && len(b) == 32 {
			return b, nil
		}
	}
	path := filepath.Join(dataDir, "wrap.key")
	if b, err := os.ReadFile(path); err == nil {
		key, err := base64.RawURLEncoding.DecodeString(string(b))
		if err == nil && len(key) == 32 {
			_ = keyring.Set(keyringService, keyringUser, string(b))
			return key, nil
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(key)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, err
	}
	_ = keyring.Set(keyringService, keyringUser, encoded)
	return key, nil
}
