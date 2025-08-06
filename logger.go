package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// LogLevel 日志级别
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// Logger 日志管理器
type Logger struct {
	level  LogLevel
	logger *log.Logger
	file   *os.File
}

// NewLogger 创建新的日志管理器
func NewLogger(levelStr string, logFile string) *Logger {
	var level LogLevel
	switch strings.ToLower(levelStr) {
	case "debug":
		level = DEBUG
	case "info":
		level = INFO
	case "warn":
		level = WARN
	case "error":
		level = ERROR
	default:
		level = INFO // 默认为INFO级别
	}
	
	var writer io.Writer = os.Stdout
	var file *os.File
	
	// 如果指定了日志文件路径
	if logFile != "" {
		// 创建日志文件目录
		dir := filepath.Dir(logFile)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("创建日志目录失败: %v，将使用控制台输出", err)
		} else {
			// 打开或创建日志文件
			f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				log.Printf("打开日志文件失败: %v，将使用控制台输出", err)
			} else {
				writer = f
				file = f
			}
		}
	}
	
	logger := log.New(writer, "", log.LstdFlags)
	
	return &Logger{
		level:  level,
		logger: logger,
		file:   file,
	}
}

// Debug 输出调试日志
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.level <= DEBUG {
		l.logger.Printf("[DEBUG] "+format, args...)
	}
}

// Info 输出信息日志
func (l *Logger) Info(format string, args ...interface{}) {
	if l.level <= INFO {
		l.logger.Printf("[INFO] "+format, args...)
	}
}

// Warn 输出警告日志
func (l *Logger) Warn(format string, args ...interface{}) {
	if l.level <= WARN {
		l.logger.Printf("[WARN] "+format, args...)
	}
}

// Error 输出错误日志
func (l *Logger) Error(format string, args ...interface{}) {
	if l.level <= ERROR {
		l.logger.Printf("[ERROR] "+format, args...)
	}
}

// Close 关闭日志文件
func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

// 全局日志实例
var logger *Logger

// InitLogger 初始化全局日志
func InitLogger(levelStr string, logFile string) {
	logger = NewLogger(levelStr, logFile)
}

// CloseLogger 关闭全局日志
func CloseLogger() {
	if logger != nil {
		logger.Close()
	}
}

// 便捷函数
func LogDebug(format string, args ...interface{}) {
	if logger != nil {
		logger.Debug(format, args...)
	}
}

func LogInfo(format string, args ...interface{}) {
	if logger != nil {
		logger.Info(format, args...)
	}
}

func LogWarn(format string, args ...interface{}) {
	if logger != nil {
		logger.Warn(format, args...)
	}
}

func LogError(format string, args ...interface{}) {
	if logger != nil {
		logger.Error(format, args...)
	}
}