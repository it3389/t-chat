package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	LevelTrace LogLevel = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String 返回日志级别字符串
func (l LogLevel) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LogFormat 日志格式
type LogFormat int

const (
	FormatText LogFormat = iota
	FormatJSON
)

// LogEntry 日志条目
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     LogLevel               `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level          LogLevel  `json:"level"`
	Format         LogFormat `json:"format"`
	Output         string    `json:"output"`          // 输出文件路径，"stdout"表示标准输出
	MaxSize        int64     `json:"max_size"`        // 最大文件大小（字节）
	MaxFiles       int       `json:"max_files"`       // 最大文件数量
	Compress       bool      `json:"compress"`        // 是否压缩旧文件
	IncludeCaller  bool      `json:"include_caller"`  // 是否包含调用者信息
	TimestampFormat string   `json:"timestamp_format"` // 时间戳格式
}

// DefaultLogConfig 默认日志配置
func DefaultLogConfig() *LogConfig {
	return &LogConfig{
		Level:           LevelInfo,
		Format:          FormatText,
		Output:          "logs/app.log",
		MaxSize:         100 * 1024 * 1024, // 100MB
		MaxFiles:        10,
		Compress:        true,
		IncludeCaller:   true,
		TimestampFormat: "2006-01-02 15:04:05.000",
	}
}

// EnhancedLogger 增强日志记录器
type EnhancedLogger struct {
    mu       sync.RWMutex
    config   *LogConfig
    writer   io.Writer
    file     *os.File
    // 额外的调试日志文件，用于在输出到stdout时仍持久化DEBUG
    debugWriter io.Writer
    debugFile   *os.File
    currentSize int64
    fields   map[string]interface{}
    
    // 性能优化字段
    lastLogTime map[string]time.Time // 用于日志频率限制
    logCounter  map[string]int       // 日志计数器
    mu2         sync.RWMutex         // 保护性能优化字段的锁
}

// NewEnhancedLogger 创建增强日志记录器
func NewEnhancedLogger(config *LogConfig) (*EnhancedLogger, error) {
	if config == nil {
		config = DefaultLogConfig()
	}
	
	logger := &EnhancedLogger{
		config:      config,
		fields:      make(map[string]interface{}),
		lastLogTime: make(map[string]time.Time),
		logCounter:  make(map[string]int),
	}
	
	if err := logger.setupOutput(); err != nil {
		return nil, fmt.Errorf("设置日志输出失败: %v", err)
	}
	
	return logger, nil
}

// setupOutput 设置输出
func (el *EnhancedLogger) setupOutput() error {
	if el.config.Output == "stdout" {
		el.writer = os.Stdout
		return nil
	}
	
	// 确保日志目录存在
	dir := filepath.Dir(el.config.Output)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %v", err)
	}
	
	// 打开日志文件
	file, err := os.OpenFile(el.config.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %v", err)
	}
	
	el.file = file
	el.writer = file
	
	// 获取当前文件大小
	if stat, err := file.Stat(); err == nil {
		el.currentSize = stat.Size()
	}
	
	return nil
}

// WithField 添加字段
func (el *EnhancedLogger) WithField(key string, value interface{}) *EnhancedLogger {
	el.mu.Lock()
	defer el.mu.Unlock()
	
	newLogger := &EnhancedLogger{
		config:      el.config,
		writer:      el.writer,
		file:        el.file,
		currentSize: el.currentSize,
		fields:      make(map[string]interface{}),
	}
	
	// 复制现有字段
	for k, v := range el.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	
	return newLogger
}

// WithFields 添加多个字段
func (el *EnhancedLogger) WithFields(fields map[string]interface{}) *EnhancedLogger {
	el.mu.Lock()
	defer el.mu.Unlock()
	
	newLogger := &EnhancedLogger{
		config:      el.config,
		writer:      el.writer,
		file:        el.file,
		currentSize: el.currentSize,
		fields:      make(map[string]interface{}),
	}
	
	// 复制现有字段
	for k, v := range el.fields {
		newLogger.fields[k] = v
	}
	
	// 添加新字段
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	
	return newLogger
}

// WithError 添加错误字段
func (el *EnhancedLogger) WithError(err error) *EnhancedLogger {
	if err == nil {
		return el
	}
	return el.WithField("error", err.Error())
}

// Trace 记录跟踪日志
func (el *EnhancedLogger) Trace(message string, args ...interface{}) {
	el.log(LevelTrace, message, args...)
}

// Debug 记录调试日志（临时禁用频率限制）
func (el *EnhancedLogger) Debug(message string, args ...interface{}) {
	// 临时禁用频率限制以便调试消息处理问题
	// if el.shouldSkipLog("debug:"+message, 100*time.Millisecond) {
	//	return
	// }
	el.log(LevelDebug, message, args...)
}

// Info 记录信息日志（带频率限制）
func (el *EnhancedLogger) Info(message string, args ...interface{}) {
	// 对频繁的Info日志进行限制
	if el.shouldSkipLog("info:"+message, 200*time.Millisecond) {
		return
	}
	el.log(LevelInfo, message, args...)
}

// Warn 记录警告日志
func (el *EnhancedLogger) Warn(message string, args ...interface{}) {
	el.log(LevelWarn, message, args...)
}

// Error 记录错误日志
func (el *EnhancedLogger) Error(message string, args ...interface{}) {
	el.log(LevelError, message, args...)
}

// Fatal 记录致命错误日志
func (el *EnhancedLogger) Fatal(message string, args ...interface{}) {
	el.log(LevelFatal, message, args...)
	os.Exit(1)
}

// 日志缓冲池，减少内存分配
var logBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 1024)
	},
}

// log 记录日志（优化版本）
func (el *EnhancedLogger) log(level LogLevel, message string, args ...interface{}) {
	// 检查日志级别
	if level < el.config.Level {
		return
	}
	
	// 预先格式化消息，避免在锁内进行
	var formattedMessage string
	if len(args) > 0 {
		formattedMessage = fmt.Sprintf(message, args...)
	} else {
		formattedMessage = message
	}
	
	// 预先获取调用者信息（如果需要），避免在锁内调用runtime.Caller
	var caller string
	if el.config.IncludeCaller {
		caller = el.getCaller()
	}
	
	// 预先获取时间戳
	timestamp := time.Now()
	
	// 使用读锁复制字段，减少锁竞争
	el.mu.RLock()
	var fields map[string]interface{}
	if len(el.fields) > 0 {
		fields = make(map[string]interface{}, len(el.fields))
		for k, v := range el.fields {
			fields[k] = v
		}
	}
	el.mu.RUnlock()
	
	// 创建日志条目
	entry := &LogEntry{
		Timestamp: timestamp,
		Level:     level,
		Message:   formattedMessage,
		Fields:    fields,
		Caller:    caller,
	}
	
	// 使用缓冲池减少内存分配
	buf := logBufferPool.Get().([]byte)
	buf = buf[:0] // 重置长度但保留容量
	defer logBufferPool.Put(buf)
	
	// 格式化日志（在锁外进行）
	formattedLog := el.formatEntry(entry)
	buf = append(buf, formattedLog...)
	buf = append(buf, '\n')
	
    // 写入时使用写锁
    el.mu.Lock()
    // 当输出为stdout时，DEBUG级别不写入控制台，改写入独立文件
    targetWriter := el.writer
    if el.config.Output == "stdout" && level == LevelDebug {
        if el.debugWriter == nil || el.debugFile == nil {
            // 懒加载调试日志文件
            _ = os.MkdirAll("logs", 0755)
            df, err := os.OpenFile("logs/debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
            if err == nil {
                el.debugFile = df
                el.debugWriter = df
            } else {
                // 创建失败则直接跳过DEBUG的控制台输出
                el.mu.Unlock()
                return
            }
        }
        targetWriter = el.debugWriter
    }

    if _, err := targetWriter.Write(buf); err != nil {
        el.mu.Unlock()
        fmt.Fprintf(os.Stderr, "写入日志失败: %v\n", err)
        return
    }
	
	// 更新文件大小并检查是否需要轮转
	el.currentSize += int64(len(buf))
	if el.file != nil && el.currentSize > el.config.MaxSize {
		el.rotateLog()
	}
	el.mu.Unlock()
}

// formatEntry 格式化日志条目
func (el *EnhancedLogger) formatEntry(entry *LogEntry) string {
	switch el.config.Format {
	case FormatJSON:
		return el.formatJSON(entry)
	default:
		return el.formatText(entry)
	}
}

// 字符串构建器池，减少内存分配
var stringBuilderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// formatText 格式化文本日志（优化版本）
func (el *EnhancedLogger) formatText(entry *LogEntry) string {
	builder := stringBuilderPool.Get().(*strings.Builder)
	builder.Reset()
	defer stringBuilderPool.Put(builder)
	
	// 预估容量，减少扩容
	builder.Grow(256)
	
	// 时间戳
	builder.WriteString(entry.Timestamp.Format(el.config.TimestampFormat))
	builder.WriteString(" [")
	builder.WriteString(entry.Level.String())
	builder.WriteByte(']')
	
	// 调用者信息
	if entry.Caller != "" {
		builder.WriteString(" [")
		builder.WriteString(entry.Caller)
		builder.WriteByte(']')
	}
	
	// 消息
	builder.WriteByte(' ')
	builder.WriteString(entry.Message)
	
	// 字段
	if len(entry.Fields) > 0 {
		builder.WriteString(" [")
		first := true
		for k, v := range entry.Fields {
			if !first {
				builder.WriteString(", ")
			}
			builder.WriteString(k)
			builder.WriteByte('=')
			builder.WriteString(fmt.Sprintf("%v", v))
			first = false
		}
		builder.WriteByte(']')
	}
	
	return builder.String()
}

// formatJSON 格式化JSON日志
func (el *EnhancedLogger) formatJSON(entry *LogEntry) string {
	// 创建JSON对象
	jsonEntry := map[string]interface{}{
		"timestamp": entry.Timestamp.Format(time.RFC3339Nano),
		"level":     entry.Level.String(),
		"message":   entry.Message,
	}
	
	if entry.Caller != "" {
		jsonEntry["caller"] = entry.Caller
	}
	
	if entry.Error != "" {
		jsonEntry["error"] = entry.Error
	}
	
	// 添加字段
	for k, v := range entry.Fields {
		jsonEntry[k] = v
	}
	
	data, err := json.Marshal(jsonEntry)
	if err != nil {
		return fmt.Sprintf(`{"timestamp":"%s","level":"ERROR","message":"JSON序列化失败: %v"}`,
			time.Now().Format(time.RFC3339Nano), err)
	}
	
	return string(data)
}

// 调用者信息缓存，减少runtime.Caller调用
var callerCache = sync.Map{}

// getCaller 获取调用者信息（带缓存优化）
func (el *EnhancedLogger) getCaller() string {
	pc, _, _, ok := runtime.Caller(3) // 跳过log、具体级别方法、getCaller
	if !ok {
		return "unknown"
	}
	
	// 尝试从缓存获取
	if cached, found := callerCache.Load(pc); found {
		return cached.(string)
	}
	
	// 获取详细信息
	frame := runtime.FuncForPC(pc)
	if frame == nil {
		return "unknown"
	}
	
	file, line := frame.FileLine(pc)
	filename := filepath.Base(file)
	caller := filename + ":" + strconv.Itoa(line) // 避免fmt.Sprintf
	
	// 缓存结果
	callerCache.Store(pc, caller)
	return caller
}

// shouldSkipLog 检查是否应该跳过日志（频率限制）
func (el *EnhancedLogger) shouldSkipLog(key string, interval time.Duration) bool {
	el.mu2.Lock()
	defer el.mu2.Unlock()
	
	now := time.Now()
	lastTime, exists := el.lastLogTime[key]
	
	if !exists || now.Sub(lastTime) >= interval {
		el.lastLogTime[key] = now
		el.logCounter[key] = 0
		return false
	}
	
	// 增加计数器，但不输出日志
	el.logCounter[key]++
	return true
}

// rotateLog 轮转日志
func (el *EnhancedLogger) rotateLog() {
	if el.file == nil {
		return
	}
	
	// 关闭当前文件
	el.file.Close()
	
	// 重命名当前文件
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.%s", el.config.Output, timestamp)
	
	if err := os.Rename(el.config.Output, backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "重命名日志文件失败: %v\n", err)
		return
	}
	
	// 压缩备份文件（如果启用）
	if el.config.Compress {
		go el.compressFile(backupPath)
	}
	
	// 创建新文件
	if err := el.setupOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "创建新日志文件失败: %v\n", err)
		return
	}
	
	// 清理旧文件
	go el.cleanupOldFiles()
}

// compressFile 压缩文件
func (el *EnhancedLogger) compressFile(filepath string) {
	// 这里可以实现文件压缩逻辑
	// 为了简化，暂时跳过实际压缩
}

// cleanupOldFiles 清理旧文件
func (el *EnhancedLogger) cleanupOldFiles() {
	dir := filepath.Dir(el.config.Output)
	basename := filepath.Base(el.config.Output)
	
	files, err := filepath.Glob(filepath.Join(dir, basename+".*"))
	if err != nil {
		return
	}
	
	// 如果文件数量超过限制，删除最旧的文件
	if len(files) > el.config.MaxFiles {
		// 按修改时间排序
		type fileInfo struct {
			path    string
			modTime time.Time
		}
		
		var fileInfos []fileInfo
		for _, file := range files {
			if stat, err := os.Stat(file); err == nil {
				fileInfos = append(fileInfos, fileInfo{
					path:    file,
					modTime: stat.ModTime(),
				})
			}
		}
		
		// 删除最旧的文件
		for i := 0; i < len(fileInfos)-el.config.MaxFiles; i++ {
			os.Remove(fileInfos[i].path)
		}
	}
}

// SetLevel 设置日志级别
func (el *EnhancedLogger) SetLevel(level LogLevel) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.config.Level = level
}

// GetLevel 获取日志级别
func (el *EnhancedLogger) GetLevel() LogLevel {
	el.mu.RLock()
	defer el.mu.RUnlock()
	return el.config.Level
}

// Close 关闭日志记录器
func (el *EnhancedLogger) Close() error {
    el.mu.Lock()
    defer el.mu.Unlock()
    
    if el.file != nil {
        return el.file.Close()
    }
    if el.debugFile != nil {
        _ = el.debugFile.Close()
    }
    return nil
}

// 实现network.Logger接口

// Printf 实现 network.Logger 接口
func (el *EnhancedLogger) Printf(format string, args ...interface{}) {
	el.Info(format, args...)
}

// Debugf 实现 network.Logger 接口（临时禁用频率限制）
func (el *EnhancedLogger) Debugf(format string, args ...interface{}) {
	// 临时禁用频率限制以便调试消息处理问题
	// if el.shouldSkipLog("debugf:"+format, 100*time.Millisecond) {
	//	return
	// }
	el.Debug(format, args...)
}

// Infof 实现 network.Logger 接口（带频率限制）
func (el *EnhancedLogger) Infof(format string, args ...interface{}) {
	// 对频繁的Infof日志进行限制
	if el.shouldSkipLog("infof:"+format, 200*time.Millisecond) {
		return
	}
	el.Info(format, args...)
}

// Warnf 实现 network.Logger 接口
func (el *EnhancedLogger) Warnf(format string, args ...interface{}) {
	el.Warn(format, args...)
}

// Errorf 实现 network.Logger 接口
func (el *EnhancedLogger) Errorf(format string, args ...interface{}) {
	el.Error(format, args...)
}

// Println 实现 network.Logger 接口
func (el *EnhancedLogger) Println(args ...interface{}) {
	message := fmt.Sprintln(args...)
	// 移除末尾的换行符
	if len(message) > 0 && message[len(message)-1] == '\n' {
		message = message[:len(message)-1]
	}
	el.Info(message)
}