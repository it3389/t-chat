package chat

import (
	"crypto/ecdsa"
	"time"
)

// EncryptionConfig 加密配置
type EncryptionConfig struct {
	Algorithm            string        `json:"algorithm"`              // 加密算法：AES-256-GCM, ChaCha20-Poly1305
	KeySize              int           `json:"key_size"`               // 密钥大小（位）
	SessionExpiry        time.Duration `json:"session_expiry"`         // 会话密钥过期时间
	EnableForwardSecrecy bool          `json:"enable_forward_secrecy"` // 启用前向保密
}

// SessionKey 会话密钥
type SessionKey struct {
	Key       []byte    `json:"key"`        // 密钥数据
	Nonce     []byte    `json:"nonce"`      // 随机数
	CreatedAt time.Time `json:"created_at"` // 创建时间
	ExpiresAt time.Time `json:"expires_at"` // 过期时间
	PeerID    string    `json:"peer_id"`    // 对等节点ID
}

// EncryptionStats 加密统计信息
type EncryptionStats struct {
	TotalMessages     int64     `json:"total_messages"`
	EncryptedMessages int64     `json:"encrypted_messages"`
	DecryptedMessages int64     `json:"decrypted_messages"`
	ActiveSessions    int       `json:"active_sessions"`
	LastActivity      time.Time `json:"last_activity"`
}

// EncryptedMessage 加密消息
type EncryptedMessage struct {
	Type      string                 `json:"type"`      // 消息类型
	Content   string                 `json:"content"`   // 加密内容（Base64）
	Key       string                 `json:"key"`       // 加密密钥（Base64）
	Nonce     string                 `json:"nonce"`     // 随机数（Base64）
	Signature string                 `json:"signature"` // 数字签名（Base64）
	Timestamp time.Time              `json:"timestamp"` // 时间戳
	PeerID    string                 `json:"peer_id"`   // 对等节点ID
	Metadata  map[string]interface{} `json:"metadata"`  // 元数据
}

// EncryptionManager 加密管理器
// 负责管理端到端加密的整个生命周期
type EncryptionManager struct {
	// 密钥对
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey

	// 会话密钥
	sessionKeys map[string]*SessionKey

	// 配置
	config *EncryptionConfig

	// 统计信息
	stats *EncryptionStats
}

// IsExpired 检查会话密钥是否过期
func (sk *SessionKey) IsExpired() bool {
	return time.Now().After(sk.ExpiresAt)
}

// IsValid 检查会话密钥是否有效
func (sk *SessionKey) IsValid() bool {
	return !sk.IsExpired() && len(sk.Key) > 0 && len(sk.Nonce) > 0
}

// DefaultEncryptionConfig 返回默认加密配置
func DefaultEncryptionConfig() *EncryptionConfig {
	return &EncryptionConfig{
		Algorithm:            "AES-256-GCM",
		KeySize:              256,
		SessionExpiry:        time.Hour, // 1小时
		EnableForwardSecrecy: true,
	}
}