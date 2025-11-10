package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LocalDiscoveryService 本地发现服务，用于解决多播环回不工作的问题
type LocalDiscoveryService struct {
	mu           sync.RWMutex
	logger       Logger
	running      bool
	serviceName  string
	port         int
	actualPort   int  // 实际使用的监听端口
	txtRecords   map[string]string
	discoveredPeers map[string]DiscoveredPeer
	discoveryCallback func(PeerInfo)
	broadcastConn *net.UDPConn
	listenConn   *net.UDPConn
	stopChan     chan struct{}
	discoveryPort int // 实际使用的发现端口
}

// LocalDiscoveryMessage 本地发现消息格式
type LocalDiscoveryMessage struct {
	Type        string            `json:"type"`        // "announce" 或 "query"
	ServiceName string            `json:"service_name"`
	Port        int               `json:"port"`
	IP          string            `json:"ip"`
	TxtRecords  map[string]string `json:"txt_records"`
	Timestamp   int64             `json:"timestamp"`
}

// NewLocalDiscoveryService 创建本地发现服务
func NewLocalDiscoveryService(logger Logger) *LocalDiscoveryService {
	return &LocalDiscoveryService{
		logger:          logger,
		discoveredPeers: make(map[string]DiscoveredPeer),
		txtRecords:      make(map[string]string),
		stopChan:        make(chan struct{}),
	}
}

// Configure 配置服务
func (lds *LocalDiscoveryService) Configure(serviceName string, port int, txtRecords map[string]string) {
	lds.mu.Lock()
	defer lds.mu.Unlock()
	
	lds.serviceName = serviceName
	lds.port = port
	lds.txtRecords = make(map[string]string)
	for k, v := range txtRecords {
		lds.txtRecords[k] = v
	}
}

// Start 启动本地发现服务
// findAvailablePort 智能寻找可用端口
func (lds *LocalDiscoveryService) findAvailablePort() (int, error) {
	// 尝试的端口范围：15353-15363
	basePort := 15353
	maxAttempts := 10
	
	for i := 0; i < maxAttempts; i++ {
		port := basePort + i
		addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", port))
		if err != nil {
			continue
		}
		
		// 尝试监听该端口
		conn, err := net.ListenUDP("udp4", addr)
		if err == nil {
			conn.Close() // 立即关闭，只是测试可用性
			return port, nil
		}
	}
	
	return 0, fmt.Errorf("无法找到可用端口 (尝试范围: %d-%d)", basePort, basePort+maxAttempts-1)
}

func (lds *LocalDiscoveryService) Start() error {
	lds.mu.Lock()
	defer lds.mu.Unlock()
	
	if lds.running {
		return nil
	}
	
	// 尝试启动服务，如果端口冲突则自动选择其他端口
	err := lds.startWithPortFallback()
	if err != nil {
		return err
	}
	
	lds.running = true
	
	// 启动接收循环
	go lds.receiveLoop()
	
	// 启动公告循环
	go lds.announceLoop()
	
	// 启动查询循环
	go lds.queryLoop()
	
	return nil
}

// startWithPortFallback 尝试启动服务，使用固定端口15353并启用地址重用
func (lds *LocalDiscoveryService) startWithPortFallback() error {
	// 使用固定端口15353，所有节点都在同一端口通信
	fixedPort := 15353
	err := lds.tryStartOnPort(fixedPort)
	if err == nil {
		lds.actualPort = fixedPort
		lds.logger.Infof("[LOCAL-DEBUG] 本地发现服务已启动，监听端口: %d", fixedPort)
		return nil
	}
	
	return fmt.Errorf("无法启动本地发现服务在端口 %d: %v", fixedPort, err)
}

// tryStartOnPort 尝试在指定端口启动服务
func (lds *LocalDiscoveryService) tryStartOnPort(port int) error {
	// 创建广播连接（用于发送到本地广播地址）
	broadcastAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.255.255.255:%d", port))
	if err != nil {
		return fmt.Errorf("解析广播地址失败: %v", err)
	}
	
	broadcastConn, err := net.DialUDP("udp4", nil, broadcastAddr)
	if err != nil {
		return fmt.Errorf("创建广播连接失败: %v", err)
	}
	
	// 创建监听连接，启用地址重用
	listenAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		broadcastConn.Close()
		return fmt.Errorf("解析监听地址失败: %v", err)
	}
	
	// 使用 ListenConfig 来设置 SO_REUSEADDR
    lc := &net.ListenConfig{
        Control: func(network, address string, c syscall.RawConn) error {
            return c.Control(func(fd uintptr) {
                // 启用地址重用，允许多个进程绑定同一端口（跨平台实现）
                _ = SetReuseAddr(fd)
            })
        },
    }
	
	listenConn, err := lc.ListenPacket(context.Background(), "udp4", listenAddr.String())
	if err != nil {
		broadcastConn.Close()
		return fmt.Errorf("创建监听连接失败: %v", err)
	}
	
	// 转换为 UDPConn
	udpConn, ok := listenConn.(*net.UDPConn)
	if !ok {
		broadcastConn.Close()
		listenConn.Close()
		return fmt.Errorf("无法转换为UDPConn")
	}
	
	// 成功创建连接，保存到结构体
	lds.broadcastConn = broadcastConn
	lds.listenConn = udpConn
	lds.logger.Infof("[LOCAL-DEBUG] 本地发现服务连接已创建，端口: %d", port)
	return nil
}

// isPortInUseError 检查错误是否为端口被占用
func isPortInUseError(err error) bool {
	if err == nil {
		return false
	}
	// Windows: "bind: Only one usage of each socket address"
	// Linux: "bind: address already in use"
	errorStr := err.Error()
	return stringContains(errorStr, "bind:") && 
		(stringContains(errorStr, "Only one usage") || 
		 stringContains(errorStr, "address already in use"))
}

// stringContains 检查字符串是否包含子字符串（不区分大小写）
func stringContains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	
	// 转换为小写进行比较
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	
	return strings.Contains(s, substr)
}

// Stop 停止服务
func (lds *LocalDiscoveryService) Stop() {
	lds.mu.Lock()
	defer lds.mu.Unlock()
	
	if !lds.running {
		return
	}
	
	lds.running = false
	close(lds.stopChan)
	
	if lds.broadcastConn != nil {
		lds.broadcastConn.Close()
	}
	if lds.listenConn != nil {
		lds.listenConn.Close()
	}
	
	lds.logger.Infof("本地发现服务已停止")
}

// SetDiscoveryCallback 设置发现回调
func (lds *LocalDiscoveryService) SetDiscoveryCallback(callback func(PeerInfo)) {
	lds.mu.Lock()
	defer lds.mu.Unlock()
	lds.discoveryCallback = callback
}

// GetDiscoveredPeers 获取已发现的节点
func (lds *LocalDiscoveryService) GetDiscoveredPeers() []DiscoveredPeer {
	lds.mu.RLock()
	defer lds.mu.RUnlock()
	
	var peers []DiscoveredPeer
	now := time.Now()
	for id, peer := range lds.discoveredPeers {
		// 过滤掉超过5分钟未见的节点
		if now.Sub(peer.LastSeen) > 5*time.Minute {
			delete(lds.discoveredPeers, id)
			continue
		}
		peers = append(peers, peer)
	}
	return peers
}

// announceLoop 公告循环
func (lds *LocalDiscoveryService) announceLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	// 立即发送一次公告
	lds.sendAnnounce()
	
	for {
		select {
		case <-lds.stopChan:
			return
		case <-ticker.C:
			lds.sendAnnounce()
		}
	}
}

// queryLoop 查询循环
func (lds *LocalDiscoveryService) queryLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	
	// 立即发送一次查询
	lds.sendQuery()
	
	for {
		select {
		case <-lds.stopChan:
			return
		case <-ticker.C:
			lds.sendQuery()
		}
	}
}

// receiveLoop 接收循环
func (lds *LocalDiscoveryService) receiveLoop() {
	buffer := make([]byte, 1024)
	
	for {
		select {
		case <-lds.stopChan:
			return
		default:
			lds.listenConn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, addr, err := lds.listenConn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				if lds.running {
					lds.logger.Debugf("接收数据失败: %v", err)
				}
				continue
			}
			
			lds.handleMessage(buffer[:n], addr)
		}
	}
}

// sendAnnounce 发送公告
func (lds *LocalDiscoveryService) sendAnnounce() {
	lds.mu.RLock()
	if !lds.running {
		lds.mu.RUnlock()
		return
	}
	
	msg := LocalDiscoveryMessage{
		Type:        "announce",
		ServiceName: lds.serviceName,
		Port:        lds.port,
		IP:          "127.0.0.1",
		TxtRecords:  lds.txtRecords,
		Timestamp:   time.Now().Unix(),
	}
	lds.mu.RUnlock()
	
	data, err := json.Marshal(msg)
	if err != nil {
		lds.logger.Errorf("序列化公告消息失败: %v", err)
		return
	}
	
	_, err = lds.broadcastConn.Write(data)
	if err != nil {
		lds.logger.Debugf("发送公告失败: %v", err)
    } else {
        lds.logger.Debugf("[LOCAL-DEBUG] 发送本地公告: %s (端口: %d)", lds.serviceName, lds.port)
    }
}

// sendQuery 发送查询
func (lds *LocalDiscoveryService) sendQuery() {
	lds.mu.RLock()
	if !lds.running {
		lds.mu.RUnlock()
		return
	}
	
	msg := LocalDiscoveryMessage{
		Type:        "query",
		ServiceName: lds.serviceName,
		Port:        lds.port,
		IP:          "127.0.0.1",
		TxtRecords:  lds.txtRecords,
		Timestamp:   time.Now().Unix(),
	}
	lds.mu.RUnlock()
	
	data, err := json.Marshal(msg)
	if err != nil {
		lds.logger.Errorf("序列化查询消息失败: %v", err)
		return
	}
	
	_, err = lds.broadcastConn.Write(data)
	if err != nil {
		lds.logger.Debugf("发送查询失败: %v", err)
    } else {
        lds.logger.Debugf("[LOCAL-DEBUG] 发送本地查询")
    }
}

// handleMessage 处理接收到的消息
func (lds *LocalDiscoveryService) handleMessage(data []byte, addr *net.UDPAddr) {
	var msg LocalDiscoveryMessage
	err := json.Unmarshal(data, &msg)
	if err != nil {
		lds.logger.Debugf("解析消息失败: %v", err)
		return
	}
	
    lds.logger.Debugf("[LOCAL-DEBUG] 收到本地发现消息: %s 来自 %s", msg.Type, addr.String())
	
	switch msg.Type {
	case "announce":
		lds.handleAnnounce(msg)
	case "query":
		lds.handleQuery(msg)
	}
}

// handleAnnounce 处理公告消息
func (lds *LocalDiscoveryService) handleAnnounce(msg LocalDiscoveryMessage) {
	// 跳过自己的公告
    lds.mu.RLock()
    lds.logger.Debugf("[LOCAL-DEBUG] 处理公告消息: %s (端口: %d) 来自 %s", msg.ServiceName, msg.Port, msg.IP)
    if msg.ServiceName == lds.serviceName && msg.Port == lds.port {
        lds.logger.Debugf("[LOCAL-DEBUG] 跳过自己的公告: %s:%d", msg.ServiceName, msg.Port)
        lds.mu.RUnlock()
        return
    }
	
	// 检查公钥是否相同（防止自连接）
	if peerPubKey, exists := msg.TxtRecords["pubkey"]; exists {
		if myPubKey, myExists := lds.txtRecords["pubkey"]; myExists {
			if peerPubKey == myPubKey {
				lds.mu.RUnlock()
				return
			}
		}
	}
	lds.mu.RUnlock()
	
	// 创建发现的节点
	peer := DiscoveredPeer{
		ID:       fmt.Sprintf("%s:%d", msg.ServiceName, msg.Port),
		Name:     msg.ServiceName,
		Address:  msg.IP,
		Port:     msg.Port,
		TxtData:  msg.TxtRecords,
		LastSeen: time.Now(),
	}
	
	lds.mu.Lock()
	existing, exists := lds.discoveredPeers[peer.ID]
	if exists {
		// 更新现有节点
		existing.LastSeen = time.Now()
		existing.TxtData = msg.TxtRecords
		lds.discoveredPeers[peer.ID] = existing
	} else {
		// 添加新节点
		lds.discoveredPeers[peer.ID] = peer
		// 只在首次发现时输出日志
		lds.logger.Infof("发现新的本地节点: %s (%s:%d)", peer.Name, peer.Address, peer.Port)
		
		// 调用回调
		if lds.discoveryCallback != nil {
			// 转换为PeerInfo格式
			peerInfo := PeerInfo{
				ID:        peer.ID,
				Username:  peer.Name,
				PublicKey: peer.TxtData["pubkey"], // 从TXT记录中获取公钥
				Address:   peer.Address,
				Port:      peer.Port,
				IsOnline:  true,
				LastSeen:  peer.LastSeen,
				Metadata:  peer.TxtData,
			}
			go lds.discoveryCallback(peerInfo)
		}
	}
	lds.mu.Unlock()
}

// handleQuery 处理查询消息
func (lds *LocalDiscoveryService) handleQuery(msg LocalDiscoveryMessage) {
	// 如果查询匹配我们的服务，发送公告作为响应
	lds.mu.RLock()
	if lds.running {
		lds.mu.RUnlock()
		lds.logger.Debugf("收到查询，发送公告响应")
		go lds.sendAnnounce()
	} else {
		lds.mu.RUnlock()
	}
}

// DiscoverPeers 执行一次发现
func (lds *LocalDiscoveryService) DiscoverPeers() {
	lds.sendQuery()
	time.Sleep(100 * time.Millisecond)
	lds.sendAnnounce()
}