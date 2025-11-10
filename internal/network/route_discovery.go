package network

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"t-chat/internal/pinecone/router/events"
)

// RouteFailureInfo 路由失败信息
type RouteFailureInfo struct {
	PeerID       string    `json:"peer_id"`
	FailureCount int       `json:"failure_count"`
	LastFailure  time.Time `json:"last_failure"`
	LastSuccess  time.Time `json:"last_success"`
	IsBlacklisted bool     `json:"is_blacklisted"`
	LastLoggedOffline time.Time `json:"last_logged_offline"` // 记录上次输出离线日志的时间
	LastLoggedFailure time.Time `json:"last_logged_failure"` // 记录上次输出失败日志的时间
	LastConnectionAttempt time.Time `json:"last_connection_attempt"` // 记录上次连接尝试的时间
	LastLoggedConnection time.Time `json:"last_logged_connection"` // 记录上次输出连接日志的时间
}

// eventWork 事件工作项
type eventWork struct {
	eventType string
	peerID    string
}

// RouteDiscoveryService 主动路由发现服务
type RouteDiscoveryService struct {
	mu                sync.RWMutex
	pineconeService   PineconeServiceInterface
	logger           Logger
	ctx              context.Context
	cancel           context.CancelFunc
	isRunning        bool
	routeFailures    map[string]*RouteFailureInfo
	discoveryTicker  *time.Ticker
	monitorTicker    *time.Ticker
	eventSubscriber  chan events.Event

	// 配置参数
	failureThreshold int           // 失败次数阈值
	discoveryInterval time.Duration // 发现间隔
	monitorInterval   time.Duration // 监控间隔
	retryDelay       time.Duration // 重试延迟
	
	// 快速收敛优化参数
	fastConvergenceMode bool          // 快速收敛模式
	lastTopologyChange  time.Time     // 上次拓扑变化时间
	convergenceTimeout  time.Duration // 收敛超时时间
	convergenceTimer   *time.Timer   // 收敛定时器
	
	// 增强功能
	networkQuality     map[string]float64 // 网络质量评分
	lastDiscoveryTime  time.Time          // 上次发现时间
	connectionAttempts map[string]int     // 连接尝试次数
	backoffMultiplier  float64            // 退避乘数
	maxBackoffDelay    time.Duration      // 最大退避延迟
	
	// 日志抑制字段
	lastLoggedOptimization time.Time // 上次输出路由优化日志的时间
	lastLoggedConvergence  time.Time // 上次输出收敛日志的时间
	
	// goroutine优化
	eventWorkQueue     chan eventWork // 事件工作队列
	workerPool         chan struct{}  // 工作池信号量
}



// NewRouteDiscoveryService 创建路由发现服务
func NewRouteDiscoveryService(pineconeService PineconeServiceInterface, logger Logger) RouteDiscoveryServiceInterface {
	ctx, cancel := context.WithCancel(context.Background())
	return &RouteDiscoveryService{
		pineconeService:   pineconeService,
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		routeFailures:    make(map[string]*RouteFailureInfo),
		eventSubscriber:  make(chan events.Event, 50), // 减少缓冲从100到50个事件
		failureThreshold: 3,
		// 优化：从10秒增加到30秒，减少频繁的路由发现
		discoveryInterval: 30 * time.Second,
		// 优化：从3秒增加到15秒，减少频繁的路由监控
		monitorInterval:   15 * time.Second,
		// 优化：从2秒增加到10秒，减少频繁的失败重试
		retryDelay:       10 * time.Second,
		// 快速收敛优化参数
		fastConvergenceMode: false,
		lastTopologyChange:  time.Now(),
		convergenceTimeout:  30 * time.Second, // 30秒收敛超时
		
		// 增强功能初始化
		networkQuality:     make(map[string]float64),
		connectionAttempts: make(map[string]int),
		backoffMultiplier:  1.5,                // 指数退避乘数
		maxBackoffDelay:    60 * time.Second,   // 最大退避延迟
		
		// goroutine优化
		eventWorkQueue:     make(chan eventWork, 20), // 进一步减少缓冲从50到20个事件
		workerPool:         make(chan struct{}, 1),  // 进一步限制最多1个工作goroutine
	}
}

// Start 启动路由发现服务
func (rds *RouteDiscoveryService) Start() error {
	rds.mu.Lock()
	defer rds.mu.Unlock()

	if rds.isRunning {
		return fmt.Errorf("路由发现服务已在运行")
	}

	// 订阅路由器事件
	if router := rds.pineconeService.GetRouter(); router != nil {
		router.Subscribe(rds.eventSubscriber)
	} else {
		rds.logger.Warnf("⚠️ 无法获取路由器实例，跳过事件订阅")
	}

	rds.isRunning = true
	rds.discoveryTicker = time.NewTicker(rds.discoveryInterval)
	rds.monitorTicker = time.NewTicker(rds.monitorInterval)

	// 启动路由监控协程
	go rds.routeMonitorLoop()
	// 启动主动发现协程
	go rds.activeDiscoveryLoop()
	// 启动事件监听协程
	go rds.eventListenerLoop()
	// 启动单个工作处理器协程
	go rds.eventWorkProcessor()

	return nil
}

// Stop 停止路由发现服务
func (rds *RouteDiscoveryService) Stop() error {
	rds.mu.Lock()
	defer rds.mu.Unlock()

	if !rds.isRunning {
		return nil
	}

	rds.isRunning = false
	rds.cancel()

	if rds.discoveryTicker != nil {
		rds.discoveryTicker.Stop()
	}
	if rds.monitorTicker != nil {
		rds.monitorTicker.Stop()
	}
	if rds.convergenceTimer != nil {
		rds.convergenceTimer.Stop()
		rds.convergenceTimer = nil
	}

	return nil
}

// routeMonitorLoop 路由监控循环（自适应间隔）
func (rds *RouteDiscoveryService) routeMonitorLoop() {
	for {
		select {
		case <-rds.ctx.Done():
			return
		case <-rds.monitorTicker.C:
			rds.monitorRouteHealth()
			// 动态调整监控间隔
			newInterval := rds.getAdaptiveMonitorInterval()
			rds.monitorTicker.Reset(newInterval)
		}
	}
}

// activeDiscoveryLoop 主动发现循环
func (rds *RouteDiscoveryService) activeDiscoveryLoop() {
	// 创建路由优化定时器（每120秒优化一次，进一步减少频率）
	optimizeTicker := time.NewTicker(120 * time.Second)
	defer optimizeTicker.Stop()
	
	for {
		select {
		case <-rds.ctx.Done():
			return
		case <-rds.discoveryTicker.C:
			rds.performActiveDiscovery()
			// 动态调整发现间隔
			newInterval := rds.getAdaptiveDiscoveryInterval()
			rds.discoveryTicker.Reset(newInterval)
		case <-optimizeTicker.C:
			// 直接执行优化，避免创建goroutine
			rds.optimizeRouteSelection()
		}
	}
}

// eventListenerLoop 事件监听循环
func (rds *RouteDiscoveryService) eventListenerLoop() {
	for {
		select {
		case <-rds.ctx.Done():
			return
		case event := <-rds.eventSubscriber:
			rds.handleRouterEvent(event)
		}
	}
}

// monitorRouteHealth 监控路由健康状态
func (rds *RouteDiscoveryService) monitorRouteHealth() {
	networkInfo := rds.pineconeService.GetNetworkInfo()
	peers, ok := networkInfo["peers"].([]map[string]interface{})
	if !ok {
		return
	}

	rds.mu.Lock()
	defer rds.mu.Unlock()

	// 检查当前连接的对等节点
	activePeers := make(map[string]bool)
	for _, peer := range peers {
		if peerID, ok := peer["id"].(string); ok {
			activePeers[peerID] = true
			// 重置成功连接的节点的失败计数，但保留日志抑制时间戳
			if failure, exists := rds.routeFailures[peerID]; exists {
				failure.LastSuccess = time.Now()
				if failure.FailureCount > 0 {
				failure.FailureCount = 0
				failure.IsBlacklisted = false
				// 注意：不重置LastLoggedOffline和LastLoggedFailure，保持日志抑制效果
			}
			}
		}
	}

	// 批量处理失败的路由，减少goroutine创建
	var failedPeers []string
	needDiscovery := false
	
	// 检查失败的路由和离线节点
	var peersToRemove []string
	logSuppressInterval := 5 * time.Minute // 5分钟内不重复输出相同节点的离线日志
	
	for peerID, failure := range rds.routeFailures {
		if !activePeers[peerID] {
			// 只对未被标记为失效的节点进行失败计数，避免重复检测
			if !failure.IsBlacklisted {
				failure.FailureCount++
				failure.LastFailure = time.Now()
				
				// 只有在距离上次输出离线日志超过抑制间隔时才输出日志
				if time.Now().Sub(failure.LastLoggedOffline) > logSuppressInterval {
					rds.logger.Warnf("❌ 检测到节点 %s 离线，失败次数: %d", peerID[:8], failure.FailureCount)
					failure.LastLoggedOffline = time.Now()
				}
				
				// 降低失败阈值，更快触发路由重新发现
				if failure.FailureCount >= 2 {
					failure.IsBlacklisted = true
					// 只有在距离上次输出失败日志超过抑制间隔时才输出日志
					if time.Now().Sub(failure.LastLoggedFailure) > logSuppressInterval {
						rds.logger.Errorf("🚫 节点 %s 连续失败 %d 次，标记为失效", peerID[:8], failure.FailureCount)
						failure.LastLoggedFailure = time.Now()
					}
					failedPeers = append(failedPeers, peerID)
					needDiscovery = true
				}
			}
			
			// 如果节点连续失败超过10次，从路由发现列表中移除
			if failure.FailureCount >= 10 {
				peersToRemove = append(peersToRemove, peerID)
			}
		}
	}
	
	// 移除连续失败过多的节点
	for _, peerID := range peersToRemove {
		delete(rds.routeFailures, peerID)
		delete(rds.networkQuality, peerID)
		delete(rds.connectionAttempts, peerID)
		// 节点连续失败超过阈值，已从路由发现中移除
	}

	// 批量处理路由发现，减少goroutine数量
	if needDiscovery {
		go func() {
			for _, peerID := range failedPeers {
				rds.triggerRouteDiscovery(peerID)
			}
			rds.connectToStaticPeers()
			rds.discoverMoreRoutes()
		}()
	}

	// 批量ping活跃节点，使用工作池模式减少goroutine创建 - 已禁用自动ping消息
	/*
	if len(activePeers) > 0 {
		go func() {
			// 使用工作池，最多5个worker
			workerCount := 5
			if len(activePeers) < workerCount {
				workerCount = len(activePeers)
			}
			
			peerChan := make(chan string, len(activePeers))
			for peerID := range activePeers {
				peerChan <- peerID
			}
			close(peerChan)
			
			// 启动worker池
			var wg sync.WaitGroup
			for i := 0; i < workerCount; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for peerID := range peerChan {
						rds.pingPeer(peerID)
					}
				}()
			}
			wg.Wait()
		}()
	}
	*/
}

// performActiveDiscovery 执行主动路由发现
func (rds *RouteDiscoveryService) performActiveDiscovery() {
	// 更新发现时间
	rds.mu.Lock()
	rds.lastDiscoveryTime = time.Now()
	rds.mu.Unlock()

	// 获取当前网络状态
	networkInfo := rds.pineconeService.GetNetworkInfo()
	connectedPeers := 0
	if peerCount, ok := networkInfo["connected_peers"].(int); ok {
		connectedPeers = peerCount
	} else {
		// 无法获取连接节点数量，默认为0
	}

	if connectedPeers == 0 {
		// 当前无连接节点，尝试连接到静态节点
		rds.connectToStaticPeers()
		return
	}

	// 基于网络质量决定目标连接数
	averageQuality := rds.getAverageNetworkQuality()
	targetConnections := 3
	if averageQuality < 0.5 {
		// 网络质量差时保持更多连接
		targetConnections = 5
	} else if averageQuality > 0.8 {
		// 网络质量好时可以减少连接数
		targetConnections = 2
	}

	// 检查是否需要发现更多路由
	if connectedPeers < targetConnections {
		// 连接节点数量不足，尝试发现更多节点
		rds.discoverMoreRoutes()
	}

	// 测试现有路由的连通性 - 已禁用自动ping消息
	// rds.testRouteConnectivity()
}

// triggerRouteDiscovery 触发路由发现
func (rds *RouteDiscoveryService) triggerRouteDiscovery(failedPeerID string) {
	// 立即执行一次主动发现
	rds.performActiveDiscovery()

	// 立即触发强制重连
	go rds.forceReconnectPeer(failedPeerID)

	// 尝试重新连接失效的节点（延迟重试）
	time.AfterFunc(rds.retryDelay, func() {
		rds.retryFailedPeer(failedPeerID)
	})
}

// connectToStaticPeers 触发动态节点发现
func (rds *RouteDiscoveryService) connectToStaticPeers() {
	// 防止频繁调用，添加时间间隔检查
	now := time.Now()
	if now.Sub(rds.lastDiscoveryTime) < 10*time.Second {
		// 跳过重复的节点发现请求
		return
	}
	rds.lastDiscoveryTime = now
	
	// 触发mDNS发现
	mdnsService := rds.getMDNSService()
	if mdnsService != nil {
		go func() {
			// 先清理过期的节点
			mdnsService.CleanupExpiredPeers()
			
			peers, err := mdnsService.DiscoverPeers(5 * time.Second)
			if err != nil {
				rds.logger.Errorf("mDNS发现失败: %v", err)
				return
			}
			
			// 主动连接发现的节点，添加连接抑制机制
			for _, peer := range peers {
				go func(p PeerInfo) {
					rds.connectToPeerWithSuppression(p, "主动连接")
				}(peer)
			}
		}()
	} else {
		rds.logger.Warnf("mDNS服务不可用")
	}
	
	// 触发蓝牙发现（如果可用）
	networkInfo := rds.pineconeService.GetNetworkInfo()
	if bluetoothEnabled, ok := networkInfo["bluetooth_enabled"].(bool); ok && bluetoothEnabled {
		// 蓝牙已启用，可能发现蓝牙节点
	}
}

// discoverMoreRoutes 发现更多路由
func (rds *RouteDiscoveryService) discoverMoreRoutes() {
	// 触发mDNS发现
	if mdnsService := rds.getMDNSService(); mdnsService != nil {
		go func() {
			// 先清理过期的节点
			mdnsService.CleanupExpiredPeers()
			
			// 触发mDNS节点发现
			peers, err := mdnsService.DiscoverPeers(5 * time.Second)
			if err != nil {
				rds.logger.Errorf("mDNS发现失败: %v", err)
				return
			}
			if len(peers) > 0 {
				// mDNS发现节点
			}
			
			// 智能连接策略：只连接关键节点，而不是所有发现的节点
			// 获取当前连接数
			networkInfo := rds.pineconeService.GetNetworkInfo()
			connectedCount := 0
			if connectedPeers, ok := networkInfo["peers"].([]map[string]interface{}); ok {
				connectedCount = len(connectedPeers)
			}
			
			// 设置最大连接数限制，避免连接过多节点
			maxConnections := 8 // 最多维持8个直接连接
			if connectedCount >= maxConnections {
				// 已达到最大连接数，跳过新节点连接
				return
			}
			
			// 只连接部分高质量节点
			maxNewConnections := maxConnections - connectedCount
			if maxNewConnections > 3 {
				maxNewConnections = 3 // 每次最多连接3个新节点
			}
			
			// 选择要连接的节点（优先选择质量较高的节点）
			selectedPeers := rds.selectBestPeersToConnect(peers, maxNewConnections)
			
			if len(selectedPeers) > 0 {
				// 智能连接策略选择节点
				
				// 使用工作池连接选中的节点
				workerCount := len(selectedPeers)
				if workerCount > 2 {
					workerCount = 2 // 限制并发连接数
				}
				
				peerChan := make(chan PeerInfo, len(selectedPeers))
				for _, peer := range selectedPeers {
					peerChan <- peer
				}
				close(peerChan)
				
				// 启动worker池连接节点
				var wg sync.WaitGroup
				for i := 0; i < workerCount; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for peer := range peerChan {
							rds.connectToPeerWithSuppression(peer, "智能路由发现")
						}
					}()
				}
				wg.Wait()
			} else {
				// 所有发现的节点都已连接或不适合连接
			}
		}()
	}

	// 可以在这里添加其他发现机制，如DHT查询、蓝牙扫描等
}

// connectToPeerWithSuppression 带抑制机制的节点连接
func (rds *RouteDiscoveryService) connectToPeerWithSuppression(peer PeerInfo, context string) {
	// 首先检查节点是否已经连接
	networkInfo := rds.pineconeService.GetNetworkInfo()
	if peers, ok := networkInfo["peers"].([]map[string]interface{}); ok {
		for _, connectedPeer := range peers {
			if peerID, ok := connectedPeer["id"].(string); ok && peerID == peer.ID {
				// 节点已连接，跳过连接
				return
			}
		}
	}
	
	rds.mu.Lock()
	defer rds.mu.Unlock()
	
	// 获取或创建路由失败信息
	failure, exists := rds.routeFailures[peer.ID]
	if !exists {
		failure = &RouteFailureInfo{
			PeerID: peer.ID,
		}
		rds.routeFailures[peer.ID] = failure
	}
	
	now := time.Now()
	connectionSuppressInterval := 30 * time.Second // 减少连接抑制间隔从2分钟到30秒
	logSuppressInterval := 5 * time.Minute         // 日志抑制间隔
	
	// 检查是否在连接抑制间隔内
	if now.Sub(failure.LastConnectionAttempt) < connectionSuppressInterval {
		return // 跳过连接，避免频繁重连
	}
	
	// 更新连接尝试时间
	failure.LastConnectionAttempt = now
	
	// 检查是否需要输出连接日志
	_ = now.Sub(failure.LastLoggedConnection) > logSuppressInterval
	
	addr := fmt.Sprintf("pinecone://%s:%d", peer.Address, peer.Port)
	
	// 异步连接节点
	go func() {
		if err := rds.pineconeService.ConnectToPeer(addr); err != nil {
			// 连接节点失败
		} else {
			// 成功连接到节点
		}
	}()
}

// testRouteConnectivity 测试路由连通性
func (rds *RouteDiscoveryService) testRouteConnectivity() {
	networkInfo := rds.pineconeService.GetNetworkInfo()
	peers, ok := networkInfo["peers"].([]map[string]interface{})
	if !ok {
		return
	}

	for _, peer := range peers {
		if peerID, ok := peer["id"].(string); ok {
			go rds.pingPeer(peerID)
		}
	}
}

// pingPeer 测试节点连通性
func (rds *RouteDiscoveryService) pingPeer(peerID string) {
	// 记录ping开始时间
	startTime := time.Now()
	
	// 发送ping消息测试连通性
	pingPacket := MessagePacket{
		ID:        fmt.Sprintf("ping-%d", time.Now().UnixNano()),
		Type:      "ping",
		Content:   "ping",
		Timestamp: time.Now(),
		From:      rds.pineconeService.GetPineconeAddr(),
		To:        peerID,
	}

	err := rds.pineconeService.SendMessagePacket(peerID, &pingPacket)
	latency := time.Since(startTime)
	
	if err != nil {
		rds.recordRouteFailure(peerID, err)
		rds.updateNetworkQuality(peerID, latency, false)
		rds.incrementConnectionAttempt(peerID)
	} else {
		rds.recordRouteSuccess(peerID)
		rds.updateNetworkQuality(peerID, latency, true)
		rds.resetConnectionAttempt(peerID)
	}
}

// forceReconnectPeer 强制重连对等节点
func (rds *RouteDiscoveryService) forceReconnectPeer(peerID string) {
	// 减少日志输出频率，避免重复信息
	// 强制重连节点
	
	// 1. 先清理mDNS缓存中的过期节点信息
	if mdnsService := rds.getMDNSService(); mdnsService != nil {
		// 强制清理该节点的缓存
		mdnsService.ForceCleanupPeer(peerID)
		// 清理过期节点
		mdnsService.CleanupExpiredPeers()
		
		go func() {
			// 等待一小段时间让节点重新广播
			time.Sleep(1 * time.Second)
			
			// 通过mDNS重新发现节点
			peers, err := mdnsService.DiscoverPeers(5 * time.Second) // 增加发现时间
			if err != nil {
				rds.logger.Errorf("mDNS重新发现失败: %v", err)
				return
			}
			
			// 查找目标节点并尝试连接
			for _, peer := range peers {
				if strings.Contains(peer.ID, peerID) || strings.Contains(peerID, peer.ID) {
					// 找到目标节点，尝试重新连接
					go rds.attemptDirectConnection(peer.Address, peer.Port, peerID)
					break
				}
			}
		}()
	}
	
	// 2. 尝试通过现有连接重新路由
	go rds.attemptRouteRecovery(peerID)
}

// attemptDirectConnection 尝试直接连接
func (rds *RouteDiscoveryService) attemptDirectConnection(address string, port int, peerID string) {
	addr := fmt.Sprintf("%s:%d", address, port)
	// 尝试直接连接
	
	// 构造Pinecone连接地址
	pineconeAddr := fmt.Sprintf("pinecone://%s", addr)
	
	// 通过Pinecone服务尝试连接
	if err := rds.pineconeService.ConnectToPeer(pineconeAddr); err != nil {
		rds.logger.Errorf("直接连接失败: %v", err)
	} else {
		// 成功重新连接到节点
		// 重置失败状态
		rds.mu.Lock()
		if failure, exists := rds.routeFailures[peerID]; exists {
			failure.FailureCount = 0
			failure.IsBlacklisted = false
			failure.LastSuccess = time.Now()
		}
		rds.mu.Unlock()
	}
}

// attemptRouteRecovery 尝试路由恢复
func (rds *RouteDiscoveryService) attemptRouteRecovery(peerID string) {
	// 尝试通过现有连接恢复路由
	
	// 获取当前网络信息
	networkInfo := rds.pineconeService.GetNetworkInfo()
	peers, ok := networkInfo["peers"].([]map[string]interface{})
	if !ok {
		return
	}
	
	// 通过现有连接尝试ping目标节点
	for _, peer := range peers {
		if activePeerID, ok := peer["id"].(string); ok && activePeerID != peerID {
			// 通过活跃节点尝试路由到目标节点 - 已禁用自动ping消息
			go func(intermediatePeer string) {
				// 通过中间节点尝试路由
				// rds.pingPeer(peerID) // 这会触发路由发现 - 已禁用
			}(activePeerID)
			break // 只通过一个中间节点尝试，避免过多并发
		}
	}
}

// retryFailedPeer 重试失效的节点
func (rds *RouteDiscoveryService) retryFailedPeer(peerID string) {
	rds.mu.Lock()
	failure, exists := rds.routeFailures[peerID]
	if !exists || !failure.IsBlacklisted {
		rds.mu.Unlock()
		return
	}
	rds.mu.Unlock()

	// 使用智能重试延迟 - 已禁用自动ping消息
	retryDelay := rds.getSmartRetryDelay(peerID)
	rds.logger.Debugf("🔄 将在 %v 后重试连接失效节点 %s", retryDelay, peerID[:8])
	
	// 延迟后重试 - 已禁用自动ping消息
	/*
	time.AfterFunc(retryDelay, func() {
		rds.pingPeer(peerID)
	})
	*/
}

// recordRouteFailure 记录路由失败
func (rds *RouteDiscoveryService) recordRouteFailure(peerID string, err error) {
	rds.mu.Lock()
	defer rds.mu.Unlock()

	if failure, exists := rds.routeFailures[peerID]; exists {
		failure.FailureCount++
		failure.LastFailure = time.Now()
	} else {
		rds.routeFailures[peerID] = &RouteFailureInfo{
			PeerID:       peerID,
			FailureCount: 1,
			LastFailure:  time.Now(),
		}
	}

	// 记录节点路由失败
}

// recordRouteSuccess 记录路由成功
func (rds *RouteDiscoveryService) recordRouteSuccess(peerID string) {
	rds.mu.Lock()
	defer rds.mu.Unlock()

	if failure, exists := rds.routeFailures[peerID]; exists {
		failure.LastSuccess = time.Now()
		failure.FailureCount = 0
		failure.IsBlacklisted = false
	}

	// 节点路由测试成功
}

// handleRouterEvent 处理路由器事件
func (rds *RouteDiscoveryService) handleRouterEvent(event events.Event) {
	// 记录拓扑变化时间并启用快速收敛模式
	rds.mu.Lock()
	rds.lastTopologyChange = time.Now()
	rds.fastConvergenceMode = true
	rds.mu.Unlock()
	
	switch e := event.(type) {
	case events.PeerRemoved:
		// 检测到节点断开，启动路由恢复
		rds.recordRouteFailure(e.PeerID, fmt.Errorf("peer disconnected"))
		
		// 强制清理mDNS缓存中的断开节点
		if mdnsService := rds.getMDNSService(); mdnsService != nil {
			mdnsService.ForceCleanupPeer(e.PeerID)
			mdnsService.CleanupExpiredPeers()
		}
		
		// 使用工作队列处理恢复操作，避免创建多个goroutine
		select {
		case rds.eventWorkQueue <- eventWork{
			eventType: "peer_removed",
			peerID:    e.PeerID,
		}:
		default:
			// 队列满时直接执行关键操作
			rds.triggerRouteDiscovery(e.PeerID)
		}

	case events.PeerAdded:
		// 检测到新节点连接，测试连接性
		rds.recordRouteSuccess(e.PeerID)
		// 使用工作队列处理新节点连接
		select {
		case rds.eventWorkQueue <- eventWork{
			eventType: "peer_added",
			peerID:    e.PeerID,
		}:
		default:
			// 队列满时直接执行关键操作
			rds.pingPeer(e.PeerID)
		}

	case events.TreeParentUpdate:
		// 树形拓扑父节点更新，重新评估路由
		select {
		case rds.eventWorkQueue <- eventWork{
			eventType: "tree_update",
		}:
		default:
			// 队列满时直接执行
			rds.performActiveDiscovery()
		}

	case events.SnakeDescUpdate:
		// Snake拓扑下行节点更新，触发路由优化
		// 直接执行，避免创建goroutine
		rds.performActiveDiscovery()

	default:
		// 处理其他可能影响路由的事件
		// 收到路由器事件
	}
	
	// 重置收敛定时器，避免重复定时器
	rds.resetConvergenceTimer()
}



// getAdaptiveDiscoveryInterval 获取自适应发现间隔
func (rds *RouteDiscoveryService) getAdaptiveDiscoveryInterval() time.Duration {
	rds.mu.RLock()
	defer rds.mu.RUnlock()
	
	if rds.fastConvergenceMode {
		// 快速收敛模式下使用更短的间隔
		return rds.discoveryInterval / 2 // 从30秒减少到15秒
	}
	return rds.discoveryInterval
}

// getAdaptiveMonitorInterval 获取自适应监控间隔
func (rds *RouteDiscoveryService) getAdaptiveMonitorInterval() time.Duration {
	rds.mu.RLock()
	defer rds.mu.RUnlock()
	
	if rds.fastConvergenceMode {
		// 快速收敛模式下使用更短的间隔
		return rds.monitorInterval / 2 // 从15秒减少到7.5秒
	}
	return rds.monitorInterval
}

// resetConvergenceTimer 重置收敛定时器，避免重复定时器
func (rds *RouteDiscoveryService) resetConvergenceTimer() {
	// 停止现有的定时器
	if rds.convergenceTimer != nil {
		rds.convergenceTimer.Stop()
	}
	
	// 创建新的定时器
	rds.convergenceTimer = time.AfterFunc(rds.convergenceTimeout, func() {
		rds.mu.Lock()
		defer rds.mu.Unlock()
		
		// 检查是否超过收敛超时时间
		if time.Since(rds.lastTopologyChange) >= rds.convergenceTimeout {
			rds.fastConvergenceMode = false
			// 添加日志抑制机制，避免重复输出相同的收敛日志
			now := time.Now()
			logSuppressInterval := 2 * time.Minute // 2分钟内不重复输出相同的收敛日志
			if now.Sub(rds.lastLoggedConvergence) > logSuppressInterval {
				// 快速收敛模式已关闭，网络拓扑已稳定
				rds.lastLoggedConvergence = now
			}
			// 清理定时器引用
			rds.convergenceTimer = nil
		}
	})
}

// getMDNSService 获取mDNS服务实例
func (rds *RouteDiscoveryService) getMDNSService() MDNSServiceInterface {
	// 从PineconeService获取mDNS服务实例
	if pineconeService, ok := rds.pineconeService.(*PineconeService); ok {
		return pineconeService.GetMDNSService()
	}
	return nil
}

// GetRouteFailures 获取路由失败信息（用于调试）
func (rds *RouteDiscoveryService) GetRouteFailures() map[string]*RouteFailureInfo {
	rds.mu.RLock()
	defer rds.mu.RUnlock()

	result := make(map[string]*RouteFailureInfo)
	for k, v := range rds.routeFailures {
		result[k] = &RouteFailureInfo{
			PeerID:       v.PeerID,
			FailureCount: v.FailureCount,
			LastFailure:  v.LastFailure,
			LastSuccess:  v.LastSuccess,
			IsBlacklisted: v.IsBlacklisted,
		}
	}
	return result
}

// DiscoverRoute 发现到指定目标的路由
func (rds *RouteDiscoveryService) DiscoverRoute(targetAddr string) error {
	// 开始发现路由
	rds.logger.Debugf("🔍 开始发现到目标 %s 的路由", targetAddr)
	
	// 触发主动发现
	rds.performActiveDiscovery()
	
	// 测试到目标的连通性
	rds.pingPeer(targetAddr)
	
	return nil
}

// GetStats 获取路由发现统计信息
func (rds *RouteDiscoveryService) GetStats() map[string]interface{} {
	rds.mu.RLock()
	defer rds.mu.RUnlock()

	failedCount := 0
	blacklistedCount := 0
	for _, failure := range rds.routeFailures {
		if failure.FailureCount > 0 {
			failedCount++
		}
		if failure.IsBlacklisted {
			blacklistedCount++
		}
	}

	return map[string]interface{}{
		"is_running":         rds.isRunning,
		"total_tracked":      len(rds.routeFailures),
		"failed_routes":      failedCount,
		"blacklisted_routes": blacklistedCount,
		"failure_threshold":  rds.failureThreshold,
		"discovery_interval": rds.discoveryInterval.String(),
		"monitor_interval":   rds.monitorInterval.String(),
		"fast_convergence":   rds.fastConvergenceMode,
		"network_quality":    rds.getAverageNetworkQuality(),
		"last_discovery":     rds.lastDiscoveryTime.Format(time.RFC3339),
	}
}

// getAverageNetworkQuality 获取平均网络质量
func (rds *RouteDiscoveryService) getAverageNetworkQuality() float64 {
	if len(rds.networkQuality) == 0 {
		return 0.0
	}
	
	total := 0.0
	for _, quality := range rds.networkQuality {
		total += quality
	}
	return total / float64(len(rds.networkQuality))
}

// updateNetworkQuality 更新网络质量评分
func (rds *RouteDiscoveryService) updateNetworkQuality(peerID string, latency time.Duration, success bool) {
	rds.mu.Lock()
	defer rds.mu.Unlock()
	
	// 基于延迟和成功率计算质量评分 (0-1)
	quality := 0.0
	if success {
		// 延迟越低质量越高
		if latency < 50*time.Millisecond {
			quality = 1.0
		} else if latency < 100*time.Millisecond {
			quality = 0.8
		} else if latency < 200*time.Millisecond {
			quality = 0.6
		} else if latency < 500*time.Millisecond {
			quality = 0.4
		} else {
			quality = 0.2
		}
	} else {
		quality = 0.0
	}
	
	// 使用指数移动平均更新质量评分
	if currentQuality, exists := rds.networkQuality[peerID]; exists {
		rds.networkQuality[peerID] = 0.7*currentQuality + 0.3*quality
	} else {
		rds.networkQuality[peerID] = quality
	}
	
	// 节点网络质量更新
}

// getSmartRetryDelay 获取智能重试延迟
func (rds *RouteDiscoveryService) getSmartRetryDelay(peerID string) time.Duration {
	rds.mu.RLock()
	defer rds.mu.RUnlock()
	
	attempts := rds.connectionAttempts[peerID]
	quality := rds.networkQuality[peerID]
	
	// 基于网络质量和尝试次数计算延迟
	baseDelay := rds.retryDelay
	
	// 网络质量越差，延迟越长
	qualityMultiplier := 1.0
	if quality < 0.3 {
		qualityMultiplier = 3.0
	} else if quality < 0.6 {
		qualityMultiplier = 2.0
	}
	
	// 指数退避
	backoffDelay := time.Duration(float64(baseDelay) * qualityMultiplier * math.Pow(rds.backoffMultiplier, float64(attempts)))
	
	// 限制最大延迟
	if backoffDelay > rds.maxBackoffDelay {
		backoffDelay = rds.maxBackoffDelay
	}
	
	return backoffDelay
}

// incrementConnectionAttempt 增加连接尝试次数
func (rds *RouteDiscoveryService) incrementConnectionAttempt(peerID string) {
	rds.mu.Lock()
	defer rds.mu.Unlock()
	
	rds.connectionAttempts[peerID]++
}

// resetConnectionAttempt 重置连接尝试次数
func (rds *RouteDiscoveryService) resetConnectionAttempt(peerID string) {
	rds.mu.Lock()
	defer rds.mu.Unlock()
	
	delete(rds.connectionAttempts, peerID)
}

// getBestQualityPeers 获取网络质量最好的节点列表
func (rds *RouteDiscoveryService) getBestQualityPeers(limit int) []string {
	rds.mu.RLock()
	defer rds.mu.RUnlock()
	
	type peerQuality struct {
		peerID  string
		quality float64
	}
	
	var peers []peerQuality
	for peerID, quality := range rds.networkQuality {
		peers = append(peers, peerQuality{peerID: peerID, quality: quality})
	}
	
	// 按质量排序
	for i := 0; i < len(peers)-1; i++ {
		for j := i + 1; j < len(peers); j++ {
			if peers[i].quality < peers[j].quality {
				peers[i], peers[j] = peers[j], peers[i]
			}
		}
	}
	
	// 返回前limit个节点
	var result []string
	for i := 0; i < len(peers) && i < limit; i++ {
		result = append(result, peers[i].peerID)
	}
	
	return result
}

// cleanupStaleData 清理过期数据
func (rds *RouteDiscoveryService) cleanupStaleData() {
	rds.mu.Lock()
	defer rds.mu.Unlock()
	
	now := time.Now()
	
	// 清理长时间未更新的网络质量数据
	for peerID, failure := range rds.routeFailures {
		if failure.LastSuccess.IsZero() && now.Sub(failure.LastFailure) > 5*time.Minute {
			delete(rds.networkQuality, peerID)
			delete(rds.connectionAttempts, peerID)
			// 清理过期节点数据
		}
	}
}

// reconnectAllKnownPeers 重新连接所有已知节点
func (rds *RouteDiscoveryService) reconnectAllKnownPeers() {
	// 重新连接所有已知节点
	
	// 触发mDNS发现所有已知节点
	if mdnsService := rds.getMDNSService(); mdnsService != nil {
		go func() {
			peers, err := mdnsService.DiscoverPeers(10 * time.Second)
			if err != nil {
				rds.logger.Errorf("重新发现节点失败: %v", err)
				return
			}
			
			// 尝试连接所有发现的节点，使用抑制机制
			for _, peer := range peers {
				go func(p PeerInfo) {
					rds.connectToPeerWithSuppression(p, "重新连接")
				}(peer)
			}
		}()
	}
	
	// 重新测试所有已知节点的连接性 - 已禁用自动ping消息
	/*
	rds.mu.RLock()
	for peerID := range rds.routeFailures {
		go rds.pingPeer(peerID)
	}
	rds.mu.RUnlock()
	*/
}

// optimizeRouteSelection 优化路由选择
// selectBestPeersToConnect 选择最佳的节点进行连接
func (rds *RouteDiscoveryService) selectBestPeersToConnect(peers []PeerInfo, maxCount int) []PeerInfo {
	if len(peers) == 0 || maxCount <= 0 {
		return nil
	}
	
	// 获取当前已连接的节点
	networkInfo := rds.pineconeService.GetNetworkInfo()
	connectedPeers := make(map[string]bool)
	if connectedPeersList, ok := networkInfo["peers"].([]map[string]interface{}); ok {
		for _, peer := range connectedPeersList {
			if peerID, ok := peer["id"].(string); ok {
				connectedPeers[peerID] = true
			}
		}
	}
	
	// 过滤掉已连接的节点
	var availablePeers []PeerInfo
	for _, peer := range peers {
		if !connectedPeers[peer.ID] && !connectedPeers[peer.PublicKey] {
			availablePeers = append(availablePeers, peer)
		}
	}
	
	if len(availablePeers) == 0 {
		return nil
	}
	
	// 如果可用节点数量不超过最大数量，直接返回所有可用节点
	if len(availablePeers) <= maxCount {
		return availablePeers
	}
	
	// 根据节点质量进行排序和选择
	rds.mu.RLock()
	type peerWithScore struct {
		peer  PeerInfo
		score float64
	}
	
	var scoredPeers []peerWithScore
	for _, peer := range availablePeers {
		score := 0.5 // 默认分数
		
		// 如果有历史质量数据，使用历史数据
		if quality, exists := rds.networkQuality[peer.ID]; exists {
			score = quality
		} else if quality, exists := rds.networkQuality[peer.PublicKey]; exists {
			score = quality
		}
		
		// 根据最后发现时间调整分数（新发现的节点优先级稍高）
		if !peer.LastSeen.IsZero() {
			timeSinceLastSeen := time.Since(peer.LastSeen)
			if timeSinceLastSeen < 5*time.Minute {
				score += 0.1 // 最近发现的节点加分
			}
		}
		
		scoredPeers = append(scoredPeers, peerWithScore{peer: peer, score: score})
	}
	rds.mu.RUnlock()
	
	// 按分数排序（降序）
	for i := 0; i < len(scoredPeers)-1; i++ {
		for j := i + 1; j < len(scoredPeers); j++ {
			if scoredPeers[i].score < scoredPeers[j].score {
				scoredPeers[i], scoredPeers[j] = scoredPeers[j], scoredPeers[i]
			}
		}
	}
	
	// 选择前maxCount个节点
	var selectedPeers []PeerInfo
	for i := 0; i < maxCount && i < len(scoredPeers); i++ {
		selectedPeers = append(selectedPeers, scoredPeers[i].peer)
	}
	
	return selectedPeers
}

// eventWorkProcessor 事件工作处理器
func (rds *RouteDiscoveryService) eventWorkProcessor() {
	for {
		select {
		case <-rds.ctx.Done():
			return
		case work := <-rds.eventWorkQueue:
			// 直接处理事件，避免创建额外的goroutine
			rds.processEventWork(work)
		}
	}
}

// processEventWork 处理事件工作
func (rds *RouteDiscoveryService) processEventWork(work eventWork) {
	switch work.eventType {
	case "peer_removed":
		rds.triggerRouteDiscovery(work.peerID)
	case "peer_added":
		// rds.pingPeer(work.peerID) // 已禁用自动ping消息
	case "tree_update":
		// rds.performActiveDiscovery() // 已禁用自动ping消息
	default:
		// 处理其他事件类型
	}
}

func (rds *RouteDiscoveryService) optimizeRouteSelection() {
	// 优化路由选择
	
	// 清理过期数据
	rds.cleanupStaleData()
	
	// 获取当前连接的节点列表
	networkInfo := rds.pineconeService.GetNetworkInfo()
	connectedPeers := make(map[string]bool)
	connectedCount := 0
	if peers, ok := networkInfo["peers"].([]map[string]interface{}); ok {
		connectedCount = len(peers)
		for _, peer := range peers {
			if peerID, ok := peer["id"].(string); ok {
				connectedPeers[peerID] = true
			}
		}
	}
	
	// 检查是否需要更多连接（保持适度的连接数）
	minConnections := 3 // 最少保持3个连接
	maxConnections := 8 // 最多8个连接
	
	if connectedCount < minConnections {
		// 连接数不足，需要发现更多节点
		// 当前连接数低于最小值，触发节点发现
		rds.discoverMoreRoutes()
		return
	}
	
	if connectedCount >= maxConnections {
		// 连接数已足够，不需要更多连接
		// 当前连接数已达到最大值，跳过路由优化
		return
	}
	
	// 获取最佳质量的节点，但只测试未连接的节点
	bestPeers := rds.getBestQualityPeers(3) // 减少测试数量
	unconnectedPeers := make([]string, 0)
	for _, peerID := range bestPeers {
		if !connectedPeers[peerID] {
			unconnectedPeers = append(unconnectedPeers, peerID)
		}
	}
	
	if len(unconnectedPeers) > 0 {
		// 添加日志抑制机制，避免重复输出相同的优化日志
		now := time.Now()
		logSuppressInterval := 2 * time.Minute // 2分钟内不重复输出相同的优化日志
		if now.Sub(rds.lastLoggedOptimization) > logSuppressInterval {
			// 发现未连接的高质量节点，优先测试连接性
			rds.lastLoggedOptimization = now
		}
		// 限制同时测试的节点数量 - 已禁用自动ping消息
		/*
		maxTest := 2
		if len(unconnectedPeers) > maxTest {
			unconnectedPeers = unconnectedPeers[:maxTest]
		}
		for _, peerID := range unconnectedPeers {
			go rds.pingPeer(peerID)
		}
		*/
	} else {
		// 当前连接质量良好，无需额外连接
	}
	
	// 检查是否有质量过低的节点需要替换（只在连接数较少时才替换）
	if connectedCount <= maxConnections-2 {
		rds.mu.RLock()
		lowQualityPeers := make([]string, 0)
		for peerID, quality := range rds.networkQuality {
			if quality < 0.3 {
				lowQualityPeers = append(lowQualityPeers, peerID)
			}
		}
		rds.mu.RUnlock()
		
		if len(lowQualityPeers) > 0 {
			rds.logger.Warnf("发现 %d 个低质量节点，触发新节点发现", len(lowQualityPeers))
			rds.discoverMoreRoutes()
		}
	}
}