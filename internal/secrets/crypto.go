package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const keySize = 32

var ErrNoKey = errors.New("secrets vault key missing")

// LoadOrCreateKey returns a 32-byte AES key from env or a file next to the DB.
func LoadOrCreateKey(dbPath, keyFile, rawEnv string) ([]byte, error) {
	if rawEnv != "" {
		return parseKey(rawEnv)
	}
	if keyFile == "" && dbPath != "" {
		keyFile = filepath.Join(filepath.Dir(dbPath), "secrets.key")
	}
	if keyFile == "" {
		return nil, ErrNoKey
	}
	if b, err := os.ReadFile(keyFile); err == nil {
		return parseKey(strings.TrimSpace(string(b)))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func parseKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrNoKey
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == keySize {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == keySize {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil && len(b) == keySize {
		return b, nil
	}
	return nil, fmt.Errorf("secrets key must be 32 bytes (hex or base64)")
}

func Seal(key []byte, plaintext string) (string, error) {
	if len(key) != keySize {
		return "", ErrNoKey
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
	out := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func Open(key []byte, packed string) (string, error) {
	if len(key) != keySize {
		return "", ErrNoKey
	}
	raw, err := base64.RawURLEncoding.DecodeString(packed)
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
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
