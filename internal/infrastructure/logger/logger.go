package logger

import (
	"fmt"
	"os"

	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger 创建日志器
func NewLogger(cfg config.LogConfig) (*zap.Logger, error) {
	// 解析日志级别
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	// 创建编码器配置
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 如果启用颜色
	if cfg.Colors {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// 创建编码器
	var encoder zapcore.Encoder
	if cfg.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// 创建输出
	writers := make([]zapcore.WriteSyncer, 0, len(cfg.Outputs))
	for _, output := range cfg.Outputs {
		writer, err := createWriter(output)
		if err != nil {
			return nil, fmt.Errorf("failed to create writer for %s: %w", output, err)
		}
		writers = append(writers, writer)
	}

	// 合并输出
	writeSyncer := zapcore.NewMultiWriteSyncer(writers...)

	// 创建核心
	core := zapcore.NewCore(encoder, writeSyncer, level)

	// 创建日志器
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger, nil
}

// parseLevel 解析日志级别
func parseLevel(levelStr string) (zapcore.Level, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		return zapcore.InfoLevel, fmt.Errorf("invalid log level: %s", levelStr)
	}
	return level, nil
}

// createWriter 创建写入器
func createWriter(output string) (zapcore.WriteSyncer, error) {
	switch output {
	case "stdout":
		return zapcore.AddSync(os.Stdout), nil
	case "stderr":
		return zapcore.AddSync(os.Stderr), nil
	default:
		// 文件输出
		file, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		return zapcore.AddSync(file), nil
	}
}
