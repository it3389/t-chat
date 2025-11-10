package handler

import (
	"fmt"
	"t-chat/internal/network"
)

// TextMessageHandler 文本消息处理器
type TextMessageHandler struct {
	*BaseMessageHandler
}

// NewTextMessageHandler 创建文本消息处理器
func NewTextMessageHandler(pineconeService network.PineconeServiceLike, logger network.Logger) *TextMessageHandler {
	base := NewBaseMessageHandler(network.MessageTypeText, network.MessagePriorityNormal, pineconeService, logger)
	return &TextMessageHandler{
		BaseMessageHandler: base,
	}
}

// HandleMessage 处理文本消息
func (h *TextMessageHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleTextMessage)
}

// handleTextMessage 实际的文本消息处理逻辑
func (h *TextMessageHandler) handleTextMessage(msg *network.Message) error {
	// 检查是否为系统内部消息类型，如果是则不显示详细内容
	if msg.Type == "user_info_exchange" {
		// 用户信息交换消息不需要显示详细内容，已由UserInfoExchangeHandler处理
		return nil
	}
	
	// 格式化时间戳
	timeStr := h.FormatTimestamp(msg.Timestamp)
	
	// 尝试获取发送者的用户名
	if username, found := h.GetUsernameByPubKey(msg.From); found {
		// 专业友好的消息显示格式
		fmt.Printf("\n\033[36m[%s]\033[0m \033[32m%s\033[0m: %s\n", timeStr, username, msg.Content)
	} else {
		// 使用公钥前8位作为标识
		if len(msg.From) >= 8 {
			fmt.Printf("\n\033[36m[%s]\033[0m \033[33m%s\033[0m: %s\n", timeStr, msg.From[:8]+"...", msg.Content)
		} else {
			fmt.Printf("\n\033[36m[%s]\033[0m \033[33m%s\033[0m: %s\n", timeStr, msg.From, msg.Content)
		}
	}
	
	// 发送消息确认
	if msg.ID != "" {
		if err := h.SendAckMessage(msg.From, msg.ID, "delivered"); err != nil {
			h.logger.Errorf("[TextHandler] ❌ 发送消息确认失败: %v", err)
			return err
		} else {
			h.logger.Debugf("[TextHandler] ✅ 已向 %s 发送消息确认 (MsgID: %s)", msg.From, msg.ID)
		}
	}
	
	return nil
}

// CanHandle 检查是否能处理消息
func (h *TextMessageHandler) CanHandle(msg *network.Message) bool {
	// 明确排除user_info_exchange消息，确保由专用处理器处理
	if msg != nil && msg.Type == "user_info_exchange" {
		return false
	}
	return h.BaseMessageHandler.CanHandle(msg)
}

func (h *TextMessageHandler) GetMessageType() string {
	return network.MessageTypeText
}

func (h *TextMessageHandler) GetPriority() int {
    return network.MessagePriorityNormal
}

func (h *TextMessageHandler) Initialize() error {
    return nil
}

func (h *TextMessageHandler) Cleanup() error {
	return nil
}