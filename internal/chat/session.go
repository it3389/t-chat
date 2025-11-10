package chat

import (
	"fmt"
	"time"
)

// Session 聊天会话管理器
// 实现 SessionInterface 接口，管理单个用户的聊天会话
type Session struct {
	username  string                            // 当前用户名
	msgStore  MessageStoreInterface             // 消息存储接口
	deliverFn func(to, content string) error   // 消息投递函数
}

// 确保 Session 实现了 SessionInterface 接口
var _ SessionInterface = (*Session)(nil)

// NewSession 创建新会话
func NewSession(username string, msgStore MessageStoreInterface) *Session {
	return &Session{
		username: username,
		msgStore: msgStore,
	}
}

// SendMessage 发送消息
func (s *Session) SendMessage(to, content string) error {
	msg := &Message{
		ID:        generateMessageID(),
		From:      s.username,
		To:        to,
		Content:   content,
		Type:      "text",
		Timestamp: time.Now(),
		Status:    "sent",
	}

	return s.msgStore.AddMessage(msg)
}

// ReceiveMessage 接收消息
func (s *Session) ReceiveMessage(from, content string) error {
	msg := &Message{
		ID:        generateMessageID(),
		From:      from,
		To:        s.username,
		Content:   content,
		Type:      "text",
		Timestamp: time.Now(),
		Status:    "delivered",
	}

	return s.msgStore.AddMessage(msg)
}

// GetMessages 获取会话消息
func (s *Session) GetMessages(with string) []*Message {
	return s.msgStore.GetMessages(s.username, with)
}

// GetUnreadMessages 获取未读消息
func (s *Session) GetUnreadMessages() []*Message {
	return s.msgStore.GetUnreadMessages(s.username)
}

// MarkRead 标记会话消息为已读
func (s *Session) MarkAsRead(messageID string) error {
	return s.msgStore.MarkAsRead(messageID)
}

// DeliverOfflineMessages 投递离线消息
func (s *Session) DeliverOfflineMessages(deliverFn func(to, content string) error) {
	s.deliverFn = deliverFn

	// 获取未读消息
	unread := s.GetUnreadMessages()

	for _, msg := range unread {
		if err := s.deliverFn(msg.To, msg.Content); err != nil {
			fmt.Printf("投递消息失败 %v\n", err)
			continue
		}

		// 标记为已投递
		s.msgStore.MarkAsDelivered(msg.ID)
	}
}

// generateMessageID 生成消息ID
func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}
