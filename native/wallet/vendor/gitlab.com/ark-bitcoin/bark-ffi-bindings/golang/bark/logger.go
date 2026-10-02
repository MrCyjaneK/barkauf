package bark

import (
	"fmt"
	"os"
)

// PrintBarkLogger is a BarkLogger that forwards records to stderr.
//
// Useful as a default during development. For production, implement
// BarkLogger yourself to forward to your logging framework of choice
// (slog, zap, logrus, etc.).
type PrintBarkLogger struct{}

var _ BarkLogger = PrintBarkLogger{}

// Log implements the BarkLogger interface.
func (PrintBarkLogger) Log(level LogLevel, target string, message string) {
	fmt.Fprintf(os.Stderr, "[bark][%s][%s] %s\n", logLevelName(level), target, message)
}

func logLevelName(level LogLevel) string {
	switch level {
	case LogLevelError:
		return "ERROR"
	case LogLevelWarn:
		return "WARN"
	case LogLevelInfo:
		return "INFO"
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelTrace:
		return "TRACE"
	default:
		return "UNKNOWN"
	}
}

// BarkAttachPrintLogger installs a default PrintBarkLogger.
//
// Calling this more than once will return an error from SetLogger because
// the underlying bridge can only be installed once per process.
func BarkAttachPrintLogger(maxLevel LogLevel) (BarkLogger, error) {
	logger := PrintBarkLogger{}
	if err := SetLogger(logger, maxLevel); err != nil {
		return nil, err
	}
	return logger, nil
}
