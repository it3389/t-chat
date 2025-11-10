package handler

import (
	"encoding/json"
	"fmt"
	"t-chat/internal/network"
)

// AckHandler ACK消息处理器
type AckHandler struct {
	*BaseMessageHandler
}

// NewAckHandler 创建ACK处理器
func NewAckHandler(pineconeService network.PineconeServiceLike, logger network.Logger) *AckHandler {
	base := NewBaseMessageHandler("ack", 10, pineconeService, logger)
	return &AckHandler{
		BaseMessageHandler: base,
	}
}

// HandleMessage 处理ACK消息
func (h *AckHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleAckMessage)
}

// handleAckMessage 处理ACK消息的核心逻辑
func (h *AckHandler) handleAckMessage(msg *network.Message) error {
    h.LogMessageReceived(msg)

	// 解析ACK数据
	var ackData map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Content), &ackData); err != nil {
		h.logger.Errorf("解析ACK数据失败: %v", err)
		return fmt.Errorf("解析ACK数据失败: %v", err)
	}

	messageID, hasMessageID := ackData["message_id"].(string)
	if !hasMessageID || messageID == "" {
		h.logger.Errorf("ACK消息缺少message_id")
		return fmt.Errorf("ACK消息缺少message_id")
	}

	h.logger.Debugf("收到ACK确认: 消息ID=%s, 来自=%s", messageID, msg.From)

	// 通知ACK接收
	if ps, ok := h.pineconeService.(interface {
		NotifyAckReceived(messageID string, success bool)
	}); ok {
		ps.NotifyAckReceived(messageID, true)
	}

    h.LogMessageProcessed(msg, nil)
    return nil
}

// CanHandle 检查是否可以处理该消息
func (h *AckHandler) CanHandle(msg *network.Message) bool {
	return msg != nil && msg.Type == "ack"
}

// NackHandler NACK消息处理器
type NackHandler struct {
	*BaseMessageHandler
}

// NewNackHandler 创建NACK处理器
func NewNackHandler(pineconeService network.PineconeServiceLike, logger network.Logger) *NackHandler {
	base := NewBaseMessageHandler("nack", 10, pineconeService, logger)
	return &NackHandler{
		BaseMessageHandler: base,
	}
}

// HandleMessage 处理NACK消息
func (h *NackHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleNackMessage)
}

// handleNackMessage 处理NACK消息的核心逻辑
func (h *NackHandler) handleNackMessage(msg *network.Message) error {
    h.LogMessageReceived(msg)

	// 解析NACK数据
	var nackData map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Content), &nackData); err != nil {
		h.logger.Errorf("解析NACK数据失败: %v", err)
		return fmt.Errorf("解析NACK数据失败: %v", err)
	}

	messageID, hasMessageID := nackData["message_id"].(string)
	if !hasMessageID || messageID == "" {
		h.logger.Errorf("NACK消息缺少message_id")
		return fmt.Errorf("NACK消息缺少message_id")
	}

	reason, _ := nackData["reason"].(string)
	if reason == "" {
		reason = "未知原因"
	}

	h.logger.Warnf("收到NACK否定确认: 消息ID=%s, 原因=%s, 来自=%s", messageID, reason, msg.From)

	// 通知NACK接收
	if ps, ok := h.pineconeService.(interface {
		NotifyAckReceived(messageID string, success bool)
	}); ok {
		ps.NotifyAckReceived(messageID, false)
	}

    h.LogMessageProcessed(msg, nil)
    return nil
}

// CanHandle 检查是否可以处理该消息
func (h *NackHandler) CanHandle(msg *network.Message) bool {
	return msg != nil && msg.Type == "nack"
}