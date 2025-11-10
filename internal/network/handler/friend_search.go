package handler

import (
    "encoding/json"
    "fmt"
    "strings"
    "time"
    "t-chat/internal/network"
)

type FriendSearchHandler struct {
	*BaseMessageHandler
	getLocalAccount func() (username, pineconeAddr string)
}

func NewFriendSearchHandler(p network.PineconeServiceLike, logger network.Logger, getLocalAccount func() (string, string)) *FriendSearchHandler {
	base := NewBaseMessageHandler("friend_search_request", 6, p, logger)
	return &FriendSearchHandler{
		BaseMessageHandler: base,
		getLocalAccount:    getLocalAccount,
	}
}

func (h *FriendSearchHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleFriendSearchRequest)
}

func (h *FriendSearchHandler) handleFriendSearchRequest(msg *network.Message) error {
    h.LogMessageReceived(msg)

	// 解析搜索请求
	var searchData map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Content), &searchData); err != nil {
		h.logger.Errorf("解析好友搜索请求失败: %v", err)
		return fmt.Errorf("解析好友搜索请求失败: %v", err)
	}

	searchQuery, hasQuery := searchData["query"].(string)
	if !hasQuery || searchQuery == "" {
		h.logger.Errorf("好友搜索请求缺少查询内容")
		return fmt.Errorf("好友搜索请求缺少查询内容")
	}

	h.logger.Infof("收到好友搜索请求: 查询=%s, 来自=%s", searchQuery, msg.From)

	// 检查当前用户是否匹配搜索条件
	if h.getLocalAccount != nil {
		username, pineconeAddr := h.getLocalAccount()
		if h.matchesSearchQuery(username, searchQuery) {
			// 发送搜索响应
			if err := h.sendFriendSearchResponse(msg.From, username, pineconeAddr); err != nil {
				h.logger.Errorf("发送好友搜索响应失败: %v", err)
				return err
			}
		}
	}

	// 搜索好友列表
	for _, f := range h.pineconeService.FriendList().GetAllFriends() {
		if h.matchesSearchQuery(f.Username, searchQuery) {
			if err := h.sendFriendSearchResponse(msg.From, f.Username, f.PineconeAddr); err != nil {
				h.logger.Errorf("发送好友搜索响应失败: %v", err)
			}
		}
	}

	// 发送确认消息
    if err := h.SendAckMessage(msg.From, msg.ID, "received"); err != nil {
        h.logger.Errorf("发送好友搜索确认失败: %v", err)
    }

    h.LogMessageProcessed(msg, nil)
    return nil
}

// matchesSearchQuery 检查用户名是否匹配搜索查询
func (h *FriendSearchHandler) matchesSearchQuery(username, query string) bool {
	return len(username) > 0 && len(query) > 0 && strings.Contains(strings.ToLower(username), strings.ToLower(query))
}

// sendFriendSearchResponse 发送好友搜索响应
func (h *FriendSearchHandler) sendFriendSearchResponse(toPubKey, username, addr string) error {
    responseData := map[string]interface{}{
        "username": username,
        "pubkey":   h.pineconeService.GetPublicKeyHex(),
        "addr":     addr,
    }

    responseJSON, err := json.Marshal(responseData)
    if err != nil {
        return fmt.Errorf("序列化好友搜索响应失败: %v", err)
    }

    // 使用消息构建器构建 MessagePacket 并发送
    packet := network.NewMessageBuilder().
        SetFrom(h.pineconeService.GetPublicKeyHex()).
        SetTo(toPubKey).
        SetType("friend_search_response").
        SetContent(string(responseJSON)).
        SetPriority(network.MessagePriorityNormal).
        Build()

    if err := h.pineconeService.SendMessagePacket(toPubKey, packet); err != nil {
        return fmt.Errorf("发送好友搜索响应失败: %v", err)
    }

	h.logger.Debugf("已发送好友搜索响应给: %s", toPubKey)
	return nil
}

// generateMessageID 生成消息ID
func (h *FriendSearchHandler) generateMessageID() string {
    return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

// CanHandle 检查是否可以处理该消息
func (h *FriendSearchHandler) CanHandle(msg *network.Message) bool {
	return msg != nil && msg.Type == "friend_search_request"
}
