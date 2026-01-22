package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testTask/internal/config"
)

type Logger struct {
	runDir    string
	runID     string
	logFile   *os.File
	console   *zap.Logger
	file      *zap.Logger
	traceFile *os.File
}

func New(cfg *config.Config) (*Logger, error) {
	runID := time.Now().Format("20060102-150405")
	runDir := filepath.Join(cfg.RunDir, runID)

	if err := os.MkdirAll(runDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create run directory: %w", err)
	}

	logPath := filepath.Join(runDir, "run.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	tracePath := filepath.Join(runDir, "trace.json")
	traceFile, err := os.Create(tracePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace file: %w", err)
	}

	var level zapcore.Level
	switch cfg.Logging.Level {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	consoleEncoderConfig := zap.NewDevelopmentEncoderConfig()
	consoleEncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	consoleEncoder := zapcore.NewConsoleEncoder(consoleEncoderConfig)
	consoleCore := zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level)
	consoleLogger := zap.New(consoleCore, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	fileEncoderConfig := zap.NewProductionEncoderConfig()
	fileEncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	fileEncoder := zapcore.NewJSONEncoder(fileEncoderConfig)
	fileCore := zapcore.NewCore(fileEncoder, zapcore.AddSync(logFile), level)
	fileLogger := zap.New(fileCore, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	screensDir := filepath.Join(runDir, "screens")
	if err := os.MkdirAll(screensDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create screens directory: %w", err)
	}

	return &Logger{
		runDir:    runDir,
		runID:     runID,
		logFile:   logFile,
		console:   consoleLogger,
		file:      fileLogger,
		traceFile: traceFile,
	}, nil
}

func (l *Logger) Console() *zap.Logger {
	return l.console
}

func (l *Logger) File() *zap.Logger {
	return l.file
}

func (l *Logger) RunDir() string {
	return l.runDir
}

func (l *Logger) RunID() string {
	return l.runID
}

func (l *Logger) ScreensDir() string {
	return filepath.Join(l.runDir, "screens")
}

func (l *Logger) Trace(data interface{}) error {
	l.file.Info("trace", zap.Any("data", data))
	return nil
}

func (l *Logger) Close() error {
	var errs []error
	if l.console != nil {
		if err := l.console.Sync(); err != nil {
			errs = append(errs, err)
		}
	}
	if l.file != nil {
		if err := l.file.Sync(); err != nil {
			errs = append(errs, err)
		}
	}
	if l.logFile != nil {
		if err := l.logFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if l.traceFile != nil {
		if err := l.traceFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to close log files: %v", errs)
	}
	return nil
}
