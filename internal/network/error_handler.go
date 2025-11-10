package network

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrorCategory 错误分类
type ErrorCategory string

const (
	ErrorCategoryNetwork     ErrorCategory = "network"
	ErrorCategoryTimeout     ErrorCategory = "timeout"
	ErrorCategoryProtocol    ErrorCategory = "protocol"
	ErrorCategoryValidation  ErrorCategory = "validation"
	ErrorCategoryPermission  ErrorCategory = "permission"
	ErrorCategoryResource    ErrorCategory = "resource"
	ErrorCategorySystem      ErrorCategory = "system"
	ErrorCategoryMessage     ErrorCategory = "message"
	ErrorCategoryUnknown     ErrorCategory = "unknown"
)

// ErrorSeverity 错误严重程度
type ErrorSeverity int

const (
	SeverityLow ErrorSeverity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// ErrorAction 错误处理动作
type ErrorAction string

const (
	ActionRetry     ErrorAction = "retry"
	ActionFallback  ErrorAction = "fallback"
	ActionIgnore    ErrorAction = "ignore"
	ActionEscalate  ErrorAction = "escalate"
	ActionTerminate ErrorAction = "terminate"
)

// DetailedError 详细错误信息
type DetailedError struct {
	Code        int           `json:"code"`
	Message     string        `json:"message"`
	Category    ErrorCategory `json:"category"`
	Severity    ErrorSeverity `json:"severity"`
	Cause       error         `json:"cause,omitempty"`
	Context     string        `json:"context"`
	Timestamp   time.Time     `json:"timestamp"`
	RetryCount  int           `json:"retry_count"`
	MaxRetries  int           `json:"max_retries"`
	RetryAfter  time.Duration `json:"retry_after"`
	Action      ErrorAction   `json:"action"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func (e *DetailedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%d] %s: %v (context: %s)", e.Category, e.Code, e.Message, e.Cause, e.Context)
	}
	return fmt.Sprintf("[%s:%d] %s (context: %s)", e.Category, e.Code, e.Message, e.Context)
}

// IsRetryable 判断错误是否可重试
func (e *DetailedError) IsRetryable() bool {
	return e.Action == ActionRetry && e.RetryCount < e.MaxRetries
}

// ShouldEscalate 判断是否需要升级处理
func (e *DetailedError) ShouldEscalate() bool {
	return e.Severity >= SeverityHigh || e.Action == ActionEscalate
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries    int           `json:"max_retries"`
	BaseDelay     time.Duration `json:"base_delay"`
	MaxDelay      time.Duration `json:"max_delay"`
	BackoffFactor float64       `json:"backoff_factor"`
	Jitter        bool          `json:"jitter"`
}

// DefaultRetryPolicy 默认重试策略
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:    3,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
}

// CalculateDelay 计算重试延迟
func (rp *RetryPolicy) CalculateDelay(attempt int) time.Duration {
	delay := time.Duration(float64(rp.BaseDelay) * float64(attempt) * rp.BackoffFactor)
	if delay > rp.MaxDelay {
		delay = rp.MaxDelay
	}
	
	// 添加抖动以避免雷群效应
	if rp.Jitter {
		jitter := time.Duration(float64(delay) * 0.1)
		delay += time.Duration(float64(jitter) * (2.0*float64(time.Now().UnixNano()%1000)/1000.0 - 1.0))
	}
	
	return delay
}

// ErrorHandler 错误处理器
type ErrorHandler struct {
	mu           sync.RWMutex
	logger       Logger
	retryPolicy  *RetryPolicy
	errorStats   map[ErrorCategory]*ErrorStats
	errorHistory []DetailedError
	maxHistory   int
	ctx          context.Context
	cancel       context.CancelFunc
}

// ErrorStats 错误统计
type ErrorStats struct {
	TotalCount    int64     `json:"total_count"`
	RecentCount   int64     `json:"recent_count"`
	LastOccurred  time.Time `json:"last_occurred"`
	FirstOccurred time.Time `json:"first_occurred"`
	RetryCount    int64     `json:"retry_count"`
	SuccessCount  int64     `json:"success_count"`
}

// NewErrorHandler 创建错误处理器
func NewErrorHandler(logger Logger) *ErrorHandler {
	ctx, cancel := context.WithCancel(context.Background())
	
	eh := &ErrorHandler{
		logger:       logger,
		retryPolicy:  DefaultRetryPolicy(),
		errorStats:   make(map[ErrorCategory]*ErrorStats),
		errorHistory: make([]DetailedError, 0),
		maxHistory:   1000,
		ctx:          ctx,
		cancel:       cancel,
	}
	
	// 启动统计清理协程
	go eh.statsCleanupLoop()
	
	return eh
}

// HandleError 处理错误
func (eh *ErrorHandler) HandleError(err error, context string, metadata map[string]interface{}) *DetailedError {
	if err == nil {
		return nil
	}
	
	// 如果已经是DetailedError，直接处理
	if detailedErr, ok := err.(*DetailedError); ok {
		eh.processError(detailedErr)
		return detailedErr
	}
	
	// 创建新的DetailedError
	detailedErr := eh.createDetailedError(err, context, metadata)
	eh.processError(detailedErr)
	
	return detailedErr
}

// createDetailedError 创建详细错误
func (eh *ErrorHandler) createDetailedError(err error, context string, metadata map[string]interface{}) *DetailedError {
	category, code := eh.categorizeError(err)
	severity := eh.determineSeverity(category, err)
	action := eh.determineAction(category, severity)
	
	return &DetailedError{
		Code:       code,
		Message:    err.Error(),
		Category:   category,
		Severity:   severity,
		Cause:      err,
		Context:    context,
		Timestamp:  time.Now(),
		RetryCount: 0,
		MaxRetries: eh.retryPolicy.MaxRetries,
		RetryAfter: eh.retryPolicy.BaseDelay,
		Action:     action,
		Metadata:   metadata,
	}
}

// categorizeError 错误分类
func (eh *ErrorHandler) categorizeError(err error) (ErrorCategory, int) {
	errorMsg := err.Error()
	
	// EOF和连接断开错误 - 优先处理
	if contains(errorMsg, []string{"EOF", "io.ReadFull Initial: EOF", "connection reset", "broken pipe", "forcibly closed"}) {
		return ErrorCategoryNetwork, ErrCodeConnectionFailed
	}
	
	// 网络相关错误
	if contains(errorMsg, []string{"connection", "network", "dial", "connect"}) {
		return ErrorCategoryNetwork, ErrCodeConnectionFailed
	}
	
	// 超时错误
	if contains(errorMsg, []string{"timeout", "deadline", "context deadline exceeded"}) {
		return ErrorCategoryTimeout, ErrCodeTimeout
	}
	
	// 协议错误
	if contains(errorMsg, []string{"protocol", "invalid message", "decode", "encode"}) {
		return ErrorCategoryProtocol, ErrCodeInvalidMessage
	}
	
	// 验证错误
	if contains(errorMsg, []string{"validation", "invalid", "malformed"}) {
		return ErrorCategoryValidation, ErrCodeInvalidMessage
	}
	
	// 权限错误
	if contains(errorMsg, []string{"permission", "unauthorized", "forbidden"}) {
		return ErrorCategoryPermission, ErrCodeServiceNotFound
	}
	
	// 资源错误
	if contains(errorMsg, []string{"resource", "memory", "disk", "file"}) {
		return ErrorCategoryResource, ErrCodeServiceNotFound
	}
	
	return ErrorCategoryUnknown, ErrCodeServiceNotFound
}

// determineSeverity 确定错误严重程度
func (eh *ErrorHandler) determineSeverity(category ErrorCategory, err error) ErrorSeverity {
	switch category {
	case ErrorCategoryNetwork:
		return SeverityMedium
	case ErrorCategoryTimeout:
		return SeverityLow
	case ErrorCategoryProtocol:
		return SeverityHigh
	case ErrorCategoryValidation:
		return SeverityMedium
	case ErrorCategoryPermission:
		return SeverityHigh
	case ErrorCategoryResource:
		return SeverityCritical
	case ErrorCategorySystem:
		return SeverityCritical
	default:
		return SeverityMedium
	}
}

// determineAction 确定处理动作
func (eh *ErrorHandler) determineAction(category ErrorCategory, severity ErrorSeverity) ErrorAction {
	switch {
	case severity >= SeverityCritical:
		return ActionEscalate
	case category == ErrorCategoryNetwork || category == ErrorCategoryTimeout:
		return ActionRetry
	case category == ErrorCategoryProtocol:
		return ActionFallback
	case severity <= SeverityLow:
		return ActionIgnore
	default:
		return ActionRetry
	}
}

// processError 处理错误
func (eh *ErrorHandler) processError(err *DetailedError) {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	
	// 更新统计信息
	eh.updateStats(err)
	
	// 记录错误历史
	eh.addToHistory(err)
	
	// 记录日志
	eh.logError(err)
}

// updateStats 更新错误统计
func (eh *ErrorHandler) updateStats(err *DetailedError) {
	stats, exists := eh.errorStats[err.Category]
	if !exists {
		stats = &ErrorStats{
			FirstOccurred: err.Timestamp,
		}
		eh.errorStats[err.Category] = stats
	}
	
	stats.TotalCount++
	stats.RecentCount++
	stats.LastOccurred = err.Timestamp
	
	if err.RetryCount > 0 {
		stats.RetryCount++
	}
}

// addToHistory 添加到错误历史
func (eh *ErrorHandler) addToHistory(err *DetailedError) {
	eh.errorHistory = append(eh.errorHistory, *err)
	
	// 限制历史记录数量
	if len(eh.errorHistory) > eh.maxHistory {
		eh.errorHistory = eh.errorHistory[len(eh.errorHistory)-eh.maxHistory:]
	}
}

// logError 记录错误日志
func (eh *ErrorHandler) logError(err *DetailedError) {
	logLevel := eh.getLogLevel(err.Severity)
	message := fmt.Sprintf("错误处理 - 类别: %s, 严重程度: %d, 动作: %s, 消息: %s",
		err.Category, err.Severity, err.Action, err.Message)
	
	switch logLevel {
	case "DEBUG":
		// Debug message
	case "INFO":
		// Info message
	case "WARN":
		eh.logger.Warnf(message)
	case "ERROR":
		eh.logger.Errorf(message)
	}
	
	// 记录详细信息
	if err.Metadata != nil && len(err.Metadata) > 0 {
		// 错误元数据已记录
	}
}

// getLogLevel 获取日志级别
func (eh *ErrorHandler) getLogLevel(severity ErrorSeverity) string {
	switch severity {
	case SeverityLow:
		return "DEBUG"
	case SeverityMedium:
		return "INFO"
	case SeverityHigh:
		return "WARN"
	case SeverityCritical:
		return "ERROR"
	default:
		return "INFO"
	}
}

// RetryWithPolicy 使用策略重试
func (eh *ErrorHandler) RetryWithPolicy(operation func() error, context string, metadata map[string]interface{}) error {
	var lastErr *DetailedError
	maxRetries := eh.retryPolicy.MaxRetries
	
	// 对于连接错误，增加重试次数
	if metadata != nil {
		if errorType, exists := metadata["error_type"]; exists && errorType == "connection" {
			maxRetries = maxRetries * 2 // 连接错误时加倍重试
		}
	}
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := operation()
		if err == nil {
			// 操作成功，更新成功统计
			if lastErr != nil {
				eh.mu.Lock()
				if stats, exists := eh.errorStats[lastErr.Category]; exists {
					stats.SuccessCount++
				}
				eh.mu.Unlock()
			}
			return nil
		}
		
		// 处理错误
		lastErr = eh.HandleError(err, context, metadata)
		lastErr.RetryCount = attempt
		
		// 检查是否应该重试
		if !lastErr.IsRetryable() || attempt >= maxRetries {
			break
		}
		
		// 计算延迟并等待
		delay := eh.retryPolicy.CalculateDelay(attempt + 1)
		
		// 对于网络错误，使用更长的延迟
		if lastErr.Category == ErrorCategoryNetwork {
			delay = delay * 2
		}
		
		select {
		case <-eh.ctx.Done():
			return eh.ctx.Err()
		case <-time.After(delay):
			// 继续重试
		}
	}
	
	return lastErr
}

// GetErrorStats 获取错误统计
func (eh *ErrorHandler) GetErrorStats() map[ErrorCategory]*ErrorStats {
	eh.mu.RLock()
	defer eh.mu.RUnlock()
	
	stats := make(map[ErrorCategory]*ErrorStats)
	for category, stat := range eh.errorStats {
		stats[category] = &ErrorStats{
			TotalCount:    stat.TotalCount,
			RecentCount:   stat.RecentCount,
			LastOccurred:  stat.LastOccurred,
			FirstOccurred: stat.FirstOccurred,
			RetryCount:    stat.RetryCount,
			SuccessCount:  stat.SuccessCount,
		}
	}
	
	return stats
}

// GetRecentErrors 获取最近的错误
func (eh *ErrorHandler) GetRecentErrors(limit int) []DetailedError {
	eh.mu.RLock()
	defer eh.mu.RUnlock()
	
	if limit <= 0 || limit > len(eh.errorHistory) {
		limit = len(eh.errorHistory)
	}
	
	start := len(eh.errorHistory) - limit
	return eh.errorHistory[start:]
}

// ClearStats 清除统计信息
func (eh *ErrorHandler) ClearStats() {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	
	for category := range eh.errorStats {
		eh.errorStats[category].RecentCount = 0
	}
}

// statsCleanupLoop 统计清理循环
func (eh *ErrorHandler) statsCleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-eh.ctx.Done():
			return
		case <-ticker.C:
			eh.ClearStats()
		}
	}
}

// Stop 停止错误处理器
func (eh *ErrorHandler) Stop() {
	eh.cancel()
}

// SetRetryPolicy 设置重试策略
func (eh *ErrorHandler) SetRetryPolicy(policy *RetryPolicy) {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	eh.retryPolicy = policy
}

// contains 检查字符串是否包含任一关键词
func contains(str string, keywords []string) bool {
	for _, keyword := range keywords {
		if len(str) >= len(keyword) {
			for i := 0; i <= len(str)-len(keyword); i++ {
				if str[i:i+len(keyword)] == keyword {
					return true
				}
			}
		}
	}
	return false
}

// isConnectionError 检查是否是连接相关错误
func (eh *ErrorHandler) isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	
	errorStr := strings.ToLower(err.Error())
	connectionKeywords := []string{"eof", "connection", "broken pipe", "forcibly closed", "network is unreachable", "no route to host", "timeout", "refused"}
	
	for _, keyword := range connectionKeywords {
		if strings.Contains(errorStr, keyword) {
			return true
		}
	}
	return false
}