package domain

import "time"

// Session is an opaque bearer-token session. Only the SHA-256 hash of the raw
// token is stored; the raw token is returned to the client at login and never
// persisted.
type Session struct {
	TokenHash string // hex-encoded SHA-256 of the raw token
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}
