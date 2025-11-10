package chat

import (
	"fmt"
	"sync"
)

// ChatManager 聊天管理器
// 负责协调消息存储、会话管理和加密功能
type ChatManager struct {
	messageStore      MessageStoreInterface
	encryptionManager EncryptionInterface
	sessions          map[string]SessionInterface
	mutex             sync.RWMutex
	config            *ChatConfig
}

// ChatConfig 聊天配置
type ChatConfig struct {
	EnableEncryption bool                `json:"enable_encryption"`
	MessageStorePath string              `json:"message_store_path"`
	EncryptionConfig *EncryptionConfig   `json:"encryption_config"`
}

// NewChatManager 创建新的聊天管理器
func NewChatManager(config *ChatConfig) (*ChatManager, error) {
	if config == nil {
		config = &ChatConfig{
			EnableEncryption: true,
			MessageStorePath: "messages/messages.json",
			EncryptionConfig: &EncryptionConfig{
				Algorithm:            "AES-256-GCM",
				KeySize:              256,
				SessionExpiry:        3600, // 1小时
				EnableForwardSecrecy: true,
			},
		}
	}

	// 创建消息存储
	messageStore := NewMessageStore()
	if config.MessageStorePath != "" {
		messageStore.filePath = config.MessageStorePath
	}

	// 创建加密管理器
	var encryptionManager EncryptionInterface
	if config.EnableEncryption {
		em, err := NewEncryptionManager(config.EncryptionConfig)
		if err != nil {
			return nil, fmt.Errorf("创建加密管理器失败: %w", err)
		}
		encryptionManager = em
	}

	return &ChatManager{
		messageStore:      messageStore,
		encryptionManager: encryptionManager,
		sessions:          make(map[string]SessionInterface),
		config:            config,
	}, nil
}

// CreateSession 创建会话
func (cm *ChatManager) CreateSession(username string) (SessionInterface, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if session, exists := cm.sessions[username]; exists {
		return session, nil
	}

	session := NewSession(username, cm.messageStore)
	cm.sessions[username] = session

	return session, nil
}

// GetSession 获取会话
func (cm *ChatManager) GetSession(username string) (SessionInterface, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	session, exists := cm.sessions[username]
	if !exists {
		return nil, fmt.Errorf("会话不存在: %s", username)
	}

	return session, nil
}

// GetEncryptionManager 获取加密管理器
func (cm *ChatManager) GetEncryptionManager() EncryptionInterface {
	return cm.encryptionManager
}

// GetMessageStore 获取消息存储
func (cm *ChatManager) GetMessageStore() MessageStoreInterface {
	return cm.messageStore
}

// SendEncryptedMessage 发送加密消息
func (cm *ChatManager) SendEncryptedMessage(from, to, content string) error {
	if !cm.config.EnableEncryption || cm.encryptionManager == nil {
		return fmt.Errorf("加密功能未启用")
	}

	// 加密消息
	encryptedMsg, err := cm.encryptionManager.EncryptMessage(to, content)
	if err != nil {
		return fmt.Errorf("加密消息失败: %w", err)
	}

	// 获取会话
	session, err := cm.GetSession(from)
	if err != nil {
		session, err = cm.CreateSession(from)
		if err != nil {
			return fmt.Errorf("创建会话失败: %w", err)
		}
	}

	// 发送加密消息（这里简化处理，实际应该序列化encryptedMsg）
	return session.SendMessage(to, fmt.Sprintf("encrypted:%s", encryptedMsg.Content))
}

// ReceiveEncryptedMessage 接收加密消息
func (cm *ChatManager) ReceiveEncryptedMessage(from, to string, encryptedMsg *EncryptedMessage) error {
	if !cm.config.EnableEncryption || cm.encryptionManager == nil {
		return fmt.Errorf("加密功能未启用")
	}

	// 解密消息
	plainText, err := cm.encryptionManager.DecryptMessage(encryptedMsg)
	if err != nil {
		return fmt.Errorf("解密消息失败: %w", err)
	}

	// 获取会话
	session, err := cm.GetSession(to)
	if err != nil {
		session, err = cm.CreateSession(to)
		if err != nil {
			return fmt.Errorf("创建会话失败: %w", err)
		}
	}

	// 接收解密后的消息
	return session.ReceiveMessage(from, plainText)
}

// GetChatStats 获取聊天统计信息
func (cm *ChatManager) GetChatStats() map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	stats := map[string]interface{}{
		"active_sessions": len(cm.sessions),
		"encryption_enabled": cm.config.EnableEncryption,
	}

	if cm.encryptionManager != nil {
		stats["encryption_stats"] = cm.encryptionManager.GetStats()
	}

	return stats
}

// Cleanup 清理资源
func (cm *ChatManager) Cleanup() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// 清理会话
	cm.sessions = make(map[string]SessionInterface)
}