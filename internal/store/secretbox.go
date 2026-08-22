package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	secretKeySize = 32
	secretPrefix  = "enc:v1:"
)

type secretBox struct {
	gcm cipher.AEAD
}

func openSecretBox(dataDir string) (*secretBox, error) {
	keyPath := filepath.Join(dataDir, ".tagger-secrets.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, secretKeySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate provider secret key: %w", err)
		}
		file, createErr := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr == nil {
			if _, writeErr := file.Write(key); writeErr != nil {
				_ = file.Close()
				return nil, fmt.Errorf("write provider secret key: %w", writeErr)
			}
			if closeErr := file.Close(); closeErr != nil {
				return nil, fmt.Errorf("close provider secret key: %w", closeErr)
			}
		} else if !errors.Is(createErr, os.ErrExist) {
			return nil, fmt.Errorf("create provider secret key: %w", createErr)
		} else if key, err = os.ReadFile(keyPath); err != nil {
			return nil, fmt.Errorf("read provider secret key after race: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("read provider secret key: %w", err)
	}
	if len(key) != secretKeySize {
		return nil, fmt.Errorf("provider secret key must be %d bytes", secretKeySize)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return nil, fmt.Errorf("secure provider secret key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize provider secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize provider secret box: %w", err)
	}
	return &secretBox{gcm: gcm}, nil
}

func (box *secretBox) seal(payload []byte) (string, error) {
	if box == nil || box.gcm == nil {
		return "", errors.New("provider secret box is unavailable")
	}
	nonce := make([]byte, box.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate provider secret nonce: %w", err)
	}
	sealed := box.gcm.Seal(nonce, nonce, payload, nil)
	return secretPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (box *secretBox) open(value []byte) ([]byte, bool, error) {
	if len(value) < len(secretPrefix) || string(value[:len(secretPrefix)]) != secretPrefix {
		return append([]byte(nil), value...), false, nil
	}
	if box == nil || box.gcm == nil {
		return nil, true, errors.New("provider secret box is unavailable")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(string(value[len(secretPrefix):]))
	if err != nil {
		return nil, true, fmt.Errorf("decode provider secret payload: %w", err)
	}
	nonceSize := box.gcm.NonceSize()
	if len(sealed) < nonceSize {
		return nil, true, errors.New("provider secret payload is truncated")
	}
	plaintext, err := box.gcm.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return nil, true, fmt.Errorf("decrypt provider secret payload: %w", err)
	}
	return plaintext, true, nil
}
