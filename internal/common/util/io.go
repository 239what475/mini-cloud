package util

import (
	"fmt"
	"io"
	"log/slog"
)

func Fprintf(writer io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(writer, format, args...)
}

func Fprint(writer io.Writer, args ...any) {
	_, _ = fmt.Fprint(writer, args...)
}

func Fprintln(writer io.Writer, args ...any) {
	_, _ = fmt.Fprintln(writer, args...)
}

func CloseAndLog(logger *slog.Logger, resource string, closer io.Closer, attrs ...any) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		args := make([]any, 0, len(attrs)+2)
		args = append(args, "error", err)
		args = append(args, attrs...)
		logger.Warn("close "+resource+" failed", args...)
	}
}
