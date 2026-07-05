package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.Logger
}

type contextKey string

const loggerKey contextKey = "logger"

func New(level, folder string, isDev bool) (*Logger, error) {
	var zaplvl zap.AtomicLevel
	var err error
	if level == "" {
		zaplvl = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	} else {
		zaplvl, err = zap.ParseAtomicLevel(level)
		if err != nil {
			return nil, fmt.Errorf("parse log level: %w", err)
		}
	}

	var writeSyncer zapcore.WriteSyncer
	if folder != "" {
		if err := os.MkdirAll(folder, 0755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}

		logPath := filepath.Join(folder, "app.log")
		file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}

		writeSyncer = zapcore.NewMultiWriteSyncer(
			zapcore.AddSync(os.Stdout),
			zapcore.AddSync(file),
		)
	} else {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	var encoderConfig zapcore.EncoderConfig
	if isDev {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		encoderConfig = zap.NewProductionEncoderConfig()
		encoderConfig.EncodeTime = zapcore.EpochTimeEncoder
	}

	var encoder zapcore.Encoder
	if isDev {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	core := zapcore.NewCore(encoder, writeSyncer, zaplvl)
	zapLogger := zap.New(core, zap.AddCaller())

	return &Logger{zapLogger}, nil
}

func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(loggerKey).(*Logger); ok {
		return l
	}

	defaultLogger, _ := zap.NewProduction()
	return &Logger{defaultLogger}
}

func ToContext(ctx context.Context, log *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}

func (l *Logger) With(fields ...zap.Field) *Logger {
	return &Logger{l.Logger.With(fields...)}
}
