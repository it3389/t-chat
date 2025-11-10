package handler

import (
    "encoding/json"
    "fmt"
    "time"
    "t-chat/internal/network"
)

// UserInfoExchangeHandler 用户信息交换处理器
type UserInfoExchangeHandler struct {
	*BaseMessageHandler
}

// NewUserInfoExchangeHandler 创建用户信息交换处理器
func NewUserInfoExchangeHandler(pineconeService network.PineconeServiceLike, logger network.Logger) *UserInfoExchangeHandler {
	base := NewBaseMessageHandler("user_info_exchange", 5, pineconeService, logger)
	return &UserInfoExchangeHandler{
		BaseMessageHandler: base,
	}
}

// HandleMessage 处理用户信息交换消息
func (h *UserInfoExchangeHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleUserInfoExchange)
}

// handleUserInfoExchange 处理用户信息交换的核心逻辑
func (h *UserInfoExchangeHandler) handleUserInfoExchange(msg *network.Message) error {
    h.LogMessageReceived(msg)

	// 解析用户信息交换数据
	var userInfo map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Content), &userInfo); err != nil {
		h.logger.Errorf("解析用户信息交换数据失败: %v", err)
		return fmt.Errorf("解析用户信息交换数据失败: %v", err)
	}

	// 获取用户名和公钥
	username, hasUsername := userInfo["username"].(string)
	pubkey, hasPubkey := userInfo["pubkey"].(string)

	if !hasUsername || !hasPubkey {
		h.logger.Errorf("用户信息交换数据不完整: username=%v, pubkey=%v", hasUsername, hasPubkey)
		return fmt.Errorf("用户信息交换数据不完整")
	}

	h.logger.Infof("收到用户信息交换: 用户名=%s, 公钥=%s", username, pubkey)

	// 更新用户映射
	if ps, ok := h.pineconeService.(interface {
		UpdateUserMapping(username, pubkey string)
	}); ok {
		ps.UpdateUserMapping(username, pubkey)
	}

	// 发送用户信息响应
	if err := h.sendUserInfoResponse(msg.From); err != nil {
		h.logger.Errorf("发送用户信息响应失败: %v", err)
		return err
	}

	// 发送确认消息
    if err := h.SendAckMessage(msg.From, msg.ID, "received"); err != nil {
        h.logger.Errorf("发送用户信息交换确认失败: %v", err)
    }

    h.LogMessageProcessed(msg, nil)
    return nil
}

// sendUserInfoResponse 发送用户信息响应
func (h *UserInfoExchangeHandler) sendUserInfoResponse(toPubKey string) error {
	// 获取当前用户信息
	currentUser := h.pineconeService.FriendList().GetCurrentAccount()
	if currentUser == nil {
		return fmt.Errorf("无法获取当前用户信息")
	}

	// 构建响应数据
	responseData := map[string]interface{}{
		"username": currentUser.Username,
		"pubkey":   h.pineconeService.GetPublicKeyHex(),
		"type":     "response",
	}

	responseJSON, err := json.Marshal(responseData)
	if err != nil {
		return fmt.Errorf("序列化用户信息响应失败: %v", err)
	}

	// 创建响应消息
    // 构建并发送 MessagePacket
    packet := network.NewMessageBuilder().
        SetFrom(h.pineconeService.GetPublicKeyHex()).
        SetTo(toPubKey).
        SetType("user_info_exchange").
        SetContent(string(responseJSON)).
        SetPriority(network.MessagePriorityNormal).
        Build()

    // 发送响应
    if err := h.pineconeService.SendMessagePacket(toPubKey, packet); err != nil {
        return fmt.Errorf("发送用户信息响应失败: %v", err)
    }

	h.logger.Debugf("已发送用户信息响应给: %s", toPubKey)
	return nil
}

// CanHandle 检查是否可以处理该消息
func (h *UserInfoExchangeHandler) CanHandle(msg *network.Message) bool {
	return msg != nil && msg.Type == "user_info_exchange"
}

// generateMessageID 生成消息ID
func (h *UserInfoExchangeHandler) generateMessageID() string {
    return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}