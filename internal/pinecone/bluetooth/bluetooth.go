package bluetooth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"t-chat/internal/pinecone/router"
	"t-chat/internal/pinecone/types"
)

// PlatformBluetoothService 平台特定的蓝牙服务接口
// 新增 SendData 方法
type PlatformBluetoothService interface {
	scanForPineconeDevices()
	connectToPeer(peer *BluetoothPeer)
	isPineconeDevice(name, address string) bool
	SendData(peer *BluetoothPeer, data []byte) error
}

// BluetoothService Pinecone 蓝牙服务
type BluetoothService struct {
	router     *router.Router
	logger     types.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	mutex      sync.RWMutex
	discovered map[string]*BluetoothPeer
	peersMutex sync.RWMutex
	platform   PlatformBluetoothService
	// 新增多连接管理字段
	connectionPool map[string]*BluetoothConnection
	poolMutex      sync.RWMutex
	stats          *BluetoothStats
}

// BluetoothConnection 蓝牙连接信息
type BluetoothConnection struct {
	Peer           *BluetoothPeer
	ConnectedAt    time.Time
	LastActivity   time.Time
	MessageCount   int64
	BytesSent      int64
	BytesReceived  int64
	ReconnectCount int
	Status         string // "connected", "disconnected", "reconnecting"
}

// BluetoothStats 蓝牙统计信息
type BluetoothStats struct {
	TotalConnections   int64
	ActiveConnections  int64
	TotalMessagesSent  int64
	TotalMessagesRcvd  int64
	TotalBytesSent     int64
	TotalBytesReceived int64
	ReconnectAttempts  int64
	LastUpdated        time.Time
}

// BluetoothPeer 蓝牙对等节点信息
type BluetoothPeer struct {
	PublicKey string
	Address   string
	Name      string
	LastSeen  time.Time
	Connected bool
	// 新增握手相关字段
	HandshakeCompleted bool
	RemotePublicKey    string
	HandshakeTime      time.Time
	// 新增加密相关字段
	SharedKey []byte
	Encrypted bool
}

// NewBluetoothService 创建新的蓝牙服务
func NewBluetoothService(logger types.Logger, r *router.Router) *BluetoothService {
	bs := &BluetoothService{
		router:         r,
		logger:         logger,
		discovered:     make(map[string]*BluetoothPeer),
		connectionPool: make(map[string]*BluetoothConnection),
		stats:          &BluetoothStats{},
	}

	// 根据平台创建特定的蓝牙服务
	bs.platform = createPlatformBluetoothService(bs)

	return bs
}

// Start 启动蓝牙服务
func (bs *BluetoothService) Start() error {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	if bs.started {
		return nil
	}

	bs.ctx, bs.cancel = context.WithCancel(context.Background())
	bs.started = true

	// 启动蓝牙发现
	go bs.startDiscovery()

	// 启动蓝牙连接管理
	go bs.startConnectionManager()

	// 自动发现与自动连接
	go bs.autoConnectLoop()


	return nil
}

// Stop 停止蓝牙服务
func (bs *BluetoothService) Stop() error {
	bs.mutex.Lock()
	defer bs.mutex.Unlock()

	if !bs.started {
		return nil
	}

	bs.started = false
	if bs.cancel != nil {
		bs.cancel()
	}

	// 清理连接池
	bs.poolMutex.Lock()
	for key := range bs.connectionPool {
		delete(bs.connectionPool, key)
	}
	bs.poolMutex.Unlock()

	// 清理发现的设备
	bs.peersMutex.Lock()
	for key := range bs.discovered {
		delete(bs.discovered, key)
	}
	bs.peersMutex.Unlock()

	bs.logger.Println("蓝牙服务已停止")
	return nil
}

// startDiscovery 启动蓝牙设备发现
func (bs *BluetoothService) startDiscovery() {
	ticker := time.NewTicker(30 * time.Second) // 每30秒扫描一次
	defer ticker.Stop()

	for {
		select {
		case <-bs.ctx.Done():
			return
		case <-ticker.C:
			bs.scanForPineconeDevices()
		}
	}
}

// scanForPineconeDevices 扫描 Pinecone 蓝牙设备
func (bs *BluetoothService) scanForPineconeDevices() {
	// 使用平台特定的实现
	bs.platform.scanForPineconeDevices()
}

// startConnectionManager 启动连接管理器
func (bs *BluetoothService) startConnectionManager() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-bs.ctx.Done():
			return
		case <-ticker.C:
			bs.manageConnections()
		}
	}
}

// manageConnections 管理蓝牙连接
func (bs *BluetoothService) manageConnections() {
	bs.peersMutex.RLock()
	peers := make([]*BluetoothPeer, 0, len(bs.discovered))
	for _, peer := range bs.discovered {
		peers = append(peers, peer)
	}
	bs.peersMutex.RUnlock()

	for _, peer := range peers {
		if !peer.Connected && time.Since(peer.LastSeen) < 2*time.Minute {
			go bs.connectToPeer(peer)
		}
	}
}

// connectToPeer 连接到蓝牙对等节点
func (bs *BluetoothService) connectToPeer(peer *BluetoothPeer) {
	// 使用平台特定的实现
	bs.platform.connectToPeer(peer)
}

// GetDiscoveredPeers 获取发现的蓝牙设备
func (bs *BluetoothService) GetDiscoveredPeers() []*BluetoothPeer {
	bs.peersMutex.RLock()
	defer bs.peersMutex.RUnlock()

	peers := make([]*BluetoothPeer, 0, len(bs.discovered))
	for _, peer := range bs.discovered {
		peers = append(peers, peer)
	}
	return peers
}

// IsStarted 检查蓝牙服务是否已启动
func (bs *BluetoothService) IsStarted() bool {
	bs.mutex.RLock()
	defer bs.mutex.RUnlock()
	return bs.started
}

// GetConnectionStats 获取连接统计信息
func (bs *BluetoothService) GetConnectionStats() *BluetoothStats {
	bs.poolMutex.RLock()
	defer bs.poolMutex.RUnlock()

	activeCount := int64(0)
	for _, conn := range bs.connectionPool {
		if conn.Status == "connected" {
			activeCount++
		}
	}

	bs.stats.ActiveConnections = activeCount
	bs.stats.LastUpdated = time.Now()
	return bs.stats
}

// AddConnection 添加连接到连接池
func (bs *BluetoothService) AddConnection(peer *BluetoothPeer) {
	bs.poolMutex.Lock()
	defer bs.poolMutex.Unlock()

	conn := &BluetoothConnection{
		Peer:         peer,
		ConnectedAt:  time.Now(),
		LastActivity: time.Now(),
		Status:       "connected",
	}

	bs.connectionPool[peer.PublicKey] = conn
	bs.stats.TotalConnections++
	bs.logger.Printf("添加蓝牙连接到连接池: %s", peer.Name)
}

// RemoveConnection 从连接池移除连接
func (bs *BluetoothService) RemoveConnection(peer *BluetoothPeer) {
	bs.poolMutex.Lock()
	defer bs.poolMutex.Unlock()

	if conn, exists := bs.connectionPool[peer.PublicKey]; exists {
		bs.stats.TotalMessagesSent += conn.MessageCount
		bs.stats.TotalBytesSent += conn.BytesSent
		bs.stats.TotalBytesReceived += conn.BytesReceived
		delete(bs.connectionPool, peer.PublicKey)
		bs.logger.Printf("从连接池移除蓝牙连接: %s", peer.Name)
	}
}

// UpdateConnectionActivity 更新连接活动状态
func (bs *BluetoothService) UpdateConnectionActivity(peer *BluetoothPeer, bytesSent, bytesReceived int64) {
	bs.poolMutex.Lock()
	defer bs.poolMutex.Unlock()

	if conn, exists := bs.connectionPool[peer.PublicKey]; exists {
		conn.LastActivity = time.Now()
		conn.MessageCount++
		conn.BytesSent += bytesSent
		conn.BytesReceived += bytesReceived
	}
}

// GetActiveConnections 获取活跃连接列表
func (bs *BluetoothService) GetActiveConnections() []*BluetoothConnection {
	bs.poolMutex.RLock()
	defer bs.poolMutex.RUnlock()

	active := make([]*BluetoothConnection, 0)
	for _, conn := range bs.connectionPool {
		if conn.Status == "connected" {
			active = append(active, conn)
		}
	}
	return active
}

// SendPineconeMessageToPeer 通过蓝牙发送 Pinecone 消息
func (bs *BluetoothService) SendPineconeMessageToPeer(peer *BluetoothPeer, msg []byte) error {
	bs.logger.Printf("[Send] To: %s, Encrypted: %v, MsgLen: %d, Msg(hex): %x", peer.Name, peer.Encrypted, len(msg), msg)
	// 更新连接活动状态
	bs.UpdateConnectionActivity(peer, int64(len(msg)), 0)
	// 如果握手完成且已协商密钥，则加密消息
	if peer.HandshakeCompleted && peer.Encrypted && len(peer.SharedKey) > 0 {
		encryptedMsg, err := bs.encryptMessage(msg, peer.SharedKey)
		if err != nil {
			bs.logger.Printf("加密消息失败: %v", err)
			return err
		}
		bs.logger.Printf("[Send] 发送加密消息给 %s, 密文长度: %d, 密文(hex): %x", peer.Name, len(encryptedMsg), encryptedMsg)
		return bs.platform.SendData(peer, encryptedMsg)
	}
	// 未加密，直接发送
	bs.logger.Printf("[Send] 发送明文消息给 %s", peer.Name)
	return bs.platform.SendData(peer, msg)
}

// SendPineconeMessageToPeerFromOverlay 从 overlay 网络发送消息到蓝牙节点
func (bs *BluetoothService) SendPineconeMessageToPeerFromOverlay(peer *BluetoothPeer, msg []byte) error {
	bs.logger.Printf("overlay 网络 -> 蓝牙节点 %s", peer.Name)
	return bs.SendPineconeMessageToPeer(peer, msg)
}

// GetBluetoothPeers 获取所有蓝牙节点，供 overlay 网络查询
func (bs *BluetoothService) GetBluetoothPeers() []*BluetoothPeer {
	bs.peersMutex.RLock()
	defer bs.peersMutex.RUnlock()

	peers := make([]*BluetoothPeer, 0, len(bs.discovered))
	for _, peer := range bs.discovered {
		if peer.Connected && peer.HandshakeCompleted {
			peers = append(peers, peer)
		}
	}
	return peers
}

// FindBluetoothPeerByPublicKey 根据公钥查找蓝牙节点
func (bs *BluetoothService) FindBluetoothPeerByPublicKey(publicKey string) *BluetoothPeer {
	bs.peersMutex.RLock()
	defer bs.peersMutex.RUnlock()

	for _, peer := range bs.discovered {
		if peer.RemotePublicKey == publicKey && peer.Connected && peer.HandshakeCompleted {
			return peer
		}
	}
	return nil
}

// encryptMessage 使用 AES-GCM 加密消息
func (bs *BluetoothService) encryptMessage(msg []byte, key []byte) ([]byte, error) {
	bs.logger.Printf("[Encrypt] 明文长度: %d, 明文(hex): %x, 密钥hash: %x", len(msg), msg, key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nonce, nonce, msg, nil)
	bs.logger.Printf("[Encrypt] 密文长度: %d, 密文(hex): %x", len(ciphertext), ciphertext)
	return ciphertext, nil
}

// decryptMessage 使用 AES-GCM 解密消息
func (bs *BluetoothService) decryptMessage(encryptedMsg []byte, key []byte) ([]byte, error) {
	bs.logger.Printf("[Decrypt] 密文长度: %d, 密文(hex): %x, 密钥hash: %x", len(encryptedMsg), encryptedMsg, key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(encryptedMsg) < nonceSize {
		return nil, fmt.Errorf("密文太短")
	}
	nonce, ciphertext := encryptedMsg[:nonceSize], encryptedMsg[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		bs.logger.Printf("[Decrypt] 解密失败: %v", err)
		return nil, err
	}
	bs.logger.Printf("[Decrypt] 解密成功, 明文长度: %d, 明文(hex): %x", len(plaintext), plaintext)
	return plaintext, nil
}

// negotiateSharedKey 协商共享密钥
func (bs *BluetoothService) negotiateSharedKey(peer *BluetoothPeer) {
    if !peer.HandshakeCompleted {
        return
    }
    // 如果平台层（例如 Windows）已经通过 ECDH 设置了共享密钥，则不再覆盖
    if len(peer.SharedKey) > 0 {
        bs.logger.Printf("[Key] 已存在共享密钥，跳过旧派生逻辑")
        return
    }
    if bs.router != nil {
        if idGetter, ok := interface{}(bs.router).(interface {
            GetLocalPublicKey() string
        }); ok {
            localPubkey := idGetter.GetLocalPublicKey()
            combined := localPubkey + ":" + peer.RemotePublicKey
            hash := sha256.Sum256([]byte(combined))
            peer.SharedKey = hash[:32]
            peer.Encrypted = true
            bs.logger.Printf("[Key] 与 %s 协商共享密钥完成, 密钥hash: %x", peer.Name, peer.SharedKey)
        }
    }
}

// SendPineconeIdentity 自动发送本地身份信息
func (bs *BluetoothService) SendPineconeIdentity(peer *BluetoothPeer) {
	if bs.router != nil {
		if idGetter, ok := interface{}(bs.router).(interface {
			GetLocalPublicKey() string
		}); ok {
			pubkey := idGetter.GetLocalPublicKey()
			bs.logger.Printf("自动发送本地身份信息给 %s: %s", peer.Name, pubkey)
			bs.SendPineconeMessageToPeer(peer, []byte(pubkey))
			// 发送握手确认
			bs.SendHandshakeConfirmation(peer)
		}
	}
}

// SendHandshakeConfirmation 发送握手确认
func (bs *BluetoothService) SendHandshakeConfirmation(peer *BluetoothPeer) {
	bs.logger.Printf("发送握手确认给 %s", peer.Name)
	// 发送握手确认消息（格式：HANDSHAKE_CONFIRM:本地公钥）
	if bs.router != nil {
		if idGetter, ok := interface{}(bs.router).(interface {
			GetLocalPublicKey() string
		}); ok {
			pubkey := idGetter.GetLocalPublicKey()
			handshakeMsg := fmt.Sprintf("HANDSHAKE_CONFIRM:%s", pubkey)
			bs.SendPineconeMessageToPeer(peer, []byte(handshakeMsg))
		}
	}
}

// handleBluetoothData 处理收到的蓝牙数据
func (bs *BluetoothService) handleBluetoothData(peer *BluetoothPeer, data []byte) {
	bs.logger.Printf("[Recv] From: %s, Encrypted: %v, DataLen: %d, Data(hex): %x", peer.Name, peer.Encrypted, len(data), data)
	// 如果已加密，先解密
	var plaintext []byte
	var err error
	if peer.Encrypted && len(peer.SharedKey) > 0 {
		plaintext, err = bs.decryptMessage(data, peer.SharedKey)
		if err != nil {
			bs.logger.Printf("解密消息失败: %v", err)
			return
		}
		bs.logger.Printf("[Recv] 收到 %s 的加密消息, 明文(hex): %x", peer.Name, plaintext)
	} else {
		plaintext = data
	}

	msg := string(plaintext)
	bs.logger.Printf("[Recv] 收到 %s 的消息: %s", peer.Name, msg)

	// 处理握手确认消息
	if strings.HasPrefix(msg, "HANDSHAKE_CONFIRM:") {
		parts := strings.SplitN(msg, ":", 2)
		if len(parts) == 2 {
			peer.RemotePublicKey = parts[1]
			peer.HandshakeCompleted = true
			peer.HandshakeTime = time.Now()
			bs.logger.Printf("握手完成: %s 的公钥为 %s", peer.Name, peer.RemotePublicKey)
			// 握手完成后协商共享密钥
			bs.negotiateSharedKey(peer)
		}
		return
	}

	// 处理身份信息（首次收到）
	if !peer.HandshakeCompleted && !strings.HasPrefix(msg, "HANDSHAKE_CONFIRM:") {
		peer.RemotePublicKey = msg
		bs.logger.Printf("收到 %s 的身份信息: %s", peer.Name, peer.RemotePublicKey)
		// 自动回复握手确认
		bs.SendHandshakeConfirmation(peer)
		return
	}

	// 处理 Pinecone 消息并转发到 overlay 网络
	bs.routePineconeMessageFromBluetooth(peer, plaintext)
}

// routePineconeMessageFromBluetooth 将蓝牙收到的 Pinecone 消息路由到 overlay 网络
func (bs *BluetoothService) routePineconeMessageFromBluetooth(peer *BluetoothPeer, data []byte) {
	// 更新连接活动状态
	bs.UpdateConnectionActivity(peer, 0, int64(len(data)))

	if bs.router != nil {
		if handler, ok := interface{}(bs.router).(interface {
			HandleIncomingMessageFromBluetooth(peer *BluetoothPeer, data []byte)
		}); ok {
			bs.logger.Printf("将蓝牙消息路由到 overlay 网络: %s -> overlay", peer.Name)
			handler.HandleIncomingMessageFromBluetooth(peer, data)
		} else {
			// 兼容旧接口
			if handler, ok := interface{}(bs.router).(interface {
				HandleIncomingBluetoothMessage(peer *BluetoothPeer, data []byte)
			}); ok {
				handler.HandleIncomingBluetoothMessage(peer, data)
			}
		}
	}
}

// createDefaultPlatformBluetoothService 创建默认平台蓝牙服务
func createDefaultPlatformBluetoothService(bs *BluetoothService) PlatformBluetoothService {
	// 默认实现，用于不支持蓝牙的平台
	return &DefaultBluetoothService{bs}
}

// DefaultBluetoothService 默认蓝牙服务实现
type DefaultBluetoothService struct {
	*BluetoothService
}

func (dbs *DefaultBluetoothService) scanForPineconeDevices() {
	// 默认平台不支持蓝牙功能，静默处理
}

func (dbs *DefaultBluetoothService) connectToPeer(peer *BluetoothPeer) {
	// 默认平台不支持蓝牙连接，静默处理
}

func (dbs *DefaultBluetoothService) isPineconeDevice(name, address string) bool {
	return false
}

// SendData 默认实现，返回不支持
func (dbs *DefaultBluetoothService) SendData(peer *BluetoothPeer, data []byte) error {
	return fmt.Errorf("当前平台不支持蓝牙数据发送")
}

// 自动发现与自动连接循环
func (bs *BluetoothService) autoConnectLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-bs.ctx.Done():
			return
		case <-ticker.C:
			bs.peersMutex.RLock()
			for _, peer := range bs.discovered {
				if !peer.Connected && bs.platform != nil {
					// bs.logger.Printf("自动连接 Pinecone 蓝牙设备: %s (%s)", peer.Name, peer.Address)
					go bs.platform.connectToPeer(peer)
				}
			}
			bs.peersMutex.RUnlock()

			// 检查连接状态和重连
			bs.checkConnectionHealth()
		}
	}
}

// checkConnectionHealth 检查连接健康状态
func (bs *BluetoothService) checkConnectionHealth() {
	bs.poolMutex.RLock()
	connections := make([]*BluetoothConnection, 0, len(bs.connectionPool))
	for _, conn := range bs.connectionPool {
		connections = append(connections, conn)
	}
	bs.poolMutex.RUnlock()

	for _, conn := range connections {
		// 检查连接是否超时（超过5分钟无活动）
		if time.Since(conn.LastActivity) > 5*time.Minute {
			bs.logger.Printf("蓝牙连接超时，尝试重连: %s", conn.Peer.Name)
			conn.Status = "reconnecting"
			conn.ReconnectCount++
			bs.stats.ReconnectAttempts++

			// 异步重连
			go bs.reconnectPeer(conn.Peer)
		}
	}
}

// reconnectPeer 重连对等节点
func (bs *BluetoothService) reconnectPeer(peer *BluetoothPeer) {
	bs.logger.Printf("开始重连蓝牙设备: %s", peer.Name)

	// 标记为断开
	peer.Connected = false

	// 尝试重新连接
	if bs.platform != nil {
		bs.platform.connectToPeer(peer)
	}

	// 如果重连成功，更新状态
	if peer.Connected {
		bs.logger.Printf("重连成功: %s", peer.Name)
		if conn, exists := bs.connectionPool[peer.PublicKey]; exists {
			conn.Status = "connected"
			conn.LastActivity = time.Now()
		}
	} else {
		bs.logger.Printf("重连失败: %s", peer.Name)
	}
}
