// Package secrets provides AES-256-GCM envelope encryption for credentials and
// license keys stored in the database, and redacting types that prevent secrets
// from appearing in logs or the audit trail.
//
// Envelope scheme: a random 32-byte data key is generated per secret, encrypted
// by the master key (from RINFRA_MASTER_KEY env var), and stored alongside the
// ciphertext. The master key never touches the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// ErrMasterKeyMissing is returned when RINFRA_MASTER_KEY is not set or invalid.
var ErrMasterKeyMissing = errors.New("RINFRA_MASTER_KEY must be a 32-byte base64-encoded value")

// ErrTampered is returned when decryption authentication fails, indicating the
// ciphertext or nonce has been altered.
var ErrTampered = errors.New("ciphertext authentication failed: data may have been tampered with")

// masterKeySize is the required length of the AES-256 master key in bytes.
const masterKeySize = 32

// Redacted is a string type that never reveals its value in logs or formatted
// output. Use it to wrap API keys, passwords, and connection strings.
type Redacted string

// String implements fmt.Stringer, returning a static placeholder.
func (r Redacted) String() string { return "[redacted]" }

// LogValue implements slog.LogValuer so structured log records never capture
// the underlying value.
func (r Redacted) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// Envelope holds the encrypted output produced by Encrypt.
type Envelope struct {
	// Ciphertext is the AES-256-GCM encrypted payload (data key + plaintext
	// encrypted by master key and data key respectively).
	Ciphertext []byte
	// Nonce is the random GCM nonce used during encryption. Store alongside
	// the ciphertext; required for decryption.
	Nonce []byte
	// KeyID is an opaque identifier for the master-key version used. It is a
	// truncated base64 SHA-256 fingerprint of the master key (non-reversible, no
	// raw key bytes); replaced by a KMS key ARN in a later phase.
	KeyID string
}

// Encrypter encrypts and decrypts secrets using the configured master key.
type Encrypter struct {
	masterKey []byte
	keyID     string
}

// NewFromEnv creates an Encrypter by reading RINFRA_MASTER_KEY from the
// environment. The value must be a standard base64-encoded 32-byte key.
// Returns ErrMasterKeyMissing if the variable is absent or the decoded length
// is wrong.
func NewFromEnv() (*Encrypter, error) {
	raw := os.Getenv("RINFRA_MASTER_KEY")
	if raw == "" {
		return nil, ErrMasterKeyMissing
	}
	return New(raw)
}

// New creates an Encrypter from a base64-encoded 32-byte master key string.
func New(masterKeyB64 string) (*Encrypter, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode: %s", ErrMasterKeyMissing, err)
	}
	if len(key) != masterKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, need %d", ErrMasterKeyMissing, len(key), masterKeySize)
	}
	// KeyID is a truncated base64 SHA-256 fingerprint of the master key — stable
	// and non-reversible, and crucially never exposes raw key bytes. Replaced by
	// a KMS key ARN in a later phase.
	sum := sha256.Sum256(key)
	keyID := base64.RawStdEncoding.EncodeToString(sum[:])[:16]
	return &Encrypter{masterKey: key, keyID: keyID}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM envelope encryption. A fresh
// random data key is generated for each call; the data key is then wrapped by
// the master key. Both the wrapped data key and the encrypted plaintext are
// stored in the returned Envelope.
//
// Layout of Ciphertext: [wrappedDataKeyNonce(12)] [wrappedDataKey(32+16)]
// [plaintextNonce(12)] [encryptedPlaintext(len+16)]
func (e *Encrypter) Encrypt(plaintext []byte) (Envelope, error) {
	// Generate a fresh random data key.
	dataKey := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, dataKey); err != nil {
		return Envelope{}, fmt.Errorf("generate data key: %w", err)
	}

	// Wrap the data key with the master key.
	wrappedKey, wrapNonce, err := gcmSeal(e.masterKey, dataKey)
	if err != nil {
		return Envelope{}, fmt.Errorf("wrap data key: %w", err)
	}

	// Encrypt the plaintext with the data key.
	encPlaintext, ptNonce, err := gcmSeal(dataKey, plaintext)
	if err != nil {
		return Envelope{}, fmt.Errorf("encrypt plaintext: %w", err)
	}

	// Pack into a single ciphertext blob: wrapNonce || wrappedKey || ptNonce || encPlaintext
	blob := make([]byte, 0, 12+len(wrappedKey)+12+len(encPlaintext))
	blob = append(blob, wrapNonce...)
	blob = append(blob, wrappedKey...)
	blob = append(blob, ptNonce...)
	blob = append(blob, encPlaintext...)

	// The Nonce field in the Envelope is the outer nonce (wrapNonce); it is
	// stored separately for the DB columns (nonce BYTEA).
	return Envelope{
		Ciphertext: blob,
		Nonce:      wrapNonce,
		KeyID:      e.keyID,
	}, nil
}

// Decrypt recovers the plaintext from an Envelope produced by Encrypt.
// Returns ErrTampered if authentication fails.
func (e *Encrypter) Decrypt(env Envelope) ([]byte, error) {
	blob := env.Ciphertext
	if len(blob) < 12+masterKeySize+aes.BlockSize+12+aes.BlockSize {
		return nil, fmt.Errorf("decrypt: %w: ciphertext too short", ErrTampered)
	}

	// Unpack: wrapNonce(12) | wrappedKey(32+16=48) | ptNonce(12) | encPlaintext
	const wrapNonceLen = 12
	const wrappedKeyLen = masterKeySize + 16 // ciphertext overhead for GCM
	const ptNonceLen = 12

	wrapNonce := blob[:wrapNonceLen]
	wrappedKey := blob[wrapNonceLen : wrapNonceLen+wrappedKeyLen]
	ptNonce := blob[wrapNonceLen+wrappedKeyLen : wrapNonceLen+wrappedKeyLen+ptNonceLen]
	encPlaintext := blob[wrapNonceLen+wrappedKeyLen+ptNonceLen:]

	// Unwrap the data key.
	dataKey, err := gcmOpen(e.masterKey, wrappedKey, wrapNonce)
	if err != nil {
		return nil, fmt.Errorf("unwrap data key: %w", ErrTampered)
	}

	// Decrypt the plaintext.
	plaintext, err := gcmOpen(dataKey, encPlaintext, ptNonce)
	if err != nil {
		return nil, fmt.Errorf("decrypt plaintext: %w", ErrTampered)
	}

	return plaintext, nil
}

// gcmSeal encrypts plaintext with key using AES-256-GCM and returns
// (ciphertext, nonce, error). A fresh random nonce is generated each call.
func gcmSeal(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// gcmOpen decrypts ciphertext with key and nonce using AES-256-GCM.
func gcmOpen(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plaintext, nil
}
