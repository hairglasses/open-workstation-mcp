package slogcfg

import (
	"io"
	"log/slog"
	"os"
)

type Config struct {
	ServiceName  string
	Level        slog.Level
	Output       io.Writer
	JSON         bool
	AddSource    bool
	ExtraHandler func(slog.Handler) slog.Handler
}

func Init(cfg Config) *slog.Logger {
	out := cfg.Output
	if out == nil {
		out = os.Stderr
	}
	opts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: cfg.AddSource,
	}
	var h slog.Handler
	if cfg.JSON {
		h = slog.NewJSONHandler(out, opts)
	} else {
		h = slog.NewTextHandler(out, opts)
	}
	if cfg.ExtraHandler != nil {
		h = cfg.ExtraHandler(h)
	}
	logger := slog.New(h)
	if cfg.ServiceName != "" {
		logger = logger.With("service", cfg.ServiceName)
	}
	slog.SetDefault(logger)
	return logger
}
