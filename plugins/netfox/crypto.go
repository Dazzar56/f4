package netfox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"os/user"
	"strings"
)

const cryptoPrefix = "~ENC~"

// cryptoKeyOverride is used for testing key-dependency logic.
var cryptoKeyOverride []byte

// getCryptoKey derives a 32-byte AES key bound to the current machine and user.
func getCryptoKey() []byte {
	if cryptoKeyOverride != nil {
		return cryptoKeyOverride
	}
	host, _ := os.Hostname()
	username := ""
	if usr, err := user.Current(); err == nil && usr != nil {
		username = usr.Username
	}
	// Derive a stable 256-bit key
	hash := sha256.Sum256([]byte(host + ":" + username + ":f4-netfox"))
	return hash[:]
}

// obfuscate encrypts a string using AES-GCM and returns a base64 encoded result with a prefix.
func obfuscate(plain string) string {
	if plain == "" {
		return ""
	}
	block, err := aes.NewCipher(getCryptoKey())
	if err != nil {
		return plain // Fallback to plain on unexpected crypto error
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return plain
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return plain
	}
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plain), nil)
	return cryptoPrefix + base64.StdEncoding.EncodeToString(ciphertext)
}

// deobfuscate attempts to decrypt a prefixed base64 string using AES-GCM.
func deobfuscate(enc string) string {
	if !strings.HasPrefix(enc, cryptoPrefix) {
		return enc // Plaintext or unencrypted legacy data
	}
	encBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, cryptoPrefix))
	if err != nil {
		return "" // Corrupted base64
	}
	block, err := aes.NewCipher(getCryptoKey())
	if err != nil {
		return ""
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	nonceSize := aesGCM.NonceSize()
	if len(encBytes) < nonceSize {
		return "" // Truncated data
	}
	nonce, ciphertext := encBytes[:nonceSize], encBytes[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "" // Wrong key (different host/user) or corrupted ciphertext
	}
	return string(plaintext)
}
