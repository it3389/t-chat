package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// 消息状态常量
const (
	MessageStatusPending   = "pending"   // 待发送
	MessageStatusSent      = "sent"      // 已发送
	MessageStatusDelivered = "delivered" // 已送达
	MessageStatusRead      = "read"      // 已读
	MessageStatusFailed    = "failed"    // 发送失败
)

// Message 消息结构体
// 表示聊天系统中的一条消息
type Message struct {
	ID        string    `json:"id"`        // 消息唯一标识符
	From      string    `json:"from"`      // 发送者用户名
	To        string    `json:"to"`        // 接收者用户名
	Content   string    `json:"content"`   // 消息内容
	Type      string    `json:"type"`      // 消息类型：text, file, system
	Timestamp time.Time `json:"timestamp"` // 消息时间戳
	Status    string    `json:"status"`    // 消息状态：sent, delivered, read, failed
}

// MessageStore 消息存储实现
// 实现 MessageStoreInterface 接口，提供消息的持久化存储
type MessageStore struct {
	messages map[string][]*Message // 按会话ID分组的消息存储
	mutex    sync.RWMutex          // 读写锁，保证并发安全
	filePath string                // 消息存储文件路径
}

// 确保 MessageStore 实现了 MessageStoreInterface 接口
var _ MessageStoreInterface = (*MessageStore)(nil)

// NewMessageStore 创建新的消息存储
func NewMessageStore() *MessageStore {
	ms := &MessageStore{
		messages: make(map[string][]*Message),
		filePath: "messages/messages.json",
	}

	// 确保目录存在
	os.MkdirAll("messages", 0755)

	// 加载现有消息
	ms.loadMessages()

	return ms
}

// AddMessage 添加消息
func (ms *MessageStore) AddMessage(msg *Message) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	sessionID := ms.getSessionID(msg.From, msg.To)
	if ms.messages[sessionID] == nil {
		ms.messages[sessionID] = make([]*Message, 0)
	}

	ms.messages[sessionID] = append(ms.messages[sessionID], msg)

	return ms.saveMessages()
}

// GetMessages 获取会话消息
func (ms *MessageStore) GetMessages(from, to string) []*Message {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	sessionID := ms.getSessionID(from, to)
	return ms.messages[sessionID]
}

// GetUnreadMessages 获取未读消息
func (ms *MessageStore) GetUnreadMessages(username string) []*Message {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	var unread []*Message
	for sessionID, messages := range ms.messages {
		// 检查是否是发给该用户的消息
		if ms.isSessionForUser(sessionID, username) {
			for _, msg := range messages {
				if msg.To == username && msg.Status == "sent" {
					unread = append(unread, msg)
				}
			}
		}
	}

	return unread
}

// MarkAsRead 标记消息为已读
func (ms *MessageStore) MarkAsRead(messageID string) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	for _, messages := range ms.messages {
		for _, msg := range messages {
			if msg.ID == messageID {
				msg.Status = "read"
				return ms.saveMessages()
			}
		}
	}

	return fmt.Errorf("消息不存在 %s", messageID)
}

// MarkAsDelivered 标记消息为已送达
func (ms *MessageStore) MarkAsDelivered(messageID string) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	for _, messages := range ms.messages {
		for _, msg := range messages {
			if msg.ID == messageID {
				msg.Status = "delivered"
				return ms.saveMessages()
			}
		}
	}

	return fmt.Errorf("消息不存在 %s", messageID)
}

// MarkFailed 标记消息为失败
func (ms *MessageStore) MarkFailed(messageID string) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	for _, messages := range ms.messages {
		for _, msg := range messages {
			if msg.ID == messageID {
				msg.Status = "failed"
				return ms.saveMessages()
			}
		}
	}

	return fmt.Errorf("消息不存在 %s", messageID)
}

// getSessionID 获取会话ID
func (ms *MessageStore) getSessionID(from, to string) string {
	// 确保会话ID的一致性（按字母顺序排序）
	if from < to {
		return fmt.Sprintf("%s_%s", from, to)
	}
	return fmt.Sprintf("%s_%s", to, from)
}

// isSessionForUser 检查会话是否属于用户
func (ms *MessageStore) isSessionForUser(sessionID, username string) bool {
	// 简单的字符串包含检查
	return len(sessionID) > len(username) &&
		(sessionID[:len(username)] == username ||
			sessionID[len(sessionID)-len(username):] == username)
}

// loadMessages 加载消息
func (ms *MessageStore) loadMessages() error {
	data, err := os.ReadFile(ms.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在是正常
		}
		return err
	}

	var messages map[string][]*Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return err
	}

	ms.messages = messages
	return nil
}

// saveMessages 保存消息
func (ms *MessageStore) saveMessages() error {
	data, err := json.MarshalIndent(ms.messages, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ms.filePath, data, 0644)
}
