package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	installationTokenBytes = 32
	installationHashDomain = "ecctl-installation-v1\x00"
)

func activeInstallationHash(configPath string) string {
	if configPath == "" {
		return ""
	}
	token, err := loadOrCreateInstallationToken(context.Background(), configPath)
	if err != nil {
		return ""
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(installationHashDomain))
	_, _ = hash.Write(token)
	return hex.EncodeToString(hash.Sum(nil))
}

func newInstallationToken() ([]byte, error) {
	token := make([]byte, installationTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	return token, nil
}

func encodeInstallationToken(token []byte) ([]byte, error) {
	if len(token) != installationTokenBytes {
		return nil, errors.New("invalid installation token")
	}
	return []byte(hex.EncodeToString(token) + "\n"), nil
}

func decodeInstallationToken(raw []byte) ([]byte, error) {
	encoded := strings.TrimSpace(string(raw))
	if len(encoded) != installationTokenBytes*2 || encoded != strings.ToLower(encoded) {
		return nil, errors.New("invalid installation token")
	}
	token, err := hex.DecodeString(encoded)
	if err != nil || len(token) != installationTokenBytes {
		return nil, errors.New("invalid installation token")
	}
	return token, nil
}
