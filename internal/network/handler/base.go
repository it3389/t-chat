package handler

import (
	"fmt"
	"sync"
	"time"
	"t-chat/internal/network"
	"t-chat/internal/performance"
)

// MessageHandlerInterface 消息处理器接口
type MessageHandlerInterface interface {
	// 核心处理方法
	HandleMessage(msg *network.Message) error
	
	// 处理器元信息
	GetMessageType() string
	GetPriority() int
	CanHandle(msg *network.Message) bool
	
	// 生命周期管理
	Initialize() error
	Cleanup() error
	IsInitialized() bool
	
	// 统计信息
	GetStats() HandlerStats
	ResetStats()
}

// HandlerStats 处理器统计信息
type HandlerStats struct {
	MessagesProcessed int64     `json:"messages_processed"`
	MessagesSucceeded int64     `json:"messages_succeeded"`
	MessagesFailed    int64     `json:"messages_failed"`
	LastProcessedTime time.Time `json:"last_processed_time"`
	AverageProcessTime time.Duration `json:"average_process_time"`
	TotalProcessTime  time.Duration `json:"total_process_time"`
}

// BaseMessageHandler 基础消息处理器
// 提供通用功能和默认实现，减少代码重复
type BaseMessageHandler struct {
	// 基本配置
	messageType string
	priority    int
	
	// 依赖服务
	pineconeService network.PineconeServiceLike
	logger         network.Logger
	
	// 状态管理
	initialized bool
	mutex       sync.RWMutex
	
	// 统计信息
	stats HandlerStats
	
	// 性能优化
	stringOptimizer *performance.StringOptimizer
	jsonOptimizer   *performance.JSONOptimizer
}

// NewBaseMessageHandler 创建基础消息处理器
func NewBaseMessageHandler(messageType string, priority int, pineconeService network.PineconeServiceLike, logger network.Logger) *BaseMessageHandler {
	return &BaseMessageHandler{
		messageType:     messageType,
		priority:        priority,
		pineconeService: pineconeService,
		logger:          logger,
		stringOptimizer: performance.GetStringOptimizer(),
		jsonOptimizer:   performance.GetJSONOptimizer(),
	}
}

// GetMessageType 获取消息类型
func (bmh *BaseMessageHandler) GetMessageType() string {
	return bmh.messageType
}

// GetPriority 获取优先级
func (bmh *BaseMessageHandler) GetPriority() int {
	return bmh.priority
}

// CanHandle 检查是否能处理消息（默认实现）
func (bmh *BaseMessageHandler) CanHandle(msg *network.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Type == bmh.messageType
}

// HandleMessage 处理消息（需要子类重写）
func (bmh *BaseMessageHandler) HandleMessage(msg *network.Message) error {
	return fmt.Errorf("基础处理器不能直接处理消息，需要子类实现")
}

// Initialize 初始化处理器
func (bmh *BaseMessageHandler) Initialize() error {
	bmh.mutex.Lock()
	defer bmh.mutex.Unlock()
	
	if bmh.initialized {
		return nil
	}
	
	// 验证依赖
	if bmh.pineconeService == nil {
		return fmt.Errorf("PineconeService 不能为空")
	}
	
	if bmh.logger == nil {
		return fmt.Errorf("Logger 不能为空")
	}
	
	bmh.initialized = true
	bmh.logger.Debugf("[BaseHandler] %s 处理器初始化完成", bmh.messageType)
	return nil
}

// Cleanup 清理处理器
func (bmh *BaseMessageHandler) Cleanup() error {
	bmh.mutex.Lock()
	defer bmh.mutex.Unlock()
	
	bmh.initialized = false
	bmh.logger.Debugf("[BaseHandler] %s 处理器清理完成", bmh.messageType)
	return nil
}

// IsInitialized 检查是否已初始化
func (bmh *BaseMessageHandler) IsInitialized() bool {
	bmh.mutex.RLock()
	defer bmh.mutex.RUnlock()
	return bmh.initialized
}

// GetStats 获取统计信息
func (bmh *BaseMessageHandler) GetStats() HandlerStats {
	bmh.mutex.RLock()
	defer bmh.mutex.RUnlock()
	return bmh.stats
}

// ResetStats 重置统计信息
func (bmh *BaseMessageHandler) ResetStats() {
	bmh.mutex.Lock()
	defer bmh.mutex.Unlock()
	bmh.stats = HandlerStats{}
}

// 受保护的方法，供子类使用

// ValidateMessage 验证消息
func (bmh *BaseMessageHandler) ValidateMessage(msg *network.Message) error {
	if msg == nil {
		return fmt.Errorf("消息不能为空")
	}
	
	if msg.Type == "" {
		return fmt.Errorf("消息类型不能为空")
	}
	
	if msg.From == "" {
		return fmt.Errorf("消息发送者不能为空")
	}
	
	return nil
}

// SendAckMessage 发送确认消息
func (bmh *BaseMessageHandler) SendAckMessage(to, originalMsgID, status string) error {
	if bmh.pineconeService == nil {
		return fmt.Errorf("PineconeService 未初始化")
	}
	
	// 使用类型断言检查是否支持SendAckMessage方法
	if ps, ok := bmh.pineconeService.(interface {
		SendAckMessage(string, string, string) error
	}); ok {
		bmh.logger.Debugf("[BaseHandler] 发送ACK确认: To=%s, OriginalMsgID=%s, Status=%s", to, originalMsgID, status)
		return ps.SendAckMessage(to, originalMsgID, status)
	}
	
	return fmt.Errorf("PineconeService 不支持 SendAckMessage 方法")
}

// GetUsernameByPubKey 根据公钥获取用户名
func (bmh *BaseMessageHandler) GetUsernameByPubKey(pubKey string) (string, bool) {
	if bmh.pineconeService == nil {
		return "", false
	}
	
	// 使用类型断言检查是否支持GetUsernameByPubKey方法
	if ps, ok := bmh.pineconeService.(interface {
		GetUsernameByPubKey(string) (string, bool)
	}); ok {
		return ps.GetUsernameByPubKey(pubKey)
	}
	
	return "", false
}

// FormatTimestamp 格式化时间戳
func (bmh *BaseMessageHandler) FormatTimestamp(t time.Time) string {
	return t.Format("15:04:05")
}

// LogMessageReceived 记录消息接收日志
func (bmh *BaseMessageHandler) LogMessageReceived(msg *network.Message) {
	bmh.logger.Debugf("[%s] 接收到消息: ID=%s, From=%s, Content=%s", 
		bmh.messageType, msg.ID, msg.From, msg.Content)
}

// LogMessageProcessed 记录消息处理完成日志
func (bmh *BaseMessageHandler) LogMessageProcessed(msg *network.Message, err error) {
	if err != nil {
		bmh.logger.Errorf("[%s] 消息处理失败: ID=%s, Error=%v", 
			bmh.messageType, msg.ID, err)
	} else {
		bmh.logger.Debugf("[%s] 消息处理成功: ID=%s", 
			bmh.messageType, msg.ID)
	}
}

// UpdateStats 更新统计信息
func (bmh *BaseMessageHandler) UpdateStats(startTime time.Time, err error) {
	bmh.mutex.Lock()
	defer bmh.mutex.Unlock()
	
	processTime := time.Since(startTime)
	bmh.stats.MessagesProcessed++
	bmh.stats.LastProcessedTime = time.Now()
	bmh.stats.TotalProcessTime += processTime
	
	if bmh.stats.MessagesProcessed > 0 {
		bmh.stats.AverageProcessTime = bmh.stats.TotalProcessTime / time.Duration(bmh.stats.MessagesProcessed)
	}
	
	if err != nil {
		bmh.stats.MessagesFailed++
	} else {
		bmh.stats.MessagesSucceeded++
	}
}

// ProcessMessageWithStats 带统计信息的消息处理包装器
func (bmh *BaseMessageHandler) ProcessMessageWithStats(msg *network.Message, handler func(*network.Message) error) error {
	startTime := time.Now()
	
	// 验证消息
	if err := bmh.ValidateMessage(msg); err != nil {
		bmh.UpdateStats(startTime, err)
		return err
	}
	
	// 记录接收日志
	bmh.LogMessageReceived(msg)
	
	// 处理消息
	err := handler(msg)
	
	// 更新统计信息
	bmh.UpdateStats(startTime, err)
	
	// 记录处理结果
	bmh.LogMessageProcessed(msg, err)
	
	return err
}