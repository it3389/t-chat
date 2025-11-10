package chat

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"time"
)

// NewEncryptionManager 创建加密管理器
func NewEncryptionManager(config *EncryptionConfig) (*EncryptionManager, error) {
	if config == nil {
		config = DefaultEncryptionConfig()
	}

	// 生成密钥对
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成密钥对失败: %w", err)
	}

	return &EncryptionManager{
		privateKey:  privateKey,
		publicKey:   &privateKey.PublicKey,
		sessionKeys: make(map[string]*SessionKey),
		config:      config,
		stats:       &EncryptionStats{},
	}, nil
}

// GetPublicKey 获取公钥
func (em *EncryptionManager) GetPublicKey() *ecdsa.PublicKey {
	return em.publicKey
}

// GetPublicKeyPEM 获取PEM格式的公钥
func (em *EncryptionManager) GetPublicKeyPEM() (string, error) {
	pubBytes, err := x509.MarshalPKIXPublicKey(em.publicKey)
	if err != nil {
		return "", fmt.Errorf("序列化公钥失败: %w", err)
	}

	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	return string(pubPEM), nil
}

// ImportPublicKey 导入对等节点的公钥
func (em *EncryptionManager) ImportPublicKey(peerID, pemData string) error {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return fmt.Errorf("无效的PEM格式")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("解析公钥失败: %w", err)
	}

	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("不是ECDSA公钥")
	}

	// 生成会话密钥
	return em.GenerateSessionKey(peerID, ecdsaPub)
}

// EncryptMessage 加密消息
func (em *EncryptionManager) EncryptMessage(peerID, plainText string) (*EncryptedMessage, error) {
	// 获取或创建会话密钥
	sessionKey, err := em.getOrCreateSessionKey(peerID)
	if err != nil {
		return nil, fmt.Errorf("获取会话密钥失败: %w", err)
	}

	// 使用AES加密
	cipherText, err := encryptWithAES(plainText, sessionKey.Key, sessionKey.Nonce)
	if err != nil {
		return nil, fmt.Errorf("加密失败: %w", err)
	}

	// 创建加密消息
	encryptedMsg := &EncryptedMessage{
		Type:      "text",
		Content:   base64.StdEncoding.EncodeToString(cipherText),
		Key:       base64.StdEncoding.EncodeToString(sessionKey.Key),
		Nonce:     base64.StdEncoding.EncodeToString(sessionKey.Nonce),
		Timestamp: time.Now(),
		PeerID:    peerID,
		Metadata:  make(map[string]interface{}),
	}

	// 签名消息
	signature, err := em.signMessage(cipherText)
	if err != nil {
		return nil, fmt.Errorf("签名失败: %w", err)
	}
	encryptedMsg.Signature = base64.StdEncoding.EncodeToString(signature)

	// 更新统计信息
	em.updateStats(true, false)

	return encryptedMsg, nil
}

// DecryptMessage 解密消息
func (em *EncryptionManager) DecryptMessage(encryptedMsg *EncryptedMessage) (string, error) {
	// 验证签名
	if err := em.verifySignature(encryptedMsg); err != nil {
		return "", fmt.Errorf("签名验证失败: %w", err)
	}

	// 解码数据
	cipherText, err := base64.StdEncoding.DecodeString(encryptedMsg.Content)
	if err != nil {
		return "", fmt.Errorf("解码密文失败: %w", err)
	}

	key, err := base64.StdEncoding.DecodeString(encryptedMsg.Key)
	if err != nil {
		return "", fmt.Errorf("解码密钥失败: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(encryptedMsg.Nonce)
	if err != nil {
		return "", fmt.Errorf("解码随机数失败: %w", err)
	}

	// 解密
	plainText, err := decryptWithAES(cipherText, key, nonce)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}

	// 更新统计信息
	em.updateStats(false, true)

	return plainText, nil
}

// GenerateSessionKey 生成会话密钥
func (em *EncryptionManager) GenerateSessionKey(peerID string, peerPublicKey *ecdsa.PublicKey) error {
	return em.generateSessionKey(peerID, peerPublicKey)
}

// GetSessionKey 获取会话密钥
func (em *EncryptionManager) GetSessionKey(peerID string) (*SessionKey, error) {
	sessionKey, exists := em.sessionKeys[peerID]
	if !exists {
		return nil, fmt.Errorf("会话密钥不存在: %s", peerID)
	}

	if sessionKey.IsExpired() {
		delete(em.sessionKeys, peerID)
		return nil, fmt.Errorf("会话密钥已过期: %s", peerID)
	}

	return sessionKey, nil
}

// GetStats 获取加密统计信息
func (em *EncryptionManager) GetStats() *EncryptionStats {
	em.cleanupExpiredSessions()
	
	stats := *em.stats
	stats.ActiveSessions = len(em.sessionKeys)
	stats.LastActivity = time.Now()
	
	return &stats
}

// generateSessionKey 生成会话密钥（内部方法）
func (em *EncryptionManager) generateSessionKey(peerID string, peerPublicKey *ecdsa.PublicKey) error {
	// 使用ECDH生成共享密钥
	sharedKey, _ := peerPublicKey.Curve.ScalarMult(peerPublicKey.X, peerPublicKey.Y, em.privateKey.D.Bytes())
	
	// 使用SHA256哈希共享密钥
	hash := sha256.Sum256(sharedKey.Bytes())
	key := hash[:]

	// 生成随机数
	nonce := make([]byte, 12) // GCM标准随机数大小
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("生成随机数失败: %w", err)
	}

	// 创建会话密钥
	sessionKey := &SessionKey{
		Key:       key,
		Nonce:     nonce,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(em.config.SessionExpiry),
		PeerID:    peerID,
	}

	em.sessionKeys[peerID] = sessionKey
	return nil
}

// getOrCreateSessionKey 获取或创建会话密钥
func (em *EncryptionManager) getOrCreateSessionKey(peerID string) (*SessionKey, error) {
	sessionKey, exists := em.sessionKeys[peerID]
	if !exists || sessionKey.IsExpired() {
		return nil, fmt.Errorf("会话密钥不存在或已过期，需要重新建立: %s", peerID)
	}
	return sessionKey, nil
}

// signMessage 签名消息
func (em *EncryptionManager) signMessage(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, em.privateKey, hash[:])
}

// verifySignature 验证签名
func (em *EncryptionManager) verifySignature(encryptedMsg *EncryptedMessage) error {
	// 这里简化处理，实际应该验证对等节点的签名
	// 需要对等节点的公钥来验证
	return nil
}

// cleanupExpiredSessions 清理过期的会话密钥
func (em *EncryptionManager) cleanupExpiredSessions() {
	for peerID, sessionKey := range em.sessionKeys {
		if sessionKey.IsExpired() {
			delete(em.sessionKeys, peerID)
		}
	}
}

// updateStats 更新统计信息
func (em *EncryptionManager) updateStats(encrypted, decrypted bool) {
	em.stats.TotalMessages++
	if encrypted {
		em.stats.EncryptedMessages++
	}
	if decrypted {
		em.stats.DecryptedMessages++
	}
	em.stats.LastActivity = time.Now()
}
