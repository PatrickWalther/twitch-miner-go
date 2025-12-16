package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/patrickdappollonio/twitch-miner/internal/config"
)

type Logger struct {
	file    *os.File
	handler slog.Handler
}

func Setup(username string, settings config.LoggerSettings) (*Logger, error) {
	consoleLevel := parseLevel(settings.ConsoleLevel)
	fileLevel := parseLevel(settings.FileLevel)

	var writers []io.Writer
	writers = append(writers, os.Stdout)

	l := &Logger{}

	if settings.Save {
		if err := os.MkdirAll("logs", 0755); err != nil {
			return nil, err
		}

		logPath := filepath.Join("logs", username+".log")

		if settings.AutoClear {
			clearOldLogs(logPath, 7)
		}

		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		l.file = file
		writers = append(writers, file)
	}

	multiWriter := io.MultiWriter(writers...)

	level := consoleLevel
	if settings.Save && fileLevel < consoleLevel {
		level = fileLevel
	}

	handler := slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
		Level: level,
	})

	l.handler = handler
	slog.SetDefault(slog.New(handler))

	return l, nil
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

func parseLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func clearOldLogs(logPath string, daysToKeep int) {
	info, err := os.Stat(logPath)
	if err != nil {
		return
	}

	if time.Since(info.ModTime()) > time.Duration(daysToKeep)*24*time.Hour {
		os.Remove(logPath)
	}
}
