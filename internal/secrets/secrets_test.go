package secrets_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/secrets"
)

// newTestEncrypter returns an Encrypter with a deterministic test master key.
func newTestEncrypter(t *testing.T) *secrets.Encrypter {
	t.Helper()
	// 32 zero bytes, base64-encoded — fine for testing, never use in prod.
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	enc, err := secrets.New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return enc
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"short", "hello"},
		{"api-key", "AKIAIOSFODNN7EXAMPLE"},
		{"json", `{"access_key":"ak","secret":"sk"}`},
		{"unicode", "パスワード"},
	}

	enc := newTestEncrypter(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, err := enc.Encrypt([]byte(tc.plaintext))
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if len(env.Ciphertext) == 0 {
				t.Fatal("Ciphertext is empty")
			}
			if len(env.Nonce) == 0 {
				t.Fatal("Nonce is empty")
			}
			if env.KeyID == "" {
				t.Fatal("KeyID is empty")
			}

			got, err := enc.Decrypt(env)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if string(got) != tc.plaintext {
				t.Errorf("got %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestEncryptProducesUniqueCiphertexts(t *testing.T) {
	enc := newTestEncrypter(t)
	plain := []byte("same plaintext")

	env1, err := enc.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt 1: %v", err)
	}
	env2, err := enc.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt 2: %v", err)
	}

	// Different random data keys + nonces → different ciphertexts.
	if bytes.Equal(env1.Ciphertext, env2.Ciphertext) {
		t.Error("expected unique ciphertexts for each Encrypt call")
	}
}

func TestTamperDetection(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(env *secrets.Envelope)
	}{
		{
			"flip-ciphertext-middle-byte",
			func(env *secrets.Envelope) { env.Ciphertext[len(env.Ciphertext)/2] ^= 0xFF },
		},
		{
			// First byte is inside the outer wrapNonce embedded in the blob.
			"flip-ciphertext-first-byte",
			func(env *secrets.Envelope) { env.Ciphertext[0] ^= 0xFF },
		},
		{
			"truncate-ciphertext",
			func(env *secrets.Envelope) { env.Ciphertext = env.Ciphertext[:5] },
		},
		{
			// GCM authentication tag is at the tail; flipping it causes auth failure.
			"flip-ciphertext-last-byte",
			func(env *secrets.Envelope) {
				env.Ciphertext[len(env.Ciphertext)-1] ^= 0xFF
			},
		},
	}

	enc := newTestEncrypter(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, err := enc.Encrypt([]byte("secret value"))
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			tc.tamper(&env)
			_, err = enc.Decrypt(env)
			if err == nil {
				t.Fatal("expected error after tampering, got nil")
			}
			if !errors.Is(err, secrets.ErrTampered) {
				t.Errorf("expected ErrTampered, got: %v", err)
			}
		})
	}
}

func TestNewFromEnvMissingKey(t *testing.T) {
	t.Setenv("RINFRA_MASTER_KEY", "")
	_, err := secrets.NewFromEnv()
	if !errors.Is(err, secrets.ErrMasterKeyMissing) {
		t.Errorf("expected ErrMasterKeyMissing, got: %v", err)
	}
}

func TestNewInvalidBase64(t *testing.T) {
	_, err := secrets.New("not-valid-base64!!!")
	if !errors.Is(err, secrets.ErrMasterKeyMissing) {
		t.Errorf("expected ErrMasterKeyMissing, got: %v", err)
	}
}

func TestNewWrongKeyLength(t *testing.T) {
	// 16 bytes, not 32.
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := secrets.New(short)
	if !errors.Is(err, secrets.ErrMasterKeyMissing) {
		t.Errorf("expected ErrMasterKeyMissing, got: %v", err)
	}
}

func TestRedactedString(t *testing.T) {
	r := secrets.Redacted("super-secret")
	if r.String() != "[redacted]" {
		t.Errorf("String() = %q, want [redacted]", r.String())
	}
	// fmt.Sprint should also produce [redacted].
	formatted := r.String()
	if strings.Contains(formatted, "super-secret") {
		t.Error("secret value leaked through String()")
	}
}

func TestRedactedLogValue(t *testing.T) {
	r := secrets.Redacted("super-secret")
	lv := r.LogValue()
	if lv.String() != "[redacted]" {
		t.Errorf("LogValue = %q, want [redacted]", lv.String())
	}
}
