package logs

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"thunder/config"
)

var defaultLogger *slog.Logger

var (
	reset   = "\033[0m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
)

type coloredText struct {
	color string
	text  string
}

type prettyHandler struct {
	opts slog.HandlerOptions
	w    io.Writer
}

func (h *prettyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &prettyHandler{opts: h.opts, w: h.w}
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	return &prettyHandler{opts: h.opts, w: h.w}
}

func (h *prettyHandler) Handle(ctx context.Context, r slog.Record) error {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")

	var source string
	if h.opts.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		file := f.File
		if idx := strings.Index(file, "agents-workflows"); idx != -1 {
			file = file[idx:]
		} else {
			parts := strings.Split(file, string(os.PathSeparator))
			if len(parts) > 2 {
				file = strings.Join(parts[len(parts)-2:], string(os.PathSeparator))
			}
		}
		source = fmt.Sprintf("%s:%d", file, f.Line)
	}

	var levelText coloredText
	switch r.Level {
	case slog.LevelDebug:
		levelText = coloredText{color: cyan, text: "DEBUG"}
	case slog.LevelInfo:
		levelText = coloredText{color: green, text: "INFO "}
	case slog.LevelWarn:
		levelText = coloredText{color: yellow, text: "WARN "}
	case slog.LevelError:
		levelText = coloredText{color: red, text: "ERROR"}
	default:
		levelText = coloredText{color: reset, text: r.Level.String()}
	}

	var sb strings.Builder
	sb.WriteString(levelText.color)
	sb.WriteString("[")
	sb.WriteString(timestamp)
	sb.WriteString("] ")
	sb.WriteString(levelText.text)
	sb.WriteString(reset)
	sb.WriteString(" ")
	sb.WriteString(r.Message)

	if source != "" {
		sb.WriteString(" ")
		sb.WriteString(magenta)
		sb.WriteString("(")
		sb.WriteString(source)
		sb.WriteString(")")
		sb.WriteString(reset)
	}

	r.Attrs(func(attr slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(blue)
		sb.WriteString(attr.Key)
		sb.WriteString(reset)
		sb.WriteString("=")
		sb.WriteString(formatValue(attr.Value))
		return true
	})

	sb.WriteString("\n")
	_, err := h.w.Write([]byte(sb.String()))
	return err
}

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'g', -1, 64)
	case slog.KindBool:
		return strconv.FormatBool(v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%+v", v.Any())
	}
}

func Init(c *config.LogConfig) {
	if c == nil {
		return
	}
	var level slog.Level
	switch c.GetLevel() {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{
		AddSource: c.GetAddSource(),
		Level:     level,
	}

	var handler slog.Handler
	output := c.Output
	if output == nil {
		output = os.Stdout
	}

	if c.GetFormat() == "json" {
		handler = slog.NewJSONHandler(output, opts)
	} else if c.GetFormat() == "pretty" {
		handler = &prettyHandler{opts: *opts, w: output}
	} else {
		handler = slog.NewTextHandler(output, opts)
	}

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)

	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo).Writer())
}

func Info(msg string, args ...any) {
	defaultLogger.Info(msg, args...)
}

func Error(msg string, args ...any) {
	defaultLogger.Error(msg, args...)
}

func Infof(format string, args ...any) {
	defaultLogger.Info(fmt.Sprintf(format, args...))
}

func Warnf(format string, args ...any) {
	defaultLogger.Warn(fmt.Sprintf(format, args...))
}

func Errorf(format string, args ...any) {
	defaultLogger.Error(fmt.Sprintf(format, args...))
}
