package network

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"t-chat/internal/pinecone/router/events"
)

// RouteInfo 路由信息
type RouteInfo struct {
	PeerID       string    `json:"peer_id"`
	Latency      time.Duration `json:"latency"`
	Reliability  float64   `json:"reliability"`  // 可靠性评分 (0-1)
	Bandwidth    int64     `json:"bandwidth"`    // 带宽估计 (bytes/s)
	HopCount     int       `json:"hop_count"`    // 跳数
	LastUsed     time.Time `json:"last_used"`
	LastTested   time.Time `json:"last_tested"`
	FailureCount int       `json:"failure_count"`
	SuccessCount int       `json:"success_count"`
	IsActive     bool      `json:"is_active"`
}

// RouteScore 计算路由评分
func (r *RouteInfo) RouteScore() float64 {
	// 基础评分基于可靠性
	score := r.Reliability * 100
	
	// 延迟惩罚 (延迟越高，分数越低)
	latencyPenalty := float64(r.Latency.Milliseconds()) / 10.0
	score -= latencyPenalty
	
	// 跳数惩罚
	hopPenalty := float64(r.HopCount) * 5.0
	score -= hopPenalty
	
	// 带宽奖励
	bandwidthBonus := float64(r.Bandwidth) / 1024.0 / 1024.0 // MB/s
	score += bandwidthBonus
	
	// 最近使用奖励
	if time.Since(r.LastUsed) < time.Minute {
		score += 10.0
	}
	
	return score
}

// MultiPathRoute 多路径路由
type MultiPathRoute struct {
	Destination string       `json:"destination"`
	PrimaryPath *RouteInfo   `json:"primary_path"`
	BackupPaths []*RouteInfo `json:"backup_paths"`
	LastUpdate  time.Time    `json:"last_update"`
	LoadBalance bool         `json:"load_balance"` // 是否启用负载均衡
	RoundRobin  int          `json:"round_robin"`  // 轮询计数器
}

// GetBestRoute 获取最佳路由
func (mpr *MultiPathRoute) GetBestRoute() *RouteInfo {
	if mpr.PrimaryPath != nil && mpr.PrimaryPath.IsActive {
		return mpr.PrimaryPath
	}
	
	// 如果主路径不可用，从备用路径中选择最佳的
	for _, backup := range mpr.BackupPaths {
		if backup.IsActive {
			return backup
		}
	}
	
	return nil
}

// GetNextRoute 获取下一个路由（负载均衡）
func (mpr *MultiPathRoute) GetNextRoute() *RouteInfo {
	if !mpr.LoadBalance {
		return mpr.GetBestRoute()
	}
	
	// 收集所有活跃路由
	activeRoutes := []*RouteInfo{}
	if mpr.PrimaryPath != nil && mpr.PrimaryPath.IsActive {
		activeRoutes = append(activeRoutes, mpr.PrimaryPath)
	}
	for _, backup := range mpr.BackupPaths {
		if backup.IsActive {
			activeRoutes = append(activeRoutes, backup)
		}
	}
	
	if len(activeRoutes) == 0 {
		return nil
	}
	
	// 轮询选择
	route := activeRoutes[mpr.RoundRobin%len(activeRoutes)]
	mpr.RoundRobin++
	return route
}

// RouteRedundancyService 路由冗余服务
type RouteRedundancyService struct {
	mu              sync.RWMutex
	pineconeService PineconeServiceInterface
	logger          Logger
	ctx             context.Context
	cancel          context.CancelFunc
	isRunning       bool
	
	// 路由表
	multiPathRoutes map[string]*MultiPathRoute // destination -> MultiPathRoute
	routeMetrics    map[string]*RouteInfo      // peerID -> RouteInfo
	
	// 配置参数
	maxBackupPaths   int           // 最大备用路径数
	routeTestInterval time.Duration // 路由测试间隔
	metricUpdateInterval time.Duration // 指标更新间隔
	pathSelectionStrategy string    // 路径选择策略: "best", "round_robin", "weighted"
	
	// 定时器
	testTicker   *time.Ticker
	metricTicker *time.Ticker
	
	// 事件订阅
	eventSubscriber chan events.Event
}

// NewRouteRedundancyService 创建路由冗余服务
func NewRouteRedundancyService(pineconeService PineconeServiceInterface, logger Logger) *RouteRedundancyService {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &RouteRedundancyService{
		pineconeService: pineconeService,
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
		multiPathRoutes: make(map[string]*MultiPathRoute),
		routeMetrics:   make(map[string]*RouteInfo),
		
		// 优化：增加定时器间隔以减少CPU使用
		maxBackupPaths:   3,
		routeTestInterval: 120 * time.Second, // 从30秒增加到120秒
		metricUpdateInterval: 60 * time.Second, // 从10秒增加到60秒
		pathSelectionStrategy: "best",
	}
}

// Start 启动路由冗余服务
func (rrs *RouteRedundancyService) Start() error {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	if rrs.isRunning {
		return fmt.Errorf("路由冗余服务已在运行")
	}
	
	// 初始化定时器
	rrs.testTicker = time.NewTicker(rrs.routeTestInterval)
	rrs.metricTicker = time.NewTicker(rrs.metricUpdateInterval)
	
	// 订阅路由器事件
	rrs.eventSubscriber = make(chan events.Event, 100)
	if router := rrs.pineconeService.GetRouter(); router != nil {
		router.Subscribe(rrs.eventSubscriber)
	} else {
		rrs.logger.Warnf("⚠️ 无法获取路由器实例，跳过事件订阅")
	}
	
	// 启动各种循环
	go rrs.routeTestLoop()
	go rrs.metricUpdateLoop()
	go rrs.eventListenerLoop()
	
	// 初始化路由表
	rrs.initializeRoutes()
	
	rrs.isRunning = true
	return nil
}

// Stop 停止路由冗余服务
func (rrs *RouteRedundancyService) Stop() error {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	if !rrs.isRunning {
		return nil
	}
	
	// 停止定时器
	if rrs.testTicker != nil {
		rrs.testTicker.Stop()
		rrs.testTicker = nil
	}
	if rrs.metricTicker != nil {
		rrs.metricTicker.Stop()
		rrs.metricTicker = nil
	}
	
	// 关闭事件订阅
	if rrs.eventSubscriber != nil {
		close(rrs.eventSubscriber)
		rrs.eventSubscriber = nil
	}
	
	// 清理路由表，防止内存泄露
	rrs.multiPathRoutes = make(map[string]*MultiPathRoute)
	rrs.routeMetrics = make(map[string]*RouteInfo)
	
	// 取消上下文
	rrs.cancel()
	
	rrs.isRunning = false
	return nil
}

// routeTestLoop 路由测试循环
func (rrs *RouteRedundancyService) routeTestLoop() {
	for {
		select {
		case <-rrs.ctx.Done():
			return
		case <-rrs.testTicker.C:
			// 已禁用自动路由测试以停止ping消息
			// rrs.testAllRoutes()
		}
	}
}

// metricUpdateLoop 指标更新循环
func (rrs *RouteRedundancyService) metricUpdateLoop() {
	for {
		select {
		case <-rrs.ctx.Done():
			return
		case <-rrs.metricTicker.C:
			rrs.updateRouteMetrics()
		}
	}
}

// eventListenerLoop 事件监听循环
func (rrs *RouteRedundancyService) eventListenerLoop() {
	for {
		select {
		case <-rrs.ctx.Done():
			return
		case event := <-rrs.eventSubscriber:
			rrs.handleRouterEvent(event)
		}
	}
}

// initializeRoutes 初始化路由表
func (rrs *RouteRedundancyService) initializeRoutes() {
	networkInfo := rrs.pineconeService.GetNetworkInfo()
	peers, ok := networkInfo["peers"].([]map[string]interface{})
	if !ok {
		return
	}
	
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	for _, peer := range peers {
		if peerID, ok := peer["id"].(string); ok {
			// 创建路由信息
			routeInfo := &RouteInfo{
				PeerID:       peerID,
				Latency:      100 * time.Millisecond, // 默认延迟
				Reliability:  0.9,                    // 默认可靠性
				Bandwidth:    1024 * 1024,            // 默认带宽 1MB/s
				HopCount:     1,                      // 直连默认1跳
				LastTested:   time.Now(),
				IsActive:     true,
			}
			
			rrs.routeMetrics[peerID] = routeInfo
			
			// 为每个peer创建多路径路由
			multiPath := &MultiPathRoute{
				Destination: peerID,
				PrimaryPath: routeInfo,
				BackupPaths: []*RouteInfo{},
				LastUpdate:  time.Now(),
				LoadBalance: false, // 默认不启用负载均衡
			}
			
			rrs.multiPathRoutes[peerID] = multiPath
		}
	}
	
	// 初始化路由
}

// testAllRoutes 测试所有路由
func (rrs *RouteRedundancyService) testAllRoutes() {
	rrs.mu.RLock()
	routesCopy := make(map[string]*RouteInfo)
	for k, v := range rrs.routeMetrics {
		routesCopy[k] = v
	}
	rrs.mu.RUnlock()
	
	for peerID, route := range routesCopy {
		go rrs.testRoute(peerID, route)
	}
}

// testRoute 测试单个路由
func (rrs *RouteRedundancyService) testRoute(peerID string, route *RouteInfo) {
	start := time.Now()
	
	// 发送ping测试连通性和延迟
	err := rrs.pingPeer(peerID)
	latency := time.Since(start)
	
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	if err != nil {
		// 测试失败
		route.FailureCount++
		route.IsActive = false
		
		// 如果失败次数过多，降低可靠性
		if route.FailureCount > 3 {
			route.Reliability *= 0.9
		}
	} else {
		// 测试成功
		route.SuccessCount++
		route.IsActive = true
		route.Latency = latency
		route.LastTested = time.Now()
		
		// 重置失败计数并提高可靠性
		if route.FailureCount > 0 {
			route.FailureCount = 0
		}
		if route.Reliability < 0.99 {
			route.Reliability = math.Min(0.99, route.Reliability*1.01)
		}
	}
	
	// 更新多路径路由
	rrs.updateMultiPathRoute(peerID)
}

// updateMultiPathRoute 更新多路径路由
func (rrs *RouteRedundancyService) updateMultiPathRoute(destination string) {
	multiPath, exists := rrs.multiPathRoutes[destination]
	if !exists {
		return
	}
	
	// 收集所有到该目标的路由
	allRoutes := []*RouteInfo{}
	for _, route := range rrs.routeMetrics {
		if route.IsActive {
			allRoutes = append(allRoutes, route)
		}
	}
	
	// 按评分排序
	sort.Slice(allRoutes, func(i, j int) bool {
		return allRoutes[i].RouteScore() > allRoutes[j].RouteScore()
	})
	
	// 更新主路径和备用路径
	if len(allRoutes) > 0 {
		multiPath.PrimaryPath = allRoutes[0]
		
		// 设置备用路径
		multiPath.BackupPaths = []*RouteInfo{}
		for i := 1; i < len(allRoutes) && i <= rrs.maxBackupPaths; i++ {
			multiPath.BackupPaths = append(multiPath.BackupPaths, allRoutes[i])
		}
	}
	
	multiPath.LastUpdate = time.Now()
}

// updateRouteMetrics 更新路由指标
func (rrs *RouteRedundancyService) updateRouteMetrics() {
	networkInfo := rrs.pineconeService.GetNetworkInfo()
	peers, ok := networkInfo["peers"].([]map[string]interface{})
	if !ok {
		return
	}
	
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	// 标记所有路由为非活跃
	for _, route := range rrs.routeMetrics {
		route.IsActive = false
	}
	
	// 更新活跃路由
	for _, peer := range peers {
		if peerID, ok := peer["id"].(string); ok {
			if route, exists := rrs.routeMetrics[peerID]; exists {
				route.IsActive = true
				
				// 更新带宽信息（如果可用）
				if bandwidth, ok := peer["bandwidth"].(int64); ok {
					route.Bandwidth = bandwidth
				}
				
				// 更新跳数信息（如果可用）
				if hopCount, ok := peer["hop_count"].(int); ok {
					route.HopCount = hopCount
				}
			} else {
				// 新发现的peer，创建路由信息
				newRoute := &RouteInfo{
					PeerID:      peerID,
					Latency:     100 * time.Millisecond,
					Reliability: 0.8, // 新路由默认较低可靠性
					Bandwidth:   1024 * 1024,
					HopCount:    1,
					LastTested:  time.Now(),
					IsActive:    true,
				}
				rrs.routeMetrics[peerID] = newRoute
				
				// 创建多路径路由
				multiPath := &MultiPathRoute{
					Destination: peerID,
					PrimaryPath: newRoute,
					BackupPaths: []*RouteInfo{},
					LastUpdate:  time.Now(),
				}
				rrs.multiPathRoutes[peerID] = multiPath
				
				// 发现新路由，静默添加
			}
		}
	}
}

// handleRouterEvent 处理路由器事件
func (rrs *RouteRedundancyService) handleRouterEvent(event events.Event) {
	switch e := event.(type) {
	case events.PeerAdded:
		// 检测到新节点，添加到路由表
		rrs.addRoute(e.PeerID)
		
	case events.PeerRemoved:
		// 检测到节点断开，标记路由失效
		rrs.markRouteInactive(e.PeerID)
		
	case events.TreeParentUpdate:
		// 树形拓扑更新，重新评估路由
		rrs.reevaluateRoutes()
	}
}

// addRoute 添加新路由
func (rrs *RouteRedundancyService) addRoute(peerID string) {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	// 检查是否已存在
	if _, exists := rrs.routeMetrics[peerID]; exists {
		return
	}
	
	// 创建新路由
	newRoute := &RouteInfo{
		PeerID:      peerID,
		Latency:     100 * time.Millisecond,
		Reliability: 0.8,
		Bandwidth:   1024 * 1024,
		HopCount:    1,
		LastTested:  time.Now(),
		IsActive:    true,
	}
	rrs.routeMetrics[peerID] = newRoute
	
	// 创建多路径路由
	multiPath := &MultiPathRoute{
		Destination: peerID,
		PrimaryPath: newRoute,
		BackupPaths: []*RouteInfo{},
		LastUpdate:  time.Now(),
	}
	rrs.multiPathRoutes[peerID] = multiPath
}

// markRouteInactive 标记路由为非活跃
func (rrs *RouteRedundancyService) markRouteInactive(peerID string) {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	if route, exists := rrs.routeMetrics[peerID]; exists {
		route.IsActive = false
		route.FailureCount++
	}
	
	// 更新多路径路由
	rrs.updateMultiPathRoute(peerID)
}

// reevaluateRoutes 重新评估所有路由
func (rrs *RouteRedundancyService) reevaluateRoutes() {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	for destination := range rrs.multiPathRoutes {
		rrs.updateMultiPathRoute(destination)
	}
}

// pingPeer 测试peer连通性
func (rrs *RouteRedundancyService) pingPeer(peerID string) error {
	// 这里应该实现实际的ping逻辑
	// 由于接口限制，这里返回nil表示成功
	return nil
}

// GetBestRoute 获取到指定目标的最佳路由
func (rrs *RouteRedundancyService) GetBestRoute(destination string) *RouteInfo {
	rrs.mu.RLock()
	defer rrs.mu.RUnlock()
	
	if multiPath, exists := rrs.multiPathRoutes[destination]; exists {
		return multiPath.GetBestRoute()
	}
	
	return nil
}

// GetNextRoute 获取下一个路由（负载均衡）
func (rrs *RouteRedundancyService) GetNextRoute(destination string) *RouteInfo {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	if multiPath, exists := rrs.multiPathRoutes[destination]; exists {
		return multiPath.GetNextRoute()
	}
	
	return nil
}

// UpdateRoutes 更新路由信息
func (rrs *RouteRedundancyService) UpdateRoutes() error {
	// 重新评估所有路由
	rrs.reevaluateRoutes()
	
	return nil
}

// GetStats 获取路由冗余统计信息
func (rrs *RouteRedundancyService) GetStats() map[string]interface{} {
	rrs.mu.RLock()
	defer rrs.mu.RUnlock()
	
	// 统计启用负载均衡的路由数量
	loadBalancingCount := 0
	for _, multiPath := range rrs.multiPathRoutes {
		if multiPath.LoadBalance {
			loadBalancingCount++
		}
	}
	
	return map[string]interface{}{
		"multi_path_routes": len(rrs.multiPathRoutes),
		"route_metrics":     len(rrs.routeMetrics),
		"is_running":        rrs.isRunning,
		"load_balancing":    loadBalancingCount,
		"max_backup_paths":  rrs.maxBackupPaths,
	}
}

// EnableLoadBalancing 启用负载均衡
func (rrs *RouteRedundancyService) EnableLoadBalancing(destination string) {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	if multiPath, exists := rrs.multiPathRoutes[destination]; exists {
		multiPath.LoadBalance = true
	}
}

// DisableLoadBalancing 禁用负载均衡
func (rrs *RouteRedundancyService) DisableLoadBalancing(destination string) {
	rrs.mu.Lock()
	defer rrs.mu.Unlock()
	
	if multiPath, exists := rrs.multiPathRoutes[destination]; exists {
		multiPath.LoadBalance = false
	}
}

// GetRouteStats 获取路由统计信息
func (rrs *RouteRedundancyService) GetRouteStats() map[string]interface{} {
	rrs.mu.RLock()
	defer rrs.mu.RUnlock()
	
	stats := map[string]interface{}{
		"total_routes":     len(rrs.routeMetrics),
		"active_routes":    0,
		"multipath_routes": len(rrs.multiPathRoutes),
		"backup_paths":     0,
	}
	
	for _, route := range rrs.routeMetrics {
		if route.IsActive {
			stats["active_routes"] = stats["active_routes"].(int) + 1
		}
	}
	
	for _, multiPath := range rrs.multiPathRoutes {
		stats["backup_paths"] = stats["backup_paths"].(int) + len(multiPath.BackupPaths)
	}
	
	return stats
}

// GetMultiPathRoutes 获取所有多路径路由信息
func (rrs *RouteRedundancyService) GetMultiPathRoutes() map[string]*MultiPathRoute {
	rrs.mu.RLock()
	defer rrs.mu.RUnlock()
	
	result := make(map[string]*MultiPathRoute)
	for k, v := range rrs.multiPathRoutes {
		result[k] = v
	}
	return result
}