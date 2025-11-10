package network

import (
	"fmt"
	"sort"
	"sync"
)

// MessageDispatcher 改进的消息分发器
// 实现 MessageDispatcherInterface 接口
type MessageDispatcher struct {
	handlers       map[string]MessageHandlerInterface
	defaultHandler MessageHandlerInterface
	mu             sync.RWMutex
	logger         Logger
}

// 确保 MessageDispatcher 实现了 MessageDispatcherInterface 接口
var _ MessageDispatcherInterface = (*MessageDispatcher)(nil)

// NewMessageDispatcher 创建消息分发器
func NewMessageDispatcher(logger Logger) *MessageDispatcher {
	return &MessageDispatcher{
		handlers: make(map[string]MessageHandlerInterface),
		logger:   logger,
	}
}

// RegisterHandler 注册消息处理器
func (md *MessageDispatcher) RegisterHandler(handler MessageHandlerInterface) error {
	if handler == nil {
		return fmt.Errorf("处理器不能为空")
	}
	
	messageType := handler.GetMessageType()
	if messageType == "" {
		return fmt.Errorf("处理器消息类型不能为空")
	}
	
	md.mu.Lock()
	defer md.mu.Unlock()
	
	if _, exists := md.handlers[messageType]; exists {
		return fmt.Errorf("消息类型 %s 的处理器已存在", messageType)
	}
	
	// 初始化处理器
	if err := handler.Initialize(); err != nil {
		return fmt.Errorf("初始化处理器失败: %v", err)
	}
	
	md.handlers[messageType] = handler
	
	// 注册消息处理器
	
	return nil
}

// UnregisterHandler 注销消息处理器
func (md *MessageDispatcher) UnregisterHandler(messageType string) error {
	md.mu.Lock()
	defer md.mu.Unlock()
	
	handler, exists := md.handlers[messageType]
	if !exists {
		return fmt.Errorf("消息类型 %s 的处理器不存在", messageType)
	}
	
	// 清理处理器
	if err := handler.Cleanup(); err != nil {
		if md.logger != nil {
			md.logger.Warnf("清理处理器失败: %v", err)
		}
	}
	
	delete(md.handlers, messageType)
	
	// 注销消息处理器
	
	return nil
}

// 消息处理器缓存，避免重复查找
type handlerCache struct {
	handler MessageHandlerInterface
	canHandle bool
}

var handlerCachePool = sync.Pool{
	New: func() interface{} {
		return &handlerCache{}
	},
}

// DispatchMessage 分发消息
func (md *MessageDispatcher) DispatchMessage(msg *Message) error {
	if msg == nil {
		return fmt.Errorf("消息不能为空")
	}
	
	// 添加调试日志
	if md.logger != nil {
		md.logger.Debugf("[MessageDispatcher] 开始分发消息: Type=%s, From=%s, Content=%s", msg.Type, msg.From, msg.Content)
	}
	
	md.mu.RLock()
	handler, exists := md.handlers[msg.Type]
	defaultHandler := md.defaultHandler
	// 添加调试日志显示所有已注册的处理器
	if md.logger != nil {
		md.logger.Debugf("[MessageDispatcher] 查找处理器: 消息类型=%s, 是否找到专用处理器=%v", msg.Type, exists)
		for handlerType, h := range md.handlers {
			md.logger.Debugf("[MessageDispatcher] 已注册处理器: Type=%s, Handler=%T", handlerType, h)
		}
	}
	md.mu.RUnlock()
	
	// 查找特定类型的处理器
	if exists && handler.CanHandle(msg) {
		// 使用专用处理器处理消息
		if md.logger != nil {
			md.logger.Debugf("[MessageDispatcher] 使用专用处理器处理消息: %T", handler)
		}
		return handler.HandleMessage(msg)
	}
	
	// 使用默认处理器
	if defaultHandler != nil && defaultHandler.CanHandle(msg) {
		// 使用默认处理器处理消息
		if md.logger != nil {
			md.logger.Debugf("[MessageDispatcher] 使用默认处理器处理消息: %T", defaultHandler)
		}
		return defaultHandler.HandleMessage(msg)
	}
	
	// 尝试查找能处理该消息的处理器（使用对象池优化）
	md.mu.RLock()
	candidates := make([]MessageHandlerInterface, 0, len(md.handlers)) // 预分配容量
	for _, h := range md.handlers {
		if h.CanHandle(msg) {
			candidates = append(candidates, h)
		}
	}
	md.mu.RUnlock()
	
	if len(candidates) > 0 {
		// 按优先级排序，选择优先级最高的处理器
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].GetPriority() > candidates[j].GetPriority()
		})
		
		// 使用候选处理器处理消息
		if md.logger != nil {
			md.logger.Debugf("[MessageDispatcher] 使用候选处理器处理消息: %T (优先级: %d)", candidates[0], candidates[0].GetPriority())
		}
		return candidates[0].HandleMessage(msg)
	}
	
	if md.logger != nil {
		md.logger.Errorf("[MessageDispatcher] 未找到消息类型 %s 的处理器", msg.Type)
	}
	return &NetworkError{
		Code:    ErrCodeHandlerNotFound,
		Message: fmt.Sprintf("未找到消息类型 %s 的处理器", msg.Type),
	}
}

// GetHandlers 获取所有处理器
func (md *MessageDispatcher) GetHandlers() map[string]MessageHandlerInterface {
	md.mu.RLock()
	defer md.mu.RUnlock()
	
	result := make(map[string]MessageHandlerInterface)
	for messageType, handler := range md.handlers {
		result[messageType] = handler
	}
	
	return result
}

// SetDefaultHandler 设置默认处理器
func (md *MessageDispatcher) SetDefaultHandler(handler MessageHandlerInterface) {
	md.mu.Lock()
	defer md.mu.Unlock()
	
	if md.defaultHandler != nil {
		md.defaultHandler.Cleanup()
	}
	
	md.defaultHandler = handler
	
	if handler != nil {
		handler.Initialize()
		// 设置默认消息处理器
	}
}

// GetHandlerCount 获取处理器数量
func (md *MessageDispatcher) GetHandlerCount() int {
	md.mu.RLock()
	defer md.mu.RUnlock()
	return len(md.handlers)
}

// GetHandlersByPriority 按优先级获取处理器列表
func (md *MessageDispatcher) GetHandlersByPriority() []MessageHandlerInterface {
	md.mu.RLock()
	defer md.mu.RUnlock()
	
	var handlers []MessageHandlerInterface
	for _, handler := range md.handlers {
		handlers = append(handlers, handler)
	}
	
	// 按优先级排序
	sort.Slice(handlers, func(i, j int) bool {
		return handlers[i].GetPriority() > handlers[j].GetPriority()
	})
	
	return handlers
}

// Cleanup 清理分发器
func (md *MessageDispatcher) Cleanup() error {
	md.mu.Lock()
	defer md.mu.Unlock()
	
	// 清理所有处理器
	for messageType, handler := range md.handlers {
		if err := handler.Cleanup(); err != nil {
			if md.logger != nil {
				md.logger.Errorf("清理处理器 %s 失败: %v", messageType, err)
			}
		}
	}
	
	// 清理默认处理器
	if md.defaultHandler != nil {
		if err := md.defaultHandler.Cleanup(); err != nil {
			if md.logger != nil {
				md.logger.Errorf("清理默认处理器失败: %v", err)
			}
		}
	}
	
	md.handlers = make(map[string]MessageHandlerInterface)
	md.defaultHandler = nil
	
	// 消息分发器已清理
	
	return nil
}