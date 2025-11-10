package network

import (
    "bytes"
    "context"
    "fmt"
    "net"
    "strconv"
    "strings"
    "sync"
    "time"
)

// MDNSService mDNS服务
// 实现 MDNSServiceInterface 接口
type MDNSService struct {
	*NetworkService
	
	serviceName    string
	serviceType    string
	domain         string
	port           int
	txtRecords     map[string]string
	discoveredPeers map[string]*DiscoveredPeer
	announceInterval time.Duration
	queryInterval   time.Duration
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	conn           *net.UDPConn
	isAnnouncing   bool
	isQuerying     bool
	discoveryCallback func(peer PeerInfo)
	lastDiscoveryTime time.Time
	discoveryMinInterval time.Duration
	localDiscovery  *LocalDiscoveryService
}

// DiscoveredPeer 发现的对等节点
type DiscoveredPeer struct {
	ID       string
	Name     string
	Address  string
	Port     int
	TxtData  map[string]string
	LastSeen time.Time
}

// 确保 MDNSService 实现了 MDNSServiceInterface 接口
var _ MDNSServiceInterface = (*MDNSService)(nil)

// NewMDNSService 创建mDNS服务
func NewMDNSService(config *NetworkConfig, logger Logger) *MDNSService {
	ctx, cancel := context.WithCancel(context.Background())
	
	baseService := NewNetworkService("mdns", config, logger)
	
	return &MDNSService{
			NetworkService:   baseService,
			serviceName:      config.ServiceName,
			serviceType:      "_tchat._tcp",
			domain:           "local.",
			port:             config.Port,
			txtRecords:       make(map[string]string),
			discoveredPeers:  make(map[string]*DiscoveredPeer),
			// 优化：进一步增加间隔以减少CPU和网络使用
			announceInterval: 120 * time.Second, // 从30秒增加到120秒
			queryInterval:    120 * time.Second, // 从30秒增加到120秒
			ctx:              ctx,
			cancel:           cancel,
			discoveryMinInterval: 30 * time.Second, // 从10秒增加到30秒
			localDiscovery:  NewLocalDiscoveryService(logger),
		}
}

// Start 启动mDNS服务
func (ms *MDNSService) Start(ctx context.Context) error {
	if err := ms.NetworkService.Start(ms.ctx); err != nil {
		return err
	}
	
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	// 创建UDP连接
	addr, err := net.ResolveUDPAddr("udp", "224.0.0.251:5353")
	if err != nil {
		return fmt.Errorf("解析mDNS地址失败: %v", err)
	}

	// 使用默认接口监听所有可用的网络接口
	// nil 参数表示监听所有可用的网络接口，这样可以确保在多网卡环境下正常工作
	ms.conn, err = net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("创建mDNS连接失败: %v", err)
	}
	
	// 记录所有可用的网络接口信息
	if ms.logger != nil {
		interfaces, err := net.Interfaces()
		if err == nil {
			ms.logger.Infof("mDNS服务将监听所有可用的网络接口:")
			for _, iface := range interfaces {
				// 跳过回环接口和未启用的接口
				if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
					continue
				}
				
				// 检查是否支持多播
				if iface.Flags&net.FlagMulticast != 0 {
					// 获取接口的IP地址
					addrs, err := iface.Addrs()
					if err == nil {
						for _, addr := range addrs {
							if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
								ms.logger.Infof("  - %s: %s (支持多播)", iface.Name, ipnet.IP.String())
								break
							}
						}
					}
				}
			}
		} else {
			ms.logger.Warnf("无法获取网络接口信息: %v", err)
		}
	}
	
    // 配置多播选项（环回、TTL、地址重用），跨平台实现
    if err := EnableMulticastOptionsUDP(ms.conn); err != nil {
        if ms.logger != nil {
            ms.logger.Warnf("配置多播选项失败: %v", err)
        }
    } else {
        if ms.logger != nil {
            ms.logger.Debugf("已配置多播选项（环回、TTL、重用）")
        }
    }
	
	// 配置并启动本地发现服务作为备用
	if ms.localDiscovery != nil {
		ms.localDiscovery.Configure(ms.serviceName, ms.port, ms.txtRecords)
		ms.localDiscovery.SetDiscoveryCallback(ms.discoveryCallback)
		err = ms.localDiscovery.Start()
		if err != nil {
			if ms.logger != nil {
				ms.logger.Warnf("本地发现服务启动失败: %v", err)
			}
		} else {
			if ms.logger != nil {
				ms.logger.Infof("本地发现服务已启动作为mDNS备用")
			}
		}
	}
	
	// 启动服务公告
	go ms.announceLoop()
	
	// 启动服务查询
	go ms.queryLoop()
	
	// 启动消息接收
	go ms.receiveLoop()
	
	// mDNS服务已启动
	
	return nil
}

// Stop 停止mDNS服务
func (ms *MDNSService) Stop() error {
	if err := ms.NetworkService.Stop(); err != nil {
		return err
	}
	
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	ms.cancel()
	
	if ms.conn != nil {
		ms.conn.Close()
		ms.conn = nil
	}
	
	// 停止本地发现服务
	if ms.localDiscovery != nil {
		ms.localDiscovery.Stop()
	}
	
	ms.isAnnouncing = false
	ms.isQuerying = false
	
	// 清理发现的节点
	for key := range ms.discoveredPeers {
		delete(ms.discoveredPeers, key)
	}
	
	// 清理TXT记录
	for key := range ms.txtRecords {
		delete(ms.txtRecords, key)
	}
	
	// mDNS服务已停止
	
	return nil
}

// Announce 公告服务
func (ms *MDNSService) Announce() error {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	if ms.conn == nil {
		return fmt.Errorf("mDNS连接未建立")
	}
	
	// 构建DNS响应包
	response := ms.buildAnnounceResponse()
	
    // 调试日志改用 Debugf，避免控制台噪声
    if ms.logger != nil {
        ms.logger.Debugf("发送mDNS公告，TXT记录: %+v", ms.txtRecords)
    }
	
	// 发送到多播地址
	addr, _ := net.ResolveUDPAddr("udp", "224.0.0.251:5353")
	_, err := ms.conn.WriteToUDP(response, addr)
	if err != nil {
		return fmt.Errorf("发送mDNS公告失败: %v", err)
	}
	
	// 减少日志输出频率
	// if ms.logger != nil {
	//	ms.logger.Debugf("发送mDNS服务公告")
	// }
	
	return nil
}

// Query 查询服务
func (ms *MDNSService) Query() error {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	if ms.conn == nil {
		return fmt.Errorf("mDNS连接未建立")
	}
	
	// 构建DNS查询包
	query := ms.buildQueryPacket()
	
	// 发送到多播地址
	addr, _ := net.ResolveUDPAddr("udp", "224.0.0.251:5353")
	_, err := ms.conn.WriteToUDP(query, addr)
	if err != nil {
		return fmt.Errorf("发送mDNS查询失败: %v", err)
	}
	
    // 调试日志改用 Debugf，避免控制台噪声
    if ms.logger != nil {
        ms.logger.Debugf("发送mDNS查询")
    }
	
	return nil
}

// GetDiscoveredPeers 获取发现的对等节点
func (ms *MDNSService) GetDiscoveredPeers() []*PeerInfo {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	var peers []*PeerInfo
	now := time.Now()
	
	// 合并mDNS发现的节点
	for _, peer := range ms.discoveredPeers {
		// 过滤过期的对等节点（超过5分钟未见）
		if now.Sub(peer.LastSeen) > 5*time.Minute {
			continue
		}
		
		peers = append(peers, &PeerInfo{
			ID:       peer.ID,
			Address:  fmt.Sprintf("%s:%d", peer.Address, peer.Port),
			IsOnline: true,
			LastSeen: peer.LastSeen,
		})
	}
	
	// 合并本地发现的节点
	if ms.localDiscovery != nil {
		localPeers := ms.localDiscovery.GetDiscoveredPeers()
		for _, localPeer := range localPeers {
			// 检查是否已存在（避免重复）
			found := false
			for _, existingPeer := range peers {
				if existingPeer.ID == localPeer.ID {
					found = true
					break
				}
			}
			if !found {
				peers = append(peers, &PeerInfo{
					ID:       localPeer.ID,
					Address:  fmt.Sprintf("%s:%d", localPeer.Address, localPeer.Port),
					IsOnline: true,
					LastSeen: localPeer.LastSeen,
				})
			}
		}
	}
	
	return peers
}

// SetTxtRecord 设置TXT记录
func (ms *MDNSService) SetTxtRecord(key, value string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	ms.txtRecords[key] = value
	
	if ms.logger != nil {
		// 设置mDNS TXT记录
	}
}

// GetTxtRecord 获取TXT记录
func (ms *MDNSService) GetTxtRecord(key string) (string, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	value, exists := ms.txtRecords[key]
	return value, exists
}

// DiscoverPeers 发现对等节点
func (ms *MDNSService) DiscoverPeers(timeout time.Duration) ([]PeerInfo, error) {
	// 如果指定了超时时间，执行一次查询并等待结果
	if timeout > 0 {
		if err := ms.Query(); err != nil {
			return nil, fmt.Errorf("执行mDNS查询失败: %v", err)
		}
		
		// 触发本地发现
		if ms.localDiscovery != nil {
			go ms.localDiscovery.DiscoverPeers()
		}
		
		// 等待指定的超时时间以收集响应
		time.Sleep(timeout)
	}
	
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	var peers []PeerInfo
	for _, peer := range ms.discoveredPeers {
		peerInfo := PeerInfo{
			ID:       peer.ID,
			Username: peer.Name,
			Address:  peer.Address,
			Port:     peer.Port,
			IsOnline: true,
			LastSeen: peer.LastSeen,
			Metadata: peer.TxtData,
		}
		peers = append(peers, peerInfo)
	}
	
	return peers, nil
}

// SetDiscoveryCallback 设置发现回调
func (ms *MDNSService) SetDiscoveryCallback(callback func(peer PeerInfo)) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.discoveryCallback = callback
}

// StartMDNS 启动mDNS服务
func (ms *MDNSService) StartMDNS(username, nkn, pubkey string, port int) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	// 设置服务参数
	ms.serviceName = username
	ms.port = port
	
	// 设置TXT记录
	ms.txtRecords["username"] = username
	ms.txtRecords["pubkey"] = pubkey
	ms.txtRecords["port"] = fmt.Sprintf("%d", port)
	if nkn != "" {
		ms.txtRecords["nkn"] = nkn
	}
	
	// 重新配置本地发现服务以使用正确的参数
	if ms.localDiscovery != nil {
		ms.localDiscovery.Configure(ms.serviceName, ms.port, ms.txtRecords)
		if ms.logger != nil {
			ms.logger.Infof("[LOCAL-DEBUG] 重新配置本地发现服务: %s (端口: %d)", ms.serviceName, ms.port)
		}
	}
	
	// 如果服务还没有启动，启动它
	if ms.conn == nil {
		return ms.startMDNSConnection()
	}
	
	// 如果连接已存在但协程未启动，启动协程
	if !ms.isAnnouncing {
		go ms.announceLoop()
	}
	if !ms.isQuerying {
		go ms.queryLoop()
		go ms.receiveLoop()
	}
	
	// 立即发送一次公告
	go ms.Announce()
	
	if ms.logger != nil {
		// MDNS服务已配置
	}
	
	return nil
}

// StopMDNS 停止mDNS服务
func (ms *MDNSService) StopMDNS() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	// 停止服务
	return ms.Stop()
}

// startMDNSConnection 启动MDNS连接
func (ms *MDNSService) startMDNSConnection() error {
	// 创建UDP连接
	addr, err := net.ResolveUDPAddr("udp", "224.0.0.251:5353")
	if err != nil {
		return fmt.Errorf("解析mDNS地址失败: %v", err)
	}
	
	ms.conn, err = net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("创建mDNS连接失败: %v", err)
	}
	
	// 启动服务循环
	go ms.announceLoop()
	go ms.queryLoop()
	go ms.receiveLoop()
	
	if ms.logger != nil {
		// MDNS连接已建立
	}
	
	return nil
}

// announceLoop 公告循环
func (ms *MDNSService) announceLoop() {
	ms.mu.Lock()
	ms.isAnnouncing = true
	ms.mu.Unlock()
	
	ticker := time.NewTicker(ms.announceInterval)
	defer ticker.Stop()
	
	// 立即发送一次公告
	ms.Announce()
	
	for {
		select {
		case <-ms.ctx.Done():
			return
		case <-ticker.C:
			if err := ms.Announce(); err != nil {
				if ms.logger != nil {
					ms.logger.Errorf("mDNS公告失败: %v", err)
				}
			}
		}
	}
}

// queryLoop 查询循环
func (ms *MDNSService) queryLoop() {
	ms.mu.Lock()
	ms.isQuerying = true
	ms.mu.Unlock()
	
	ticker := time.NewTicker(ms.queryInterval)
	defer ticker.Stop()
	
	// 立即发送一次查询
	ms.Query()
	
	for {
		select {
		case <-ms.ctx.Done():
			return
		case <-ticker.C:
			if err := ms.Query(); err != nil {
				if ms.logger != nil {
					ms.logger.Errorf("mDNS查询失败: %v", err)
				}
			}
		}
	}
}

// receiveLoop 接收循环
func (ms *MDNSService) receiveLoop() {
	buffer := make([]byte, 1500) // 标准MTU大小
	
	for {
		select {
		case <-ms.ctx.Done():
			return
		default:
			ms.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, addr, err := ms.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // 超时是正常的，继续循环
				}
				if ms.logger != nil {
					ms.logger.Errorf("接收mDNS消息失败: %v", err)
				}
				continue
			}
			
			ms.handleMessage(buffer[:n], addr)
		}
	}
}

// handleMessage 处理接收到的消息
func (ms *MDNSService) handleMessage(data []byte, addr *net.UDPAddr) {
	// 简化的DNS包解析
	if len(data) < 12 {
		if ms.logger != nil {
			ms.logger.Debugf("收到过短的mDNS消息，长度: %d", len(data))
		}
		return // DNS头部至少12字节
	}
	
	// 检查是否是响应包
	flags := uint16(data[2])<<8 | uint16(data[3])
	isResponse := (flags & 0x8000) != 0
	
    if ms.logger != nil {
        ms.logger.Debugf("收到来自 %s 的mDNS消息，类型: %s，长度: %d", addr.String(), 
            map[bool]string{true: "响应", false: "查询"}[isResponse], len(data))
    }
	
	if isResponse {
		// 处理响应包 - 解析发现的服务
		peer := ms.parseServiceResponse(data, addr)
		if peer != nil {
			// 检查是否是自身节点
			isSelfNode := false
			
			// 检查公钥是否相同（防止相同公钥的节点互相连接）
			if peerPubKey, exists := peer.TxtData["pubkey"]; exists {
				if myPubKey, myExists := ms.txtRecords["pubkey"]; myExists {
					if peerPubKey == myPubKey {
						isSelfNode = true
					}
				}
			}
			
			// 对于同机器不同端口的节点，允许发现
			// 只有当服务名和端口都相同时才跳过
			if peer.Name == ms.serviceName && peer.Port == ms.port {
				isSelfNode = true
			}
			
			// 只有非自身节点且是新节点时才输出日志
			if !isSelfNode {
				ms.mu.RLock()
				_, exists := ms.discoveredPeers[peer.ID]
				ms.mu.RUnlock()
				
				if !exists && ms.logger != nil {
					ms.logger.Infof("发现新节点: %s (%s:%d)", peer.Name, peer.Address, peer.Port)
				}
			}
			
			ms.addDiscoveredPeer(peer)
		} else {
            if ms.logger != nil {
                ms.logger.Debugf("无法解析来自 %s 的服务响应", addr.String())
            }
		}
	} else {
		// 处理查询包 - 检查是否匹配我们的服务
        if ms.logger != nil {
            ms.logger.Debugf("处理来自 %s 的查询包", addr.String())
        }
		
		// 检查查询是否匹配我们的服务
		if ms.matchesOurService(data) {
            if ms.logger != nil {
                ms.logger.Debugf("查询匹配我们的服务，发送公告响应")
            }
			// 发送我们的服务公告作为响应
			go ms.Announce()
		} else {
            if ms.logger != nil {
                ms.logger.Debugf("收到不匹配的服务查询")
            }
		}
	}
}

// parseServiceResponse 解析服务响应
func (ms *MDNSService) parseServiceResponse(data []byte, addr *net.UDPAddr) *DiscoveredPeer {
	if ms.logger != nil {
		// 解析mDNS响应
	}
	
	// 检查是否包含我们的服务类型（支持编码和纯文本格式）
	serviceType := ms.serviceType + ms.domain
	encodedServiceType := ms.encodeDNSName(serviceType)
	
	// 检查编码格式
	containsEncoded := false
	for i := 0; i <= len(data)-len(encodedServiceType); i++ {
		if bytes.Equal(data[i:i+len(encodedServiceType)], encodedServiceType) {
			containsEncoded = true
			break
		}
	}
	
	// 检查纯文本格式（备用）
	containsPlainText := strings.Contains(string(data), serviceType)
	
	if !containsEncoded && !containsPlainText {
		if ms.logger != nil {
			// 数据不包含服务类型
		}
		return nil
	}
	
	if ms.logger != nil {
		// 找到服务类型
	}
	
	// 解析DNS格式的TXT记录
	txtData := make(map[string]string)
	
	// 查找TXT记录的开始位置
	// 在DNS响应中，TXT记录通常在特定位置，我们需要找到TXT记录数据部分
	txtStart := -1
	for i := 0; i < len(data)-2; i++ {
		// 查找TXT记录类型标识 (0x00, 0x10)
		if data[i] == 0x00 && data[i+1] == 0x10 {
			// 跳过类型(2字节)、类(2字节)、TTL(4字节)、数据长度(2字节)
			if i+10 < len(data) {
				txtStart = i + 10
				break
			}
		}
	}
	
	if txtStart == -1 {
		// 如果没找到标准DNS格式，尝试简单的字符串搜索作为备用
		dataStr := string(data)
		if ms.logger != nil {
			// 未找到DNS TXT记录格式，尝试简单解析
		}
		
		// 简单的字符串搜索（备用方案）
		if usernameStart := strings.Index(dataStr, "username="); usernameStart != -1 {
			usernameStart += 9
			usernameEnd := usernameStart
			for usernameEnd < len(dataStr) && dataStr[usernameEnd] != '\x00' && dataStr[usernameEnd] != '\n' && dataStr[usernameEnd] != '\r' {
				usernameEnd++
			}
			if usernameEnd > usernameStart {
				txtData["username"] = dataStr[usernameStart:usernameEnd]
			}
		}
		
		if pubkeyStart := strings.Index(dataStr, "pubkey="); pubkeyStart != -1 {
			pubkeyStart += 7
			pubkeyEnd := pubkeyStart
			for pubkeyEnd < len(dataStr) && dataStr[pubkeyEnd] != '\x00' && dataStr[pubkeyEnd] != '\n' && dataStr[pubkeyEnd] != '\r' {
				pubkeyEnd++
			}
			if pubkeyEnd > pubkeyStart {
				txtData["pubkey"] = dataStr[pubkeyStart:pubkeyEnd]
			}
		}
		
		if portStart := strings.Index(dataStr, "port="); portStart != -1 {
			portStart += 5
			portEnd := portStart
			for portEnd < len(dataStr) && dataStr[portEnd] != '\x00' && dataStr[portEnd] != '\n' && dataStr[portEnd] != '\r' {
				portEnd++
			}
			if portEnd > portStart {
				txtData["port"] = dataStr[portStart:portEnd]
			}
		}
	} else {
		// 解析DNS格式的TXT记录
		if ms.logger != nil {
			// 找到TXT记录开始位置
		}
		
		i := txtStart
		for i < len(data) {
			// 读取TXT记录长度
			if i >= len(data) {
				break
			}
			recordLen := int(data[i])
			i++
			
			if recordLen == 0 || i+recordLen > len(data) {
				break
			}
			
			// 读取TXT记录内容
			recordData := string(data[i : i+recordLen])
			i += recordLen
			
			if ms.logger != nil {
				// 解析TXT记录
			}
			
			// 解析键值对
			if eqIndex := strings.Index(recordData, "="); eqIndex != -1 {
				key := recordData[:eqIndex]
				value := recordData[eqIndex+1:]
				txtData[key] = value
				if ms.logger != nil {
					// 解析键值对
				}
			}
			
			// 如果遇到长度为0的记录或者已经到达数据末尾，停止解析
			if recordLen == 0 || i >= len(data) {
				break
			}
		}
	}
	
	if ms.logger != nil {
		// 解析到的TXT数据
	}
	
	// 如果没有找到用户名，跳过这个节点
	if txtData["username"] == "" {
		if ms.logger != nil {
			// 未找到用户名，跳过此节点
		}
		return nil
	}
	
	// 解析端口
	port := ms.port // 默认端口
	if portStr, exists := txtData["port"]; exists {
		if parsedPort, err := strconv.Atoi(portStr); err == nil {
			port = parsedPort
		}
	}
	
	peer := &DiscoveredPeer{
		ID:       txtData["pubkey"], // 使用公钥作为ID
		Name:     txtData["username"],
		Address:  addr.IP.String(),
		Port:     port,
		TxtData:  txtData,
		LastSeen: time.Now(),
	}
	
	return peer
}

// addDiscoveredPeer 添加发现的对等节点
func (ms *MDNSService) addDiscoveredPeer(peer *DiscoveredPeer) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	// 检查公钥是否相同（防止相同公钥的节点互相连接）
	if peerPubKey, exists := peer.TxtData["pubkey"]; exists {
		if myPubKey, myExists := ms.txtRecords["pubkey"]; myExists {
			if peerPubKey == myPubKey {
				if ms.logger != nil {
					ms.logger.Debugf("跳过自身节点: %s (相同公钥)", peer.Name)
				}
				return
			}
		}
	}
	
	// 对于同机器不同端口的节点，允许发现
	// 只有当服务名和端口都相同时才跳过
	if peer.Name == ms.serviceName && peer.Port == ms.port {
		if ms.logger != nil {
			ms.logger.Debugf("跳过相同服务: %s:%d", peer.Name, peer.Port)
		}
		return
	}
	
	existing, exists := ms.discoveredPeers[peer.ID]
	if exists {
		// 更新现有对等节点
		existing.LastSeen = peer.LastSeen
		existing.TxtData = peer.TxtData
	} else {
		// 添加新对等节点
		ms.discoveredPeers[peer.ID] = peer
		
		// 发现新的对等节点
		if ms.logger != nil {
			ms.logger.Infof("🔍 发现新节点: %s (地址: %s:%d, 公钥: %s)", peer.Name, peer.Address, peer.Port, peer.ID)
		}
		
		// 调用发现回调
		if ms.discoveryCallback != nil {
			peerInfo := PeerInfo{
				ID:        peer.ID,
				Username:  peer.Name,
				PublicKey: peer.ID, // 使用ID作为公钥，因为ID就是公钥
				Address:   peer.Address,
				Port:      peer.Port,
				IsOnline:  true,
				LastSeen:  peer.LastSeen,
				Metadata:  peer.TxtData,
			}
			go ms.discoveryCallback(peerInfo)
		}
	}
}

// buildAnnounceResponse 构建公告响应包
func (ms *MDNSService) buildAnnounceResponse() []byte {
	// 构建简化的DNS响应包
	serviceName := fmt.Sprintf("%s.%s%s", ms.serviceName, ms.serviceType, ms.domain)
	
	// DNS头部 (12字节)
	response := make([]byte, 0, 512)
	
	// Transaction ID (2字节)
	response = append(response, 0x00, 0x00)
	
	// Flags (2字节) - 标准响应
	response = append(response, 0x84, 0x00)
	
	// Questions (2字节)
	response = append(response, 0x00, 0x00)
	
	// Answer RRs (2字节)
	response = append(response, 0x00, 0x01)
	
	// Authority RRs (2字节)
	response = append(response, 0x00, 0x00)
	
	// Additional RRs (2字节)
	response = append(response, 0x00, 0x01)
	
	// 服务名称 - 使用DNS标签格式
	response = append(response, ms.encodeDNSName(serviceName)...)
	
	// 类型 TXT (16)
	response = append(response, 0x00, 0x10)
	
	// 类 IN (1)
	response = append(response, 0x00, 0x01)
	
	// TTL (4字节) - 120秒
	response = append(response, 0x00, 0x00, 0x00, 0x78)
	
	// TXT记录数据
	txtData := make([]byte, 0, 256)
	for key, value := range ms.txtRecords {
		txtRecord := fmt.Sprintf("%s=%s", key, value)
		txtData = append(txtData, byte(len(txtRecord)))
		txtData = append(txtData, []byte(txtRecord)...)
	}
	
	// 数据长度 (2字节)
	response = append(response, byte(len(txtData)>>8), byte(len(txtData)&0xFF))
	
	// TXT数据
	response = append(response, txtData...)
	
	// 附加记录 - A记录
	response = append(response, ms.encodeDNSName(serviceName)...)
	
	// 类型 A (1)
	response = append(response, 0x00, 0x01)
	
	// 类 IN (1)
	response = append(response, 0x00, 0x01)
	
	// TTL (4字节)
	response = append(response, 0x00, 0x00, 0x00, 0x78)
	
	// 数据长度 (4字节)
	response = append(response, 0x00, 0x04)
	
	// IP地址
	localIP := ms.getLocalIP()
	ipParts := strings.Split(localIP, ".")
	for _, part := range ipParts {
		if ip, err := strconv.Atoi(part); err == nil {
			response = append(response, byte(ip))
		}
	}
	
	return response
}

// buildQueryPacket 构建查询包
func (ms *MDNSService) buildQueryPacket() []byte {
	// 构建标准的DNS查询包
	serviceType := ms.serviceType + ms.domain
	
	// DNS头部 (12字节)
	query := make([]byte, 0, 256)
	
	// Transaction ID (2字节)
	query = append(query, 0x00, 0x00)
	
	// Flags (2字节) - 标准查询
	query = append(query, 0x01, 0x00)
	
	// Questions (2字节)
	query = append(query, 0x00, 0x01)
	
	// Answer RRs (2字节)
	query = append(query, 0x00, 0x00)
	
	// Authority RRs (2字节)
	query = append(query, 0x00, 0x00)
	
	// Additional RRs (2字节)
	query = append(query, 0x00, 0x00)
	
	// 查询名称 - 使用DNS标签格式
	query = append(query, ms.encodeDNSName(serviceType)...)
	
	// 查询类型 PTR (12)
	query = append(query, 0x00, 0x0C)
	
	// 查询类 IN (1)
	query = append(query, 0x00, 0x01)
	
	return query
}

// encodeDNSName 编码DNS名称为标签格式
func (ms *MDNSService) encodeDNSName(name string) []byte {
	parts := strings.Split(name, ".")
	result := make([]byte, 0, len(name)+len(parts)+1)
	
	for _, part := range parts {
		if len(part) > 0 {
			result = append(result, byte(len(part)))
			result = append(result, []byte(part)...)
		}
	}
	result = append(result, 0x00) // 名称结束符
	return result
}

// getLocalIP 获取本地IP地址
func (ms *MDNSService) getLocalIP() string {
	// 获取所有可用的本地IP地址
	var localIPs []string
	
	// 获取所有网络接口
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			// 跳过回环接口和未启用的接口
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			
			// 获取接口地址
			addrs, err := iface.Addrs()
			if err == nil {
				for _, addr := range addrs {
					if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
						localIPs = append(localIPs, ipnet.IP.String())
						if ms.logger != nil {
							ms.logger.Debugf("发现网络接口 %s: %s", iface.Name, ipnet.IP.String())
						}
					}
				}
			}
		}
	}

	// 如果找到非回环地址，返回第一个
	if len(localIPs) > 0 {
		// 降低日志级别，避免重复输出
		if ms.logger != nil {
			ms.logger.Debugf("发现本地IP地址: %v，使用: %s", localIPs, localIPs[0])
		}
		return localIPs[0]
	}

	// 如果没有找到非回环地址，尝试通过连接外部地址获取本地IP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		if ms.logger != nil {
			ms.logger.Debugf("无法获取外部IP，使用回环地址")
		}
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	if ms.logger != nil {
		ms.logger.Debugf("通过外部连接获取本地IP: %s", localAddr.IP.String())
	}
	return localAddr.IP.String()
}

// CleanupExpiredPeers 清理过期的对等节点
func (ms *MDNSService) CleanupExpiredPeers() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	now := time.Now()
	expiredThreshold := 15 * time.Second // 进一步缩短到15秒，更快清理断开的节点
	
	for id, peer := range ms.discoveredPeers {
		if now.Sub(peer.LastSeen) > expiredThreshold {
			delete(ms.discoveredPeers, id)
			
			if ms.logger != nil {
				ms.logger.Debugf("清理过期节点: %s (离线 %v)", peer.Name, now.Sub(peer.LastSeen))
			}
		}
	}
}

// ForceCleanupPeer 强制清理指定的对等节点
func (ms *MDNSService) ForceCleanupPeer(peerID string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	if peer, exists := ms.discoveredPeers[peerID]; exists {
		delete(ms.discoveredPeers, peerID)
		if ms.logger != nil {
			ms.logger.Debugf("强制清理节点: %s", peer.Name)
		}
	}
}

// ForceCleanupPeerByName 根据节点名称强制清理对等节点
func (ms *MDNSService) ForceCleanupPeerByName(peerName string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	for id, peer := range ms.discoveredPeers {
		if peer.Name == peerName {
			delete(ms.discoveredPeers, id)
			if ms.logger != nil {
				ms.logger.Debugf("根据名称强制清理节点: %s", peerName)
			}
			break
		}
	}
}

// GetStats 获取mDNS统计信息
func (ms *MDNSService) GetStats() map[string]interface{} {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	return map[string]interface{}{
		"service_name":      ms.serviceName,
		"service_type":      ms.serviceType,
		"port":              ms.port,
		"discovered_peers":  len(ms.discoveredPeers),
		"txt_records":       len(ms.txtRecords),
		"is_announcing":     ms.isAnnouncing,
		"is_querying":       ms.isQuerying,
		"announce_interval": ms.announceInterval,
		"query_interval":    ms.queryInterval,
	}
}

// matchesOurService 检查查询是否匹配我们的服务
func (ms *MDNSService) matchesOurService(data []byte) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	if ms.serviceName == "" {
		return false
	}
	
	// 构建完整的服务类型名称
	serviceType := ms.serviceType + ms.domain // "_tchat._tcp.local."
	
	// 将服务类型编码为DNS标签格式
	encodedServiceType := ms.encodeDNSName(serviceType)
	
	if ms.logger != nil {
		// 查找服务类型
	}
	
	// 在数据包中查找编码后的服务类型
	for i := 12; i <= len(data)-len(encodedServiceType); i++ {
		if bytes.Equal(data[i:i+len(encodedServiceType)], encodedServiceType) {
			if ms.logger != nil {
				// 在查询包中找到匹配的服务类型
			}
			return true
		}
	}
	
	// 也尝试查找纯文本形式（作为备用）
	serviceTypeBytes := []byte(ms.serviceType)
	for i := 12; i <= len(data)-len(serviceTypeBytes); i++ {
		if bytes.Equal(data[i:i+len(serviceTypeBytes)], serviceTypeBytes) {
			if ms.logger != nil {
				// 在查询包中找到匹配的服务类型（纯文本）
			}
			return true
		}
	}
	
	return false
}

// StartDiscovery 开始发现服务
func (ms *MDNSService) StartDiscovery() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	// 检查是否在最小间隔内，避免频繁的重复发现
	if time.Since(ms.lastDiscoveryTime) < ms.discoveryMinInterval {
		if ms.logger != nil {
			// mDNS发现请求过于频繁，跳过
		}
		return nil
	}
	
	ms.lastDiscoveryTime = time.Now()
	
	if ms.logger != nil {
		// 开始mDNS服务发现
	}
	
	// 发送查询
	if err := ms.Query(); err != nil {
		return fmt.Errorf("发送mDNS查询失败: %v", err)
	}
	
	return nil
}
// Configure 配置mDNS服务但不重复启动
func (ms *MDNSService) Configure(username, nkn, pubkey string, port int) {
    ms.mu.Lock()
    defer ms.mu.Unlock()

    // 更新服务基本信息
    ms.serviceName = username
    ms.port = port

    if ms.txtRecords == nil {
        ms.txtRecords = make(map[string]string)
    }

    // 更新TXT记录
    ms.txtRecords["username"] = username
    ms.txtRecords["pubkey"] = pubkey
    ms.txtRecords["port"] = fmt.Sprintf("%d", port)
    if nkn != "" {
        ms.txtRecords["nkn"] = nkn
    }

    // 如果服务已在运行，则立即公告更新；否则等待 Start 时生效
    if ms.conn != nil {
        go ms.Announce()
    }
}