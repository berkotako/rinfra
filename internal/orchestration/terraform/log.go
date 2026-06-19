package terraform

import (
	"io"
	"log/slog"
)

// logWriter forwards each Terraform CLI write as a structured log line.
type logWriter struct {
	log *slog.Logger
	tag string
}

func newLogWriter(log *slog.Logger, tag string) io.Writer {
	return &logWriter{log: log, tag: tag}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.log.Info(w.tag, "output", string(p))
	return len(p), nil
}
