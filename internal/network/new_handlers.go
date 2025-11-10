package network

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// NewTextMessageHandler 创建新的文本消息处理器
type NewTextMessageHandler struct {
	pineconeService PineconeServiceLike
	logger         Logger
	initialized    bool
	mutex          sync.RWMutex
	stats          HandlerStats
}

// HandlerStats 处理器统计信息
type HandlerStats struct {
	MessagesProcessed int64         `json:"messages_processed"`
	MessagesSucceeded int64         `json:"messages_succeeded"`
	MessagesFailed    int64         `json:"messages_failed"`
	LastProcessedTime time.Time     `json:"last_processed_time"`
	AverageProcessTime time.Duration `json:"average_process_time"`
	TotalProcessTime  time.Duration `json:"total_process_time"`
}

func NewNewTextMessageHandler(pineconeService PineconeServiceLike, logger Logger) *NewTextMessageHandler {
	return &NewTextMessageHandler{
		pineconeService: pineconeService,
		logger:         logger,
		initialized:    false,
	}
}

func (h *NewTextMessageHandler) HandleMessage(msg *Message) error {
	startTime := time.Now()
	
	h.logger.Debugf("📨 处理文本消息: ID=%s, From=%s, Content=%s", msg.ID, msg.From, msg.Content)
	
	// 验证消息内容
	if msg.Content == "" {
		h.logger.Warnf("收到空内容的文本消息: ID=%s", msg.ID)
		h.updateStats(startTime, fmt.Errorf("消息内容为空"))
		return fmt.Errorf("消息内容为空")
	}
	
	// 将消息放入消息通道，以便接收方能够获取到消息
	messageChannel := h.pineconeService.GetMessageChannel()
	if messageChannel != nil {
		select {
		case messageChannel <- msg:
			h.logger.Debugf("✅ 消息已放入消息通道: ID=%s", msg.ID)
		default:
			h.logger.Warnf("⚠️ 消息通道已满，无法放入消息: ID=%s", msg.ID)
		}
	} else {
		h.logger.Warnf("⚠️ 消息通道未初始化")
	}
	
	// 发送ACK确认
	if err := h.sendAckMessage(msg.From, msg.ID, "success"); err != nil {
		h.logger.Errorf("发送ACK失败: %v", err)
	}
	
	h.updateStats(startTime, nil)
	h.logger.Debugf("✅ 文本消息处理完成: ID=%s", msg.ID)
	return nil
}

func (h *NewTextMessageHandler) GetMessageType() string {
	return "text"
}

func (h *NewTextMessageHandler) GetPriority() int {
	return 5
}

func (h *NewTextMessageHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == "text"
}

func (h *NewTextMessageHandler) Initialize() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.initialized = true
	return nil
}

func (h *NewTextMessageHandler) Cleanup() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.initialized = false
	return nil
}

func (h *NewTextMessageHandler) sendAckMessage(to, originalMsgID, status string) error {
    // 统一通过PineconeService构造并发送ACK，包含metadata中的ack_data
    return h.pineconeService.SendAckMessage(to, originalMsgID, status)
}

func (h *NewTextMessageHandler) updateStats(startTime time.Time, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	h.stats.MessagesProcessed++
	h.stats.LastProcessedTime = time.Now()
	
	processingTime := time.Since(startTime)
	h.stats.TotalProcessTime += processingTime
	h.stats.AverageProcessTime = h.stats.TotalProcessTime / time.Duration(h.stats.MessagesProcessed)
	
	if err != nil {
		h.stats.MessagesFailed++
	} else {
		h.stats.MessagesSucceeded++
	}
}

// NewSystemMessageHandler 创建新的系统消息处理器
type NewSystemMessageHandler struct {
	pineconeService PineconeServiceLike
	logger         Logger
	initialized    bool
	mutex          sync.RWMutex
	stats          HandlerStats
}

func NewNewSystemMessageHandler(pineconeService PineconeServiceLike, logger Logger) *NewSystemMessageHandler {
	return &NewSystemMessageHandler{
		pineconeService: pineconeService,
		logger:         logger,
		initialized:    false,
	}
}

func (h *NewSystemMessageHandler) HandleMessage(msg *Message) error {
	startTime := time.Now()
	
	h.logger.Debugf("处理系统消息: From=%s, Content=%s", msg.From, msg.Content)
	
	// 发送ACK确认
	if err := h.sendAckMessage(msg.From, msg.ID, "success"); err != nil {
		h.logger.Errorf("发送ACK失败: %v", err)
	}
	
	h.updateStats(startTime, nil)
	h.logger.Debugf("系统消息处理完成: From=%s", msg.From)
	return nil
}

func (h *NewSystemMessageHandler) GetMessageType() string {
	return "system"
}

func (h *NewSystemMessageHandler) GetPriority() int {
	return 8
}

func (h *NewSystemMessageHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == "system"
}

func (h *NewSystemMessageHandler) Initialize() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.initialized = true
	return nil
}

func (h *NewSystemMessageHandler) Cleanup() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.initialized = false
	return nil
}

func (h *NewSystemMessageHandler) sendAckMessage(to, originalMsgID, status string) error {
    // 统一通过PineconeService构造并发送ACK，包含metadata中的ack_data
    return h.pineconeService.SendAckMessage(to, originalMsgID, status)
}

func (h *NewSystemMessageHandler) updateStats(startTime time.Time, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	h.stats.MessagesProcessed++
	h.stats.LastProcessedTime = time.Now()
	
	processingTime := time.Since(startTime)
	h.stats.TotalProcessTime += processingTime
	h.stats.AverageProcessTime = h.stats.TotalProcessTime / time.Duration(h.stats.MessagesProcessed)
	
	if err != nil {
		h.stats.MessagesFailed++
	} else {
		h.stats.MessagesSucceeded++
	}
}

// NewUserInfoExchangeHandler 创建新的用户信息交换处理器
type NewUserInfoExchangeHandler struct {
	pineconeService PineconeServiceLike
	logger         Logger
	initialized    bool
	mutex          sync.RWMutex
	stats          HandlerStats
}

func NewNewUserInfoExchangeHandler(pineconeService PineconeServiceLike, logger Logger) *NewUserInfoExchangeHandler {
	return &NewUserInfoExchangeHandler{
		pineconeService: pineconeService,
		logger:         logger,
		initialized:    false,
	}
}

func (h *NewUserInfoExchangeHandler) HandleMessage(msg *Message) error {
	startTime := time.Now()
	
	h.logger.Debugf("处理用户信息交换消息: From=%s", msg.From)
	
	// 解析用户信息
	var userInfo map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Content), &userInfo); err != nil {
		h.logger.Errorf("解析用户信息失败: %v", err)
		h.updateStats(startTime, err)
		return fmt.Errorf("解析用户信息失败: %v", err)
	}
	
	// 发送ACK确认
	if err := h.sendAckMessage(msg.From, msg.ID, "success"); err != nil {
		h.logger.Errorf("发送ACK失败: %v", err)
	}
	
	h.updateStats(startTime, nil)
	h.logger.Debugf("用户信息交换处理完成: From=%s", msg.From)
	return nil
}

func (h *NewUserInfoExchangeHandler) GetMessageType() string {
	return "user_info_exchange"
}

func (h *NewUserInfoExchangeHandler) GetPriority() int {
	return 7
}

func (h *NewUserInfoExchangeHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == "user_info_exchange"
}

func (h *NewUserInfoExchangeHandler) Initialize() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.initialized = true
	return nil
}

func (h *NewUserInfoExchangeHandler) Cleanup() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.initialized = false
	return nil
}

func (h *NewUserInfoExchangeHandler) sendAckMessage(to, originalMsgID, status string) error {
    // 统一通过PineconeService构造并发送ACK，包含metadata中的ack_data
    return h.pineconeService.SendAckMessage(to, originalMsgID, status)
}

func (h *NewUserInfoExchangeHandler) updateStats(startTime time.Time, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	h.stats.MessagesProcessed++
	h.stats.LastProcessedTime = time.Now()
	
	processingTime := time.Since(startTime)
	h.stats.TotalProcessTime += processingTime
	h.stats.AverageProcessTime = h.stats.TotalProcessTime / time.Duration(h.stats.MessagesProcessed)
	
	if err != nil {
		h.stats.MessagesFailed++
	} else {
		h.stats.MessagesSucceeded++
	}
}