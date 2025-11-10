package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// ParseLogLevel 解析日志级别字符串
func ParseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// Logger 日志记录
type Logger struct {
	file  *os.File
	log   *log.Logger
	level LogLevel
}

// NewLogger 创建新的日志记录
func NewLogger(filename string) *Logger {
	return NewLoggerWithLevel(filename, LevelInfo)
}

// NewLoggerWithLevel 创建指定级别的日志记录
func NewLoggerWithLevel(filename string, level LogLevel) *Logger {
	// 确保日志目录存在
	os.MkdirAll("logs", 0755)

	// 打开日志文件
	file, err := os.OpenFile("logs/"+filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal("无法打开日志文件:", err)
	}

	logger := &Logger{
		file:  file,
		log:   log.New(file, "", log.LstdFlags),
		level: level,
	}

	return logger
}

// SetLevel 设置日志级别
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel 获取日志级别
func (l *Logger) GetLevel() LogLevel {
	return l.level
}

// shouldLog 检查是否应该记录该级别的日志
func (l *Logger) shouldLog(level LogLevel) bool {
	return level >= l.level
}

// Info 记录信息日志
func (l *Logger) Info(message string, args ...interface{}) {
	if !l.shouldLog(LevelInfo) {
		return
	}
	msg := fmt.Sprintf("[INFO] "+message, args...)
	l.log.Println(msg)
	fmt.Println(msg)
}

// Error 记录错误日志
func (l *Logger) Error(message string, args ...interface{}) {
	if !l.shouldLog(LevelError) {
		return
	}
	msg := fmt.Sprintf("[ERROR] "+message, args...)
	l.log.Println(msg)
	fmt.Println(msg)
}

// Debug 记录调试日志
func (l *Logger) Debug(message string, args ...interface{}) {
	if !l.shouldLog(LevelDebug) {
		return
	}
	msg := fmt.Sprintf("[DEBUG] "+message, args...)
	l.log.Println(msg)
}

// Warn 记录警告日志
func (l *Logger) Warn(message string, args ...interface{}) {
	if !l.shouldLog(LevelWarn) {
		return
	}
	msg := fmt.Sprintf("[WARN] "+message, args...)
	l.log.Println(msg)
	fmt.Println(msg)
}

// Printf 实现 network.Logger 接口
func (l *Logger) Printf(format string, args ...interface{}) {
	if !l.shouldLog(LevelInfo) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	l.log.Println(msg)
	fmt.Println(msg)
}

// Debugf 实现 network.Logger 接口
func (l *Logger) Debugf(format string, args ...interface{}) {
	if !l.shouldLog(LevelDebug) {
		return
	}
	msg := fmt.Sprintf("[DEBUG] "+format, args...)
	l.log.Println(msg)
}

// Infof 实现 network.Logger 接口
func (l *Logger) Infof(format string, args ...interface{}) {
	if !l.shouldLog(LevelInfo) {
		return
	}
	msg := fmt.Sprintf("[INFO] "+format, args...)
	l.log.Println(msg)
	fmt.Println(msg)
}

// Warnf 实现 network.Logger 接口
func (l *Logger) Warnf(format string, args ...interface{}) {
	if !l.shouldLog(LevelWarn) {
		return
	}
	msg := fmt.Sprintf("[WARN] "+format, args...)
	l.log.Println(msg)
	fmt.Println(msg)
}

// Errorf 实现 network.Logger 接口
func (l *Logger) Errorf(format string, args ...interface{}) {
	if !l.shouldLog(LevelError) {
		return
	}
	msg := fmt.Sprintf("[ERROR] "+format, args...)
	l.log.Println(msg)
	fmt.Println(msg)
}

// Println 实现 network.Logger 接口
func (l *Logger) Println(args ...interface{}) {
	if !l.shouldLog(LevelInfo) {
		return
	}
	msg := fmt.Sprintln(args...)
	l.log.Print(msg)
	fmt.Print(msg)
}

// Close 关闭日志记录
func (l *Logger) Close() error {
	return l.file.Close()
}
