package handler

import (
	"t-chat/internal/network"
)

// SystemMessageHandler 系统消息处理器
type SystemMessageHandler struct {
	*BaseMessageHandler
}

// NewSystemMessageHandler 创建系统消息处理器
func NewSystemMessageHandler(pineconeService network.PineconeServiceLike, logger network.Logger) *SystemMessageHandler {
	base := NewBaseMessageHandler(network.MessageTypeSystem, network.MessagePriorityHigh, pineconeService, logger)
	return &SystemMessageHandler{
		BaseMessageHandler: base,
	}
}

// HandleMessage 处理系统消息
func (h *SystemMessageHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleSystemMessage)
}

// handleSystemMessage 实际的系统消息处理逻辑
func (h *SystemMessageHandler) handleSystemMessage(msg *network.Message) error {
	// 系统消息处理 - 仅在需要时输出
	h.logger.Debugf("[SystemHandler] 处理系统消息: ID=%s, Content=%s", msg.ID, msg.Content)
	return nil
}