package deploy

import (
	"context"
	"fmt"
	"sync"
)

// FakeRunner is an in-memory Runner that records commands and uploads for tests.
// It never opens a real SSH connection.
type FakeRunner struct {
	mu      sync.Mutex
	cmds    []string
	uploads map[string]string // remotePath -> content
	// ReturnError, if non-nil, is returned by every Run call.
	ReturnError error
}

// NewFakeRunner returns a new FakeRunner ready for use in tests.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{uploads: make(map[string]string)}
}

// Run records cmd and returns ("", ReturnError).
func (f *FakeRunner) Run(_ context.Context, cmd string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	return "", f.ReturnError
}

// Upload records the upload. Always succeeds unless ReturnError is set.
func (f *FakeRunner) Upload(_ context.Context, remotePath, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReturnError != nil {
		return fmt.Errorf("fake upload: %w", f.ReturnError)
	}
	f.uploads[remotePath] = content
	return nil
}

// Commands returns a snapshot of all commands passed to Run, in order.
func (f *FakeRunner) Commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.cmds))
	copy(out, f.cmds)
	return out
}

// Uploaded returns the content of the upload at remotePath, or ("", false) if
// no upload was made to that path.
func (f *FakeRunner) Uploaded(remotePath string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.uploads[remotePath]
	return c, ok
}

// AllUploads returns a snapshot of all uploads.
func (f *FakeRunner) AllUploads() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]string, len(f.uploads))
	for k, v := range f.uploads {
		out[k] = v
	}
	return out
}
