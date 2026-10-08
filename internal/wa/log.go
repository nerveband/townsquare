package wa

import (
	"fmt"
	"os"
	"strings"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// stderrLogger keeps whatsmeow logs off stdout so command output stays parseable.
type stderrLogger struct {
	mod string
	min int
}

var levels = map[string]int{"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3}

// Logger returns a whatsmeow logger that writes to stderr at or above level.
func Logger(module, level string) waLog.Logger {
	l, ok := levels[strings.ToUpper(level)]
	if !ok {
		l = 2
	}
	return &stderrLogger{mod: module, min: l}
}

func (s *stderrLogger) out(lvl int, name, msg string, args ...any) {
	if lvl < s.min {
		return
	}
	fmt.Fprintf(os.Stderr, "%s [%s %s] %s\n", time.Now().Format("15:04:05.000"), s.mod, name, fmt.Sprintf(msg, args...))
}

func (s *stderrLogger) Debugf(m string, a ...any) { s.out(0, "DEBUG", m, a...) }
func (s *stderrLogger) Infof(m string, a ...any)  { s.out(1, "INFO", m, a...) }
func (s *stderrLogger) Warnf(m string, a ...any)  { s.out(2, "WARN", m, a...) }
func (s *stderrLogger) Errorf(m string, a ...any) { s.out(3, "ERROR", m, a...) }
func (s *stderrLogger) Sub(mod string) waLog.Logger {
	return &stderrLogger{mod: s.mod + "/" + mod, min: s.min}
}
