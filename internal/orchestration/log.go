package orchestration

import (
	"io"
	"log/slog"
)

// logWriter is an io.Writer that forwards each write as a structured log line.
type logWriter struct {
	log          *slog.Logger
	engagementID string
}

func newLogWriter(log *slog.Logger, engagementID string) io.Writer {
	return &logWriter{log: log, engagementID: engagementID}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.log.Info("pulumi", "engagement", w.engagementID, "output", string(p))
	return len(p), nil
}
