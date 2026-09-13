package observability

import (
	"log"
	"os"
)

type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type stdLogger struct {
	l *log.Logger
}

func NewLogger() Logger {
	return &stdLogger{l: log.New(os.Stderr, "[agentmem] ", log.LstdFlags|log.Lshortfile)}
}

func (s stdLogger) Debugf(f string, a ...any) { s.l.Printf("DEBUG "+f, a...) }
func (s stdLogger) Infof(f string, a ...any)  { s.l.Printf("INFO "+f, a...) }
func (s stdLogger) Warnf(f string, a ...any)  { s.l.Printf("WARN "+f, a...) }
func (s stdLogger) Errorf(f string, a ...any) { s.l.Printf("ERROR "+f, a...) }
