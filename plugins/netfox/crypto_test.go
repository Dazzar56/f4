package netfox

import (
	"testing"
)

func TestCrypto_RoundTrip(t *testing.T) {
	orig := "my-secret-password-123"
	enc := obfuscate(orig)
	if enc == orig {
		t.Error("obfuscate should return a modified string")
	}

	dec := deobfuscate(enc)
	if dec != orig {
		t.Errorf("deobfuscate failed: expected %q, got %q", orig, dec)
	}
}

func TestCrypto_EmptyAndPlaintext(t *testing.T) {
	// Empty string
	if obfuscate("") != "" {
		t.Error("obfuscate of empty string should be empty")
	}
	if deobfuscate("") != "" {
		t.Error("deobfuscate of empty string should be empty")
	}

	// Plaintext fallback (not prefixed with cryptoPrefix)
	plain := "not-encrypted-plaintext"
	if deobfuscate(plain) != plain {
		t.Error("deobfuscate should return original string if not prefixed with cryptoPrefix")
	}
}

func TestCrypto_KeyDependency(t *testing.T) {
	orig := "secret-message"
	enc := obfuscate(orig)

	// Override crypto key to simulate a different user/machine
	cryptoKeyOverride = []byte("different-stable-key-32-bytes!!!")
	defer func() { cryptoKeyOverride = nil }()

	dec := deobfuscate(enc)
	if dec == orig {
		t.Error("deobfuscate should fail to decrypt when key is changed")
	}
	if dec != "" {
		t.Errorf("expected empty string on decryption failure, got %q", dec)
	}
}

func TestCrypto_CorruptedInput(t *testing.T) {
	// Invalid base64 with correct prefix
	invalid := cryptoPrefix + "!!!"
	if deobfuscate(invalid) != "" {
		t.Error("deobfuscate should return empty string for invalid base64")
	}

	// Too short data (less than nonce size)
	short := cryptoPrefix + "aaaa"
	if deobfuscate(short) != "" {
		t.Error("deobfuscate should return empty string for data that is too short")
	}
}

func TestCrypto_GetCryptoKey(t *testing.T) {
	key := getCryptoKey()
	if len(key) != 32 {
		t.Errorf("Expected 32-byte key, got %d bytes", len(key))
	}
}