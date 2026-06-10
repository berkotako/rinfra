package service

import (
	"encoding/json"
	"fmt"

	"github.com/rinfra/rinfra/internal/cloud"
	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/secrets"
)

// MarshalCredentials JSON-encodes a provider Raw map into the byte slice
// stored as the credential plaintext. The wire / at-rest format is a JSON
// object of string→string, e.g. {"DIGITALOCEAN_TOKEN":"dop_v1_..."}.
func MarshalCredentials(values map[string]string) ([]byte, error) {
	b, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("marshal credentials: %w", err)
	}
	return b, nil
}

// DecryptCredentials decrypts a stored credential ciphertext and returns a
// cloud.Credentials ready for use. The plaintext must be a JSON object of
// string→string as produced by MarshalCredentials.
func DecryptCredentials(enc *secrets.Encrypter, provider domain.CloudProviderType, ct, nonce []byte, keyID string) (cloud.Credentials, error) {
	plaintext, err := enc.Decrypt(secrets.Envelope{Ciphertext: ct, Nonce: nonce, KeyID: keyID})
	if err != nil {
		return cloud.Credentials{}, fmt.Errorf("decrypt credentials for %s: %w", provider, err)
	}
	var raw map[string]string
	if err := json.Unmarshal(plaintext, &raw); err != nil {
		return cloud.Credentials{}, fmt.Errorf("parse credentials for %s: %w", provider, err)
	}
	return cloud.Credentials{Provider: provider, Raw: raw}, nil
}
