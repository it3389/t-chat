package chat

import (
	"crypto/ecdsa"
)

// MessageStoreInterface 消息存储接口
type MessageStoreInterface interface {
	// AddMessage 添加消息
	AddMessage(msg *Message) error
	
	// GetMessages 获取会话消息
	GetMessages(from, to string) []*Message
	
	// GetUnreadMessages 获取未读消息
	GetUnreadMessages(username string) []*Message
	
	// MarkAsRead 标记消息为已读
	MarkAsRead(messageID string) error
	
	// MarkAsDelivered 标记消息为已送达
	MarkAsDelivered(messageID string) error
	
	// MarkFailed 标记消息为失败
	MarkFailed(messageID string) error
}

// SessionInterface 会话接口
type SessionInterface interface {
	// SendMessage 发送消息
	SendMessage(to, content string) error
	
	// ReceiveMessage 接收消息
	ReceiveMessage(from, content string) error
	
	// GetMessages 获取会话消息
	GetMessages(with string) []*Message
	
	// GetUnreadMessages 获取未读消息
	GetUnreadMessages() []*Message
	
	// MarkAsRead 标记消息为已读
	MarkAsRead(messageID string) error
	
	// DeliverOfflineMessages 投递离线消息
	DeliverOfflineMessages(deliverFn func(to, content string) error)
}

// EncryptionInterface 加密接口
type EncryptionInterface interface {
	// GetPublicKey 获取公钥
	GetPublicKey() *ecdsa.PublicKey
	
	// GetPublicKeyPEM 获取PEM格式的公钥
	GetPublicKeyPEM() (string, error)
	
	// ImportPublicKey 导入对等节点的公钥
	ImportPublicKey(peerID, pemData string) error
	
	// EncryptMessage 加密消息
	EncryptMessage(peerID, plainText string) (*EncryptedMessage, error)
	
	// DecryptMessage 解密消息
	DecryptMessage(encryptedMsg *EncryptedMessage) (string, error)
	
	// GenerateSessionKey 生成会话密钥
	GenerateSessionKey(peerID string, peerPublicKey *ecdsa.PublicKey) error
	
	// GetStats 获取加密统计信息
	GetStats() *EncryptionStats
}

// MessageIDGenerator 消息ID生成器接口
type MessageIDGenerator interface {
	// Generate 生成唯一的消息ID
	Generate() string
}

// DefaultMessageIDGenerator 默认消息ID生成器
type DefaultMessageIDGenerator struct{}

// Generate 生成消息ID
func (g *DefaultMessageIDGenerator) Generate() string {
	return generateMessageID()
}

// ChatManagerInterface 聊天管理器接口
type ChatManagerInterface interface {
	// CreateSession 创建会话
	CreateSession(username string) (SessionInterface, error)
	
	// GetSession 获取会话
	GetSession(username string) (SessionInterface, error)
	
	// GetEncryptionManager 获取加密管理器
	GetEncryptionManager() EncryptionInterface
	
	// GetMessageStore 获取消息存储
	GetMessageStore() MessageStoreInterface
}