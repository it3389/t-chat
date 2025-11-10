package network

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"t-chat/internal/account"
	"t-chat/internal/friend"
	"t-chat/internal/pinecone/bluetooth"
	"t-chat/internal/pinecone/router"
	"t-chat/internal/pinecone/types"
)

// 动态缓冲区管理
type DynamicBufferManager struct {
	mu                    sync.RWMutex
	currentBufferSize     int           // 当前缓冲区大小
	minBufferSize         int           // 最小缓冲区大小
	maxBufferSize         int           // 最大缓冲区大小
	averageMessageSize    int           // 平均消息大小
	messageSizeHistory    []int         // 消息大小历史记录
	historySize           int           // 历史记录大小
	adjustmentThreshold   float64       // 调整阈值
	lastAdjustmentTime    time.Time     // 上次调整时间
	adjustmentInterval    time.Duration // 调整间隔
	bufferPool            sync.Pool     // 缓冲区池
}

// NewDynamicBufferManager 创建动态缓冲区管理器
func NewDynamicBufferManager() *DynamicBufferManager {
	dbm := &DynamicBufferManager{
		currentBufferSize:   4 * 1024,  // 初始4KB
		minBufferSize:       1 * 1024,  // 最小1KB
		maxBufferSize:       32 * 1024, // 最大32KB
		averageMessageSize:  2 * 1024,  // 初始平均2KB
		messageSizeHistory:  make([]int, 0, 100),
		historySize:         100,
		adjustmentThreshold: 0.8, // 80%阈值
		adjustmentInterval:  5 * time.Second,
		lastAdjustmentTime:  time.Now(),
	}
	
	dbm.bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, dbm.getCurrentBufferSize())
		},
	}
	
	return dbm
}

// getCurrentBufferSize 获取当前缓冲区大小
func (dbm *DynamicBufferManager) getCurrentBufferSize() int {
	dbm.mu.RLock()
	defer dbm.mu.RUnlock()
	return dbm.currentBufferSize
}

// getBuffer 获取缓冲区
func (dbm *DynamicBufferManager) getBuffer() []byte {
	buf := dbm.bufferPool.Get().([]byte)
	currentSize := dbm.getCurrentBufferSize()
	
	// 如果缓冲区大小不匹配，重新分配
	if cap(buf) != currentSize {
		buf = make([]byte, currentSize)
	}
	
	return buf[:currentSize]
}

// putBuffer 归还缓冲区
func (dbm *DynamicBufferManager) putBuffer(buf []byte) {
	currentSize := dbm.getCurrentBufferSize()
	
	// 只归还大小匹配的缓冲区
	if cap(buf) == currentSize {
		dbm.bufferPool.Put(buf[:currentSize])
	}
	// 大小不匹配的缓冲区让GC回收
}

// recordMessageSize 记录消息大小并可能触发调整
func (dbm *DynamicBufferManager) recordMessageSize(size int) {
	dbm.mu.Lock()
	defer dbm.mu.Unlock()
	
	// 添加到历史记录
	dbm.messageSizeHistory = append(dbm.messageSizeHistory, size)
	
	// 保持历史记录大小限制
	if len(dbm.messageSizeHistory) > dbm.historySize {
		dbm.messageSizeHistory = dbm.messageSizeHistory[1:]
	}
	
	// 更新平均消息大小
	dbm.updateAverageMessageSize()
	
	// 检查是否需要调整缓冲区大小
	if time.Since(dbm.lastAdjustmentTime) >= dbm.adjustmentInterval {
		dbm.adjustBufferSize()
		dbm.lastAdjustmentTime = time.Now()
	}
}

// updateAverageMessageSize 更新平均消息大小
func (dbm *DynamicBufferManager) updateAverageMessageSize() {
	if len(dbm.messageSizeHistory) == 0 {
		return
	}
	
	total := 0
	for _, size := range dbm.messageSizeHistory {
		total += size
	}
	
	dbm.averageMessageSize = total / len(dbm.messageSizeHistory)
}

// adjustBufferSize 调整缓冲区大小
func (dbm *DynamicBufferManager) adjustBufferSize() {
	if len(dbm.messageSizeHistory) < 10 {
		return // 样本不足，不调整
	}
	
	// 计算95百分位数作为目标缓冲区大小
	sortedSizes := make([]int, len(dbm.messageSizeHistory))
	copy(sortedSizes, dbm.messageSizeHistory)
	
	// 简单排序
	for i := 0; i < len(sortedSizes)-1; i++ {
		for j := i + 1; j < len(sortedSizes); j++ {
			if sortedSizes[i] > sortedSizes[j] {
				sortedSizes[i], sortedSizes[j] = sortedSizes[j], sortedSizes[i]
			}
		}
	}
	
	// 计算95百分位数
	percentile95Index := int(float64(len(sortedSizes)) * 0.95)
	if percentile95Index >= len(sortedSizes) {
		percentile95Index = len(sortedSizes) - 1
	}
	
	targetSize := sortedSizes[percentile95Index]
	
	// 添加20%的缓冲
	targetSize = int(float64(targetSize) * 1.2)
	
	// 确保在合理范围内
	if targetSize < dbm.minBufferSize {
		targetSize = dbm.minBufferSize
	} else if targetSize > dbm.maxBufferSize {
		targetSize = dbm.maxBufferSize
	}
	
	// 只有当变化超过阈值时才调整
	changeRatio := float64(abs(targetSize-dbm.currentBufferSize)) / float64(dbm.currentBufferSize)
	if changeRatio >= dbm.adjustmentThreshold {
		oldSize := dbm.currentBufferSize
		dbm.currentBufferSize = targetSize
		
		// 清空缓冲池，强制重新分配
		dbm.bufferPool = sync.Pool{
			New: func() interface{} {
				return make([]byte, dbm.currentBufferSize)
			},
		}
		
		// 这里可以添加日志记录调整信息
		_ = oldSize // 避免未使用变量警告
	}
}

// abs 计算绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// getStats 获取缓冲区管理统计信息
func (dbm *DynamicBufferManager) getStats() map[string]interface{} {
	dbm.mu.RLock()
	defer dbm.mu.RUnlock()
	
	return map[string]interface{}{
		"current_buffer_size":  dbm.currentBufferSize,
		"average_message_size": dbm.averageMessageSize,
		"message_count":        len(dbm.messageSizeHistory),
		"min_buffer_size":      dbm.minBufferSize,
		"max_buffer_size":      dbm.maxBufferSize,
	}
}

// 全局动态缓冲区管理器
var globalBufferManager = NewDynamicBufferManager()

// PineconeService Pinecone服务
// 实现 PineconeServiceInterface 接口
type PineconeService struct {
	*NetworkService

	privateKey             []byte
	publicKey              []byte
	router                 *router.Router
	peers                  map[string]*PeerInfo
	peersMutex             sync.RWMutex
	messageDispatcher      MessageDispatcherInterface
	messageChannel         chan *Message
	friendSearchResponseHandler func(username string, pubkey string, addr string)

	// 服务依赖
	bluetoothService       BluetoothServiceInterface
	mdnsService            MDNSServiceInterface
	// 路由发现和冗余服务
	routeDiscoveryService  RouteDiscoveryServiceInterface
	routeRedundancyService RouteRedundancyServiceInterface

	// 运行状态
	isRunning              bool
	mu                     sync.RWMutex
	ctx                    context.Context
	cancel                 context.CancelFunc
	logger                 Logger
	startTime              time.Time

	// 多接口监听支持
	listeners              []net.Listener // 多个监听器
	listeningAddresses     []string       // 监听的地址列表

	// 错误处理
	errorHandler           *ErrorHandler

	// 消息确认
	pendingAcks            sync.Map // map[string]chan bool

	// 用户和主机映射
	UserHostMap            map[string]string
	PeerHostMap            map[string]string

	// 清理相关
	cleanupTicker          *time.Ticker
	lastConnectionCount    int       // 上次连接数

	// 性能监控相关字段
	messageDropCount       int64  // 消息丢弃计数
	messageProcessedCount  int64  // 消息处理计数
	queueUsageStats        sync.Map // 队列使用率统计
	metricsStartTime       time.Time // 指标统计开始时间
	
	// 优雅降级机制
	priorityQueue          *PriorityQueue // 优先级队列
	gracefulDegradation    bool           // 是否启用优雅降级
	highLoadThreshold      float64        // 高负载阈值
	
	// 动态工作池
	workerPool             *WorkerPool    // 动态消息处理工作池
	
	// Ping管理器
	pingManager            *PingManager   // ping测试管理器
	
	// 背压控制已移除
}

// 确保 PineconeService 实现了 PineconeServiceInterface 接口
var _ PineconeServiceInterface = (*PineconeService)(nil)

// LoggerAdapter 适配器，将 Logger 接口适配到 pinecone router 所需的 Logger 接口
type LoggerAdapter struct {
	logger Logger
}

// Printf 实现 router.Logger 接口的 Printf 方法
func (l *LoggerAdapter) Printf(format string, v ...interface{}) {
	l.logger.Infof(format, v...)
}

// BluetoothServiceWrapper 蓝牙服务包装器，用于适配不同的蓝牙服务实现
type BluetoothServiceWrapper struct {
	service      interface{} // 可以是任何蓝牙服务实现
	getPeersFunc func() []BluetoothPeerInfo
}

// NewBluetoothServiceAdapter 创建蓝牙服务适配器
func NewBluetoothServiceAdapter(bluetoothService interface{}) *BluetoothServiceWrapper {
	return &BluetoothServiceWrapper{
		service: bluetoothService,
	}
}

// SetGetPeersFunc 设置获取蓝牙设备的函数
func (bsw *BluetoothServiceWrapper) SetGetPeersFunc(fn func() []BluetoothPeerInfo) {
	bsw.getPeersFunc = fn
}

// Start 启动蓝牙服务
func (bsw *BluetoothServiceWrapper) Start() error {
	if bs, ok := bsw.service.(interface{ Start() error }); ok {
		return bs.Start()
	}
	return fmt.Errorf("service does not support Start method")
}

// Stop 停止蓝牙服务
func (bsw *BluetoothServiceWrapper) Stop() error {
	if bs, ok := bsw.service.(interface{ Stop() error }); ok {
		return bs.Stop()
	}
	return fmt.Errorf("service does not support Stop method")
}

// IsStarted 检查蓝牙服务是否已启动
func (bsw *BluetoothServiceWrapper) IsStarted() bool {
	if bs, ok := bsw.service.(interface{ IsStarted() bool }); ok {
		return bs.IsStarted()
	}
	return false
}

// GetDiscoveredPeers 获取发现的蓝牙设备
func (bsw *BluetoothServiceWrapper) GetDiscoveredPeers() []interface{} {
	if bsw.getPeersFunc != nil {
		peers := bsw.getPeersFunc()
		result := make([]interface{}, len(peers))
		for i, peer := range peers {
			result[i] = peer
		}
		return result
	}
	// 处理bluetooth.BluetoothService类型
	if bs, ok := bsw.service.(*bluetooth.BluetoothService); ok {
		originalPeers := bs.GetDiscoveredPeers()
		result := make([]interface{}, len(originalPeers))
		for i, peer := range originalPeers {
			result[i] = peer
		}
		return result
	}
	return []interface{}{}
}

func (l *LoggerAdapter) Println(v ...interface{}) {
	l.logger.Infof("%v", v)
}

// NewPineconeService 创建Pinecone服务
func NewPineconeService(config *NetworkConfig, logger Logger) *PineconeService {
	ctx, cancel := context.WithCancel(context.Background())

	// 随机生成密钥对
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		logger.Errorf("生成Pinecone密钥失败: %v", err)
		return nil
	}

	// 创建Pinecone路由器
	loggerAdapter := &LoggerAdapter{logger: logger}
	pineconeRouter := router.NewRouter(loggerAdapter, ed25519.PrivateKey(privateKey))

	// 创建消息分发器
	messageDispatcher := NewMessageDispatcher(logger)

	ps := &PineconeService{
		NetworkService: &NetworkService{
			name:   "PineconeService",
			config: config,
			logger: logger,
		},
		privateKey:     privateKey,
		publicKey:      publicKey,
		router:         pineconeRouter,
		messageDispatcher: messageDispatcher,
		peers:          make(map[string]*PeerInfo),
		ctx:            ctx,
		cancel:         cancel,
		messageChannel: make(chan *Message, 500),
		UserHostMap:    make(map[string]string),
		PeerHostMap:    make(map[string]string),
		logger:         logger,
		startTime:      time.Now(),
		metricsStartTime: time.Now(),
		priorityQueue:  NewPriorityQueue(200), // 优先级队列容量为200
		gracefulDegradation: true,
		highLoadThreshold: 0.8, // 80%负载阈值
		workerPool: NewWorkerPool(2, 8, 100, logger), // 最小2个，最大8个worker，队列大小100
	}

	// 初始化错误处理器
	ps.errorHandler = NewErrorHandler(logger)
	
	// 初始化路由发现服务
	ps.routeDiscoveryService = NewRouteDiscoveryService(ps, logger)
	
	// 初始化路由冗余服务
	ps.routeRedundancyService = NewRouteRedundancyService(ps, logger)
	
	// 初始化ping管理器
	ps.pingManager = NewPingManager(ps, logger)
	
	// 注册消息处理器
	ps.registerMessageHandlers()

	return ps
}

// NewPineconeServiceWithKeys 使用指定密钥对创建Pinecone服务
func NewPineconeServiceWithKeys(config *NetworkConfig, logger Logger, privateKey, publicKey []byte) *PineconeService {
	ctx, cancel := context.WithCancel(context.Background())
	
	// 创建Pinecone路由器
	loggerAdapter := &LoggerAdapter{logger: logger}
	pineconeRouter := router.NewRouter(loggerAdapter, ed25519.PrivateKey(privateKey))
	
	// 创建消息分发器
	messageDispatcher := NewMessageDispatcher(logger)
	
	ps := &PineconeService{
		NetworkService: &NetworkService{
			name:   "PineconeService",
			config: config,
			logger: logger,
		},
		privateKey:     privateKey,
		publicKey:      publicKey,
		peers:          make(map[string]*PeerInfo),
		router:         pineconeRouter,
		ctx:            ctx,
		cancel:         cancel,
		messageDispatcher: messageDispatcher,
		messageChannel: make(chan *Message, 500),
		UserHostMap:    make(map[string]string),
		PeerHostMap:    make(map[string]string),
		startTime:      time.Now(),
		logger:         logger, // 直接设置logger字段
		metricsStartTime: time.Now(),
		priorityQueue:  NewPriorityQueue(200), // 优先级队列容量为200
		gracefulDegradation: true,
		highLoadThreshold: 0.8, // 80%负载阈值
		workerPool: NewWorkerPool(2, 8, 100, logger), // 最小2个，最大8个worker，队列大小100
	}
	
	// 初始化错误处理器
	ps.errorHandler = NewErrorHandler(logger)
	
	// 初始化路由发现服务
	ps.routeDiscoveryService = NewRouteDiscoveryService(ps, logger)
	
	// 初始化路由冗余服务
	ps.routeRedundancyService = NewRouteRedundancyService(ps, logger)
	
	// 初始化ping管理器
	ps.pingManager = NewPingManager(ps, logger)
	
	// 注册消息处理器
	ps.registerMessageHandlers()
	
	return ps
}

// Start 启动Pinecone服务
func (ps *PineconeService) Start(ctx context.Context) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	
	if ps.isRunning {
		return fmt.Errorf("Pinecone服务已在运行")
	}
	
	// 设置调试日志记录器用于对象池监控
	SetDebugLogger(ps.logger)
	ps.logger.Infof("启动Pinecone服务...")
	
	// 获取默认端口
	defaultPort := "7777"
	if ps.config != nil && ps.config.PineconeListen != "" {
		// 从配置中提取端口号
		if strings.Contains(ps.config.PineconeListen, ":") {
			parts := strings.Split(ps.config.PineconeListen, ":")
			if len(parts) > 1 {
				defaultPort = parts[len(parts)-1]
			}
		}
	}
	
	// 启动多接口监听器
	err := ps.startMultiInterfaceListeners(defaultPort)
	if err != nil {
		return fmt.Errorf("启动多接口监听器失败: %v", err)
	}
	
	// 启动消息接收协程
	go ps.startMessageReceiver()
	
	// 启动优先级队列处理器
	go ps.startPriorityQueueProcessor()
	
	// 启动动态工作池
	ps.workerPool.Start()
	
	// 背压控制器已移除
	
	// 连接到配置的对等节点（使用工作池模式，减少goroutine数量）
	if ps.config != nil && len(ps.config.PineconePeers) > 0 {
		// 使用单个goroutine批量连接对等节点
		go func() {
			for _, peerAddr := range ps.config.PineconePeers {
				ps.connectToPeer(peerAddr)
				// 添加小延迟避免连接风暴
				time.Sleep(100 * time.Millisecond)
			}
		}()
	}
	
	// 异步启动路由发现服务
	if ps.routeDiscoveryService != nil {
		go func() {
			if err := ps.routeDiscoveryService.Start(); err != nil {
				ps.logger.Warnf("启动路由发现服务失败: %v", err)
			}
		}()
	}

	// 异步启动路由冗余服务
	if ps.routeRedundancyService != nil {
		go func() {
			if err := ps.routeRedundancyService.Start(); err != nil {
				ps.logger.Warnf("启动路由冗余服务失败: %v", err)
			}
		}()
	}
	
	// 启动定期清理过期节点的定时器 - 优化：从5分钟增加到15分钟，减少CPU使用
	ps.cleanupTicker = time.NewTicker(15 * time.Minute)
	go ps.startPeerCleanup()
	
	ps.isRunning = true
	// Pinecone服务已启动
	
	return nil
}

// Stop 停止Pinecone服务
func (ps *PineconeService) Stop() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	
	if !ps.isRunning {
		return nil
	}
	
	ps.cancel()
	
	// 关闭所有监听器
	for i, listener := range ps.listeners {
		if listener != nil {
			ps.logger.Infof("关闭监听器: %s", ps.listeningAddresses[i])
			if err := listener.Close(); err != nil {
				ps.logger.Warnf("关闭监听器失败 (%s): %v", ps.listeningAddresses[i], err)
			}
		}
	}
	// 清空监听器列表
	ps.listeners = nil
	ps.listeningAddresses = nil
	
	// 停止路由发现服务
	if ps.routeDiscoveryService != nil {
		if err := ps.routeDiscoveryService.Stop(); err != nil {
			ps.logger.Warnf("停止路由发现服务失败: %v", err)
		}
	}
	
	// 停止路由冗余服务
	if ps.routeRedundancyService != nil {
		if err := ps.routeRedundancyService.Stop(); err != nil {
			ps.logger.Warnf("停止路由冗余服务失败: %v", err)
		}
	}
	
	// 停止动态工作池
	if ps.workerPool != nil {
		ps.workerPool.Stop()
	}
	
	// 背压控制器已移除
	
	// 停止错误处理器
	if ps.errorHandler != nil {
		ps.errorHandler.Stop()
	}
	
	// 停止清理定时器
	if ps.cleanupTicker != nil {
		ps.cleanupTicker.Stop()
		ps.cleanupTicker = nil
	}

	// 清理所有待确认的消息
	ps.pendingAcks.Range(func(key, value interface{}) bool {
		if ackChan, ok := value.(chan bool); ok {
			select {
			case ackChan <- false: // 通知失败
			default:
				// 通道已满或已关闭，忽略
			}
		}
		ps.pendingAcks.Delete(key)
		return true
	})

	// 关闭Pinecone路由器
	if ps.router != nil {
		ps.router.Close()
	}
	
	ps.isRunning = false
	// Pinecone服务已停止
	
	return nil
}

// Connect 连接到对等节点（通过Pinecone路由器自动管理）
func (ps *PineconeService) Connect(peerID string) error {
	// Pinecone协议中连接是自动管理的，这里只是为了兼容接口
	// Pinecone连接由路由器自动管理
	return nil
}

// ConnectToPeer 连接到指定的对等节点
func (ps *PineconeService) ConnectToPeer(peerAddr string) error {
	ps.connectToPeer(peerAddr)
	return nil
}

// Disconnect 断开与对等节点的连接（通过Pinecone路由器自动管理）
func (ps *PineconeService) Disconnect(peerID string) error {
	// Pinecone协议中连接是自动管理的，这里只是为了兼容接口
	// Pinecone连接由路由器自动管理
	return nil
}

// SendMessage 发送消息到对等节点
func (ps *PineconeService) SendMessage(peerID string, message *Message) error {
	// 发送消息
	
	// 确保消息有ID
	if message.ID == "" {
		message.ID = fmt.Sprintf("%d_%s", time.Now().UnixNano(), hex.EncodeToString(ps.publicKey)[:8])
	}
	
	// 创建消息包
	packet := &MessagePacket{
		ID:        message.ID,
		Type:      message.Type,
		From:      hex.EncodeToString(ps.publicKey),
		To:        peerID,
		Timestamp: time.Now(),
		Content:   message.Content,
		Metadata:  message.Metadata,
	}
	
	// 发送消息包
	return ps.SendMessagePacket(peerID, packet)
}



// GetConnectedPeers 获取已连接的对等节点
func (ps *PineconeService) GetConnectedPeers() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	if ps.router == nil {
		return []string{}
	}
	
	// 获取自己的公钥用于过滤
	myPublicKey := fmt.Sprintf("%x", ps.publicKey)
	
	// 从Pinecone路由器获取真实的对等节点
	pineconeePeers := ps.router.Peers()
	seenPubKeys := make(map[string]bool) // 用于去重
	peers := make([]string, 0, len(pineconeePeers))
	
	for _, peer := range pineconeePeers {
		// 过滤掉本地节点（port为0的是本地节点）
		if peer.Port == 0 {
			continue
		}
		
		// 过滤掉自己的公钥
		if peer.PublicKey == myPublicKey {
			continue
		}
		
		// 去重：如果已经见过这个公钥，跳过
		if seenPubKeys[peer.PublicKey] {
			continue
		}
		
		seenPubKeys[peer.PublicKey] = true
		peers = append(peers, peer.PublicKey)
	}
	
	return peers
}

// SetMessageHandler 设置消息处理器（已弃用，使用SetMessageDispatcher）
func (ps *PineconeService) SetMessageHandler(handler MessageHandlerInterface) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	
	// 为了向后兼容，将单个处理器注册到分发器中
	if ps.messageDispatcher != nil && handler != nil {
		if err := ps.messageDispatcher.RegisterHandler(handler); err != nil {
			ps.logger.Errorf("注册消息处理器失败: %v", err)
		}
	}
}

// SetMessageDispatcher 设置消息分发器
func (ps *PineconeService) SetMessageDispatcher(dispatcher MessageDispatcherInterface) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	
	ps.messageDispatcher = dispatcher
}

// TestDispatchMessage 测试消息分发（仅用于测试）
func (ps *PineconeService) TestDispatchMessage(message *Message) {
	if ps.messageDispatcher != nil {
		ps.messageDispatcher.DispatchMessage(message)
	} else {
		ps.logger.Warnf("消息分发器未初始化")
	}
}

// GetHandlerCount 获取已注册的处理器数量（仅用于测试）
func (ps *PineconeService) GetHandlerCount() int {
	if ps.messageDispatcher != nil {
		return ps.messageDispatcher.GetHandlerCount()
	}
	return 0
}

// GetPublicKey 获取公钥
func (ps *PineconeService) GetPublicKey() []byte {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.publicKey
}

// GetRouter 获取Pinecone路由器实例
func (ps *PineconeService) GetRouter() *router.Router {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.router
}

// SetKeys 设置密钥对并创建Pinecone路由器
func (ps *PineconeService) SetKeys(privateKey, publicKey []byte) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	
	ps.privateKey = privateKey
	ps.publicKey = publicKey
	
	// 如果提供了私钥，创建Pinecone路由器
	if privateKey != nil {
		loggerAdapter := &LoggerAdapter{logger: ps.logger}
		ps.router = router.NewRouter(loggerAdapter, ed25519.PrivateKey(privateKey))
		// Pinecone路由器已创建
	}
	
	// 如果messageDispatcher未初始化，创建并注册处理器
	if ps.messageDispatcher == nil {
		ps.messageDispatcher = NewMessageDispatcher(ps.logger)
		ps.registerMessageHandlers()
		// 消息分发器已创建
	}
}

// GetBufferStats 获取动态缓冲区管理统计信息
func (ps *PineconeService) GetBufferStats() map[string]interface{} {
	return globalBufferManager.getStats()
}

// GetBufferManagerInfo 获取缓冲区管理器详细信息
func (ps *PineconeService) GetBufferManagerInfo() map[string]interface{} {
	stats := globalBufferManager.getStats()
	
	// 添加额外的运行时信息
	stats["adjustment_threshold"] = globalBufferManager.adjustmentThreshold
	stats["adjustment_interval_seconds"] = globalBufferManager.adjustmentInterval.Seconds()
	stats["history_size"] = globalBufferManager.historySize
	
	return stats
}

// SetBluetoothService 设置蓝牙服务
func (ps *PineconeService) SetBluetoothService(bluetoothService BluetoothServiceInterface) {
	// 优化：使用读写锁的写锁，减少锁竞争
	// 快速设置服务引用，减少锁持有时间
	ps.mu.Lock()
	ps.bluetoothService = bluetoothService
	ps.mu.Unlock()
	
	if ps.logger != nil {
		// 蓝牙服务已设置
	}
}

// SetMDNSService 设置MDNS服务
func (ps *PineconeService) SetMDNSService(mdnsService MDNSServiceInterface) {
	// 优化：使用读写锁的写锁，减少锁竞争
	// 同时使用defer确保锁一定会被释放
	ps.mu.Lock()
	ps.mdnsService = mdnsService
	ps.mu.Unlock()
	
	if ps.logger != nil {
		// MDNS服务已设置
	}
	
	// 设置MDNS发现回调，当发现新节点时自动尝试连接
	// 注意：回调函数在锁外设置，避免潜在的死锁
	if mdnsService != nil {
		mdnsService.SetDiscoveryCallback(func(peer PeerInfo) {
			ps.handleMDNSDiscoveredPeer(peer)
		})
	}
}

// SetHeartbeatService removed - heartbeat mechanism disabled

// handleMDNSDiscoveredPeer 处理MDNS发现的节点
func (ps *PineconeService) handleMDNSDiscoveredPeer(peer PeerInfo) {
	// MDNS发现新节点
	ps.logger.Infof("🔍 [DEBUG] 收到节点发现回调: %s (地址: %s:%d, 公钥: %s)", peer.Username, peer.Address, peer.Port, peer.PublicKey)
	
	// 检查是否是自己的节点
	if peer.PublicKey != "" && peer.PublicKey == hex.EncodeToString(ps.publicKey) {
		ps.logger.Debugf("跳过自己的节点: %s", peer.Username)
		return
	}
	
	// 异步处理节点发现，避免阻塞MDNS回调
	go func() {
		// 存储用户名到公钥的映射关系
		if peer.Username != "" && peer.PublicKey != "" {
			ps.mu.Lock()
			if ps.peers == nil {
				ps.peers = make(map[string]*PeerInfo)
			}
			
			// 检查是否已经存储过这个节点
			if existingPeer, exists := ps.peers[peer.PublicKey]; exists {
				// 节点已存在，跳过重复连接
				// 更新最后发现时间
				existingPeer.LastSeen = time.Now()
				ps.mu.Unlock()
				return
			}
			
			// 使用公钥作为key存储PeerInfo
			peer.LastSeen = time.Now()
			ps.peers[peer.PublicKey] = &peer
			ps.mu.Unlock()
			// 保存用户名映射
			// 当前存储的所有公钥
		}
		
		// 构建完整的Pinecone地址（IP:Port格式）
		var pineconeAddr string
		if peer.Port > 0 {
			pineconeAddr = fmt.Sprintf("pinecone://%s:%d", peer.Address, peer.Port)
		} else {
			// 如果没有端口信息，使用默认端口7777
			pineconeAddr = fmt.Sprintf("pinecone://%s:7777", peer.Address)
		}
		
		// 尝试连接到Pinecone地址
		
		// 尝试连接到发现的节点
		ps.connectToPeer(pineconeAddr)
	}()
}

// getBluetoothStatus 获取蓝牙服务状态
func (ps *PineconeService) getBluetoothStatus() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.bluetoothService == nil {
		return false
	}
	return ps.bluetoothService.IsStarted()
}

// getBluetoothPeers 获取蓝牙设备列表
func (ps *PineconeService) getBluetoothPeers() []map[string]interface{} {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.bluetoothService == nil {
		return []map[string]interface{}{}
	}
	
	peers := ps.bluetoothService.GetDiscoveredPeers()
	result := make([]map[string]interface{}, 0, len(peers))
	for _, peerInterface := range peers {
		if peer, ok := peerInterface.(*bluetooth.BluetoothPeer); ok {
			// 转换为BluetoothPeerInfo格式
			peerInfo := BluetoothPeerInfo{
				Name:      peer.Name,
				Address:   peer.Address,
				PublicKey: peer.PublicKey,
				Connected: peer.Connected,
				LastSeen:  peer.LastSeen,
			}
			
			peerMap := map[string]interface{}{
				"name":       peerInfo.Name,
				"address":    peerInfo.Address,
				"public_key": peerInfo.PublicKey,
				"connected":  peerInfo.Connected,
				"last_seen":  peerInfo.LastSeen,
			}
			result = append(result, peerMap)
		}
	}
	return result
}

// friendListImpl 好友列表实现
type friendListImpl struct {
	service *PineconeService
}

// AddFriend 添加好友
func (f *friendListImpl) AddFriend(username, pubkey string) error {
	// 这里可以实现添加好友的逻辑
	return nil
}

// RemoveFriend 删除好友
func (f *friendListImpl) RemoveFriend(username string) error {
	// 这里可以实现删除好友的逻辑
	return nil
}

// GetFriends 获取好友列表
func (f *friendListImpl) GetFriends() []map[string]string {
	// 这里可以实现获取好友列表的逻辑
	return []map[string]string{}
}

// IsFriend 检查是否为好友
func (f *friendListImpl) IsFriend(username string) bool {
	// 这里可以实现检查好友的逻辑
	return false
}

// GetAllFriends 获取所有好友列表
func (f *friendListImpl) GetAllFriends() []*friend.Friend {
	// 这里可以实现获取好友列表的逻辑
	// 暂时返回空列表
	return []*friend.Friend{}
}

// GetCurrentAccount 获取当前账户信息
func (f *friendListImpl) GetCurrentAccount() *friend.Friend {
	// 从全局账户管理器获取当前账户
	currentAccount := account.GetCurrentAccount()
	if currentAccount == nil {
		return nil
	}
	
	// 转换为friend.Friend格式
	return &friend.Friend{
		Username: currentAccount.Username,
		// 其他字段保持默认值
	}
}

// FriendList 获取好友列表
func (ps *PineconeService) FriendList() FriendListLike {
	return &friendListImpl{
		service: ps,
	}
}

// GetPineconeAddr 获取Pinecone地址
func (ps *PineconeService) GetPineconeAddr() string {
	// 从配置中获取监听端口
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	if ps.config != nil && ps.config.PineconeListen != "" {
		return fmt.Sprintf("pinecone://localhost%s", ps.config.PineconeListen)
	}
	return "pinecone://localhost:7777" // 默认地址
}

// GetNodeID 获取节点ID（与GetPineconeAddr相同）
func (ps *PineconeService) GetNodeID() string {
	return ps.GetPineconeAddr()
}

// GetPublicKeyHex 获取公钥的十六进制字符串表示
func (ps *PineconeService) GetPublicKeyHex() string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return hex.EncodeToString(ps.publicKey)
}

// GetListeningAddress 获取第一个TCP监听地址
func (ps *PineconeService) GetListeningAddress() string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if len(ps.listeningAddresses) > 0 {
		return ps.listeningAddresses[0]
	}
	return ""
}

// GetNetworkInfo 获取网络信息
func (ps *PineconeService) GetNetworkInfo() map[string]interface{} {
	// 第一步：获取基本信息（快速获取锁并释放）
	ps.mu.RLock()
	nodeID := ""
	if len(ps.publicKey) > 0 {
		nodeID = fmt.Sprintf("%x", ps.publicKey)
	} else {
		nodeID = "unknown"
	}
	
	// 创建peers的本地副本
	peersMap := make(map[string]*PeerInfo)
	for k, v := range ps.peers {
		peersMap[k] = v
	}
	
	isRunning := ps.isRunning
	serviceName := ps.name
	
	// 获取配置信息
	pineconeAddr := "pinecone://localhost:7777" // 默认地址
	if ps.config != nil && ps.config.PineconeListen != "" {
		pineconeAddr = fmt.Sprintf("pinecone://localhost%s", ps.config.PineconeListen)
	}
	
	// 获取蓝牙服务引用（但不调用其方法）
	bluetoothServiceRef := ps.bluetoothService
	ps.mu.RUnlock()
	
	// 从Pinecone路由器获取真实的对等节点信息（在锁外进行，带超时保护）
	var peers []map[string]interface{}
	if ps.router != nil {
		// 使用带超时的context来获取peers信息，避免无限阻塞和goroutine泄露
		type peersResult struct {
			peers []router.PeerInfo
			err   error
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		resultChan := make(chan peersResult, 1)
		go func() {
			defer cancel() // 确保context被取消
			defer func() {
				if r := recover(); r != nil {
					select {
					case resultChan <- peersResult{nil, fmt.Errorf("router.Peers() panic: %v", r)}:
					case <-ctx.Done():
					}
				}
			}()
			pineconeePeers := ps.router.Peers()
			select {
			case resultChan <- peersResult{pineconeePeers, nil}:
			case <-ctx.Done():
			}
		}()
		
		// 等待结果或超时
		select {
		case result := <-resultChan:
			if result.err != nil {
				ps.logger.Warnf("获取router peers失败: %v", result.err)
				peers = []map[string]interface{}{}
			} else {
				pineconeePeers := result.peers
				seenPubKeys := make(map[string]bool) // 用于去重
				peers = make([]map[string]interface{}, 0, len(pineconeePeers))
			
				for _, peer := range pineconeePeers {
					// 过滤掉本地节点（port为0的是本地节点）
					if peer.Port == 0 {
						continue
					}
					
					// 过滤掉自己的公钥
					if peer.PublicKey == nodeID {
						continue
					}
					
					// 去重：如果已经见过这个公钥，跳过
					if seenPubKeys[peer.PublicKey] {
						continue
					}
					
					seenPubKeys[peer.PublicKey] = true
					
					// 查找真实的用户名（使用本地副本，避免死锁）
					username := fmt.Sprintf("peer_%s", peer.PublicKey[:8]) // 默认用户名
					// 查找用户名
					for _, peerInfo := range peersMap {
						if peerInfo.PublicKey == peer.PublicKey {
							username = peerInfo.Username
							// 找到用户名
							break
						}
					}
					
					peerMap := map[string]interface{}{
						"id":          peer.PublicKey,
						"username":    username,
						"public_key":  peer.PublicKey,
						"address":     peer.URI,
						"port":        peer.Port,
						"is_online":   true,
						"last_seen":   time.Now(),
						"peer_type":   peer.PeerType,
						"zone":        peer.Zone,
						"remote_ip":   peer.RemoteIP,
						"remote_port": peer.RemotePort,
					}
					peers = append(peers, peerMap)
				}
			}
		case <-time.After(3 * time.Second):
			ps.logger.Warnf("获取router peers超时，使用空列表")
			peers = []map[string]interface{}{}
		}
	} else {
		peers = []map[string]interface{}{}
	}
	
	// 获取真实的连接数（使用超时保护避免阻塞）
	connectedPeers := 0
	if ps.router != nil {
		type peerCountResult struct {
			count int
			err   error
		}
		
		peerCountChan := make(chan peerCountResult, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					peerCountChan <- peerCountResult{0, fmt.Errorf("router.TotalPeerCount() panic: %v", r)}
				}
			}()
			count := ps.router.TotalPeerCount()
			peerCountChan <- peerCountResult{count, nil}
		}()
		
		// 等待结果或超时
		select {
		case result := <-peerCountChan:
			if result.err != nil {
				ps.logger.Warnf("获取连接数失败: %v", result.err)
				connectedPeers = 0
			} else {
				connectedPeers = result.count
			}
		case <-time.After(2 * time.Second):
			ps.logger.Warnf("获取连接数超时，使用默认值0")
			connectedPeers = 0
		}
	}
	
	// 第二步：获取蓝牙信息（无锁调用，避免死锁）
	bluetoothEnabled := false
	var bluetoothPeers []map[string]interface{}
	
	// 安全地获取蓝牙状态（不持有PineconeService的锁）
	if bluetoothServiceRef != nil {
		bluetoothEnabled = bluetoothServiceRef.IsStarted()
		
		// 如果蓝牙启用，获取设备列表（带超时保护）
		if bluetoothEnabled {
			// 使用带超时的context来获取蓝牙peers信息，避免goroutine泄露
			type bluetoothResult struct {
				peers []interface{}
				err   error
			}
			
			bluetoothCtx, bluetoothCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer bluetoothCancel()
			
			bluetoothResultChan := make(chan bluetoothResult, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						select {
						case bluetoothResultChan <- bluetoothResult{nil, fmt.Errorf("bluetooth GetDiscoveredPeers panic: %v", r)}:
						case <-bluetoothCtx.Done():
							// Context已取消，不发送结果
						}
					}
				}()
				
				peers := bluetoothServiceRef.GetDiscoveredPeers()
				
				select {
				case bluetoothResultChan <- bluetoothResult{peers, nil}:
				case <-bluetoothCtx.Done():
					// Context已取消，不发送结果
				}
			}()
			
			// 等待结果或超时
			select {
			case result := <-bluetoothResultChan:
				if result.err != nil {
					ps.logger.Warnf("获取蓝牙peers失败: %v", result.err)
					bluetoothPeers = []map[string]interface{}{}
				} else {
					bluetoothPeers = make([]map[string]interface{}, 0, len(result.peers))
					for _, peerInterface := range result.peers {
						if peer, ok := peerInterface.(*bluetooth.BluetoothPeer); ok {
							peerMap := map[string]interface{}{
								"name":       peer.Name,
								"address":    peer.Address,
								"public_key": peer.PublicKey,
								"connected":  peer.Connected,
								"last_seen":  peer.LastSeen,
							}
							bluetoothPeers = append(bluetoothPeers, peerMap)
						}
					}
				}
			case <-bluetoothCtx.Done():
				ps.logger.Warnf("获取蓝牙peers超时，使用空列表")
				bluetoothPeers = []map[string]interface{}{}
			}
		} else {
			bluetoothPeers = []map[string]interface{}{}
		}
	} else {
		bluetoothPeers = []map[string]interface{}{}
	}
	
	// 获取监听地址信息
	ps.mu.RLock()
	listeningAddresses := make([]string, len(ps.listeningAddresses))
	copy(listeningAddresses, ps.listeningAddresses)
	ps.mu.RUnlock()

	return map[string]interface{}{
		"node_id":         nodeID,
		"listen_addr":     pineconeAddr,
		"listening_addresses": listeningAddresses, // 新增：所有监听地址
		"connected":       isRunning,
		"peer_count":      len(peers),
		"peers":           peers,
		"connected_peers": connectedPeers,
		"total_peers":     len(peers),
		"is_running":      isRunning,
		"service_name":    serviceName,
		"pinecone_addr":   pineconeAddr,
		"bluetooth_enabled": bluetoothEnabled,
		"bluetooth_peers": bluetoothPeers,
	}
}

// SendMessagePacket 发送消息包
// 直接发送到Pinecone网络，不做ACK确认检测
func (ps *PineconeService) SendMessagePacket(toAddr string, packet *MessagePacket) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	// 确保在函数结束时释放MessagePacket对象
	defer ReleaseMessagePacket(packet)
	
	// 确保消息包的From字段被设置为当前节点的公钥地址
	if packet.From == "" {
		packet.From = ps.GetPublicKeyHex()
	}
	
	// 添加详细的消息发送日志
	ps.logger.Debugf("[MessageSend] 开始发送消息包: ID=%s, Type=%s, From=%s, To=%s, Content=%s", packet.ID, packet.Type, packet.From, toAddr, packet.Content)
	
	if ps.router == nil {
		err := fmt.Errorf("Pinecone路由器未初始化")
		ps.logger.Errorf("[MessageSend] 路由器未初始化")
		if ps.errorHandler != nil {
			ps.errorHandler.HandleError(err, "SendMessagePacket", map[string]interface{}{
				"target_addr": toAddr,
				"packet_type": packet.Type,
			})
		}
		return err
	}
	
	// 将消息包序列化为JSON
	ps.logger.Debugf("[MessageSend] 序列化消息包: ID=%s", packet.ID)
	packetData, err := MarshalJSONPooled(packet)
	if err != nil {
		err = fmt.Errorf("序列化消息包失败: %v", err)
		ps.logger.Errorf("[MessageSend] 序列化失败: %v", err)
		if ps.errorHandler != nil {
			ps.errorHandler.HandleError(err, "SendMessagePacket", map[string]interface{}{
				"target_addr": toAddr,
				"packet_type": packet.Type,
			})
		}
		return err
	}
	ps.logger.Debugf("[MessageSend] 序列化成功，数据长度: %d 字节", len(packetData))
	
	// 解析目标地址的公钥
	var peerPublicKeyBytes []byte
	if len(toAddr) == 64 { // 十六进制编码的公钥
		peerPublicKeyBytes, err = hex.DecodeString(toAddr)
		if err != nil {
			err = fmt.Errorf("解析目标公钥失败: %v", err)
			if ps.errorHandler != nil {
				ps.errorHandler.HandleError(err, "SendMessagePacket", map[string]interface{}{
					"target_addr": toAddr,
					"packet_type": packet.Type,
				})
			}
			return err
		}
	} else {
		err = fmt.Errorf("无效的目标地址格式: %s", toAddr)
		if ps.errorHandler != nil {
			ps.errorHandler.HandleError(err, "SendMessagePacket", map[string]interface{}{
				"target_addr": toAddr,
				"packet_type": packet.Type,
			})
		}
		return err
	}
	
	// 创建目标地址
	if len(peerPublicKeyBytes) != 32 {
		err = fmt.Errorf("无效的公钥长度: %d，期望32字节", len(peerPublicKeyBytes))
		if ps.errorHandler != nil {
			ps.errorHandler.HandleError(err, "SendMessagePacket", map[string]interface{}{
				"target_addr": toAddr,
				"packet_type": packet.Type,
				"key_length": len(peerPublicKeyBytes),
			})
		}
		return err
	}
	
	// 使用 types.PublicKey 作为目标地址
	var targetAddr types.PublicKey
	copy(targetAddr[:], peerPublicKeyBytes)
	
	// 直接使用Pinecone路由器发送消息包，不做ACK确认
	// 完全信任Pinecone网络的路由和传输能力
	ps.logger.Debugf("[MessageSend] 通过Pinecone路由器发送: ID=%s, TargetAddr=%x", packet.ID, targetAddr)
	_, err = ps.router.WriteTo(packetData, targetAddr)
	if err != nil {
		// 即使WriteTo返回错误，也只记录警告，不阻止消息发送
		ps.logger.Warnf("[MessageSend] ⚠️ Pinecone路由器返回错误，但消息可能已进入网络: %v", err)
		// 不返回错误，让上层认为发送成功
		return nil
	}
	
	// 成功发送消息包
	// 使用Debug级别日志避免在测试时产生过多输出
	ps.logger.Debugf("[MessageSend] ✅ 消息已提交到Pinecone网络路由: %s (Type: %s, To: %s)", packet.ID, packet.Type, toAddr)
	return nil
}

// sendMessageWithAck 发送需要确认的消息
func (ps *PineconeService) sendMessageWithAck(targetAddr types.PublicKey, packetData []byte, toAddr string, packet *MessagePacket) error {
	const maxRetries = 5 // 增加重试次数
	const ackTimeout = 8 * time.Second // 减少ACK超时时间
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// 重试发送消息
			
			// 在重试前先检查网络连接状态
			connectedPeers := ps.GetConnectedPeers()
			// 当前连接的节点数量
			
			// 如果连接的节点数量少于2个，触发多种路由发现机制
			if len(connectedPeers) < 2 {
				ps.logger.Debugf("⚠️ 连接节点不足，触发路由重新发现")
				
				// 触发路由发现服务
				if ps.routeDiscoveryService != nil {
					ps.routeDiscoveryService.DiscoverRoute(toAddr)
				}
				
				// 触发mDNS发现
				if ps.mdnsService != nil {
					go func() {
				_, _ = ps.mdnsService.DiscoverPeers(5 * time.Second)
			}()
				}
				
				// 等待路由发现完成
				time.Sleep(3 * time.Second)
			} else {
				// 即使有连接，也触发路由发现以寻找更好的路径
				if ps.routeDiscoveryService != nil {
					ps.routeDiscoveryService.DiscoverRoute(toAddr)
				}
			}
			
			// 指数退避，但时间更短
			time.Sleep(time.Duration(attempt) * 1 * time.Second)
		}
		
		// 发送消息
		_, err := ps.router.WriteTo(packetData, targetAddr)
		if err != nil {
			ps.logger.Debugf("❌ 发送消息失败 (尝试 %d/%d): %v", attempt+1, maxRetries, err)
			// 记录路由失败
			if ps.routeDiscoveryService != nil {
				ps.routeDiscoveryService.(*RouteDiscoveryService).recordRouteFailure(toAddr, err)
			}
			continue
		}
		
		// 发送成功，记录调试日志
		ps.logger.Debugf("📤 消息发送成功 (尝试 %d/%d): %s", attempt+1, maxRetries, packet.ID)
		
		// 等待ACK确认
		if ps.waitForAck(packet.ID, ackTimeout) {
			// 消息成功发送并确认
			// 记录路由成功
			if ps.routeDiscoveryService != nil {
				ps.routeDiscoveryService.(*RouteDiscoveryService).recordRouteSuccess(toAddr)
			}
			return nil
		}
		
		ps.logger.Debugf("⏰ 消息 %s 未收到确认 (尝试 %d/%d)", packet.ID, attempt+1, maxRetries)
	}
	
	err := fmt.Errorf("消息发送失败：经过 %d 次重试后仍未收到确认", maxRetries)
	if ps.errorHandler != nil {
		ps.errorHandler.HandleError(err, "sendMessageWithAck", map[string]interface{}{
			"target_addr": toAddr,
			"packet_id": packet.ID,
			"packet_type": packet.Type,
		})
	}
	return err
}

// waitForAck 等待ACK确认
func (ps *PineconeService) waitForAck(messageID string, timeout time.Duration) bool {
	ackChan := make(chan bool, 1)
	
	// 创建临时的ACK处理器来等待确认
	ps.pendingAcks.Store(messageID, ackChan)
	
	// 等待确认或超时
	select {
	case ackReceived := <-ackChan:
		ps.pendingAcks.Delete(messageID)
		return ackReceived
	case <-time.After(timeout):
		ps.pendingAcks.Delete(messageID)
		return false
	}
}

// GetAllPeerAddrs 获取所有对等节点地址
func (ps *PineconeService) GetAllPeerAddrs() []string {
	ps.peersMutex.RLock()
	defer ps.peersMutex.RUnlock()
	
	addrs := make([]string, 0, len(ps.peers))
	for addr := range ps.peers {
		addrs = append(addrs, addr)
	}
	
	return addrs
}

// GetStartTime 获取启动时间
func (ps *PineconeService) GetStartTime() time.Time {
	return ps.startTime
}

// GetUsernameByPubKey 根据公钥获取用户名
func (ps *PineconeService) GetUsernameByPubKey(pubkey string) (string, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	for _, peer := range ps.peers {
		if peer.PublicKey == pubkey {
			return peer.Username, true
		}
	}
	
	return "", false
}

// GetPubKeyByUsername 根据用户名获取公钥
func (ps *PineconeService) GetPubKeyByUsername(username string) (string, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	for _, peer := range ps.peers {
		if peer.Username == username {
			return peer.PublicKey, true
		}
	}
	
	return "", false
}

// GetMessageChannel 获取消息通道
func (ps *PineconeService) GetMessageChannel() chan *Message {
	return ps.messageChannel
}

// OnFriendSearchResponse 设置好友搜索响应处理函数
func (ps *PineconeService) OnFriendSearchResponse(handler func(username string, pubkey string, addr string)) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	
	ps.friendSearchResponseHandler = handler
	// 好友搜索响应处理函数已设置
}

// FriendSearchHandlerAdapter 好友搜索请求处理器适配器
type FriendSearchHandlerAdapter struct {
	pineconeService *PineconeService
	logger         Logger
	initialized    bool
}

func (h *FriendSearchHandlerAdapter) HandleMessage(msg *Message) error {
	// 将Message转换为MessagePacket进行处理
	packet := &MessagePacket{
		Type:     msg.Type,
		Content:  msg.Content,
		Metadata: msg.Metadata,
	}
	h.pineconeService.handleFriendSearchRequest(packet, msg.From)
	return nil
}

func (h *FriendSearchHandlerAdapter) GetMessageType() string {
	return "friend_search_request"
}

func (h *FriendSearchHandlerAdapter) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FriendSearchHandlerAdapter) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == "friend_search_request"
}

func (h *FriendSearchHandlerAdapter) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FriendSearchHandlerAdapter) Cleanup() error {
	h.initialized = false
	return nil
}

// FriendSearchResponseHandlerAdapter 好友搜索响应处理器适配器
type FriendSearchResponseHandlerAdapter struct {
	pineconeService *PineconeService
	logger         Logger
	initialized    bool
}

func (h *FriendSearchResponseHandlerAdapter) HandleMessage(msg *Message) error {
	// 将Message转换为MessagePacket进行处理
	packet := &MessagePacket{
		Type:     msg.Type,
		Content:  msg.Content,
		Metadata: msg.Metadata,
	}
	
	// 调用原有的处理逻辑
	// 处理好友搜索响应
	
	// 从 Metadata 中提取响应数据
	respData, ok := packet.Metadata["friend_search_response"]
	if !ok {
		h.pineconeService.logger.Errorf("Metadata 中缺少 friend_search_response 字段")
		return fmt.Errorf("Metadata 中缺少 friend_search_response 字段")
	}
	
	var response FriendSearchResponse
	switch v := respData.(type) {
	case FriendSearchResponse:
		response = v
	case *FriendSearchResponse:
		response = *v
	case map[string]interface{}:
		// 从 map 转换为结构体
		if name, ok := v["name"].(string); ok {
			response.Name = name
		}
		if pubKey, ok := v["public_key"].(string); ok {
			response.PublicKey = pubKey
		}
		if pathData, ok := v["path"].([]interface{}); ok {
			for _, p := range pathData {
				if pathStr, ok := p.(string); ok {
					response.Path = append(response.Path, pathStr)
				}
			}
		}
	default:
		h.pineconeService.logger.Errorf("无法解析 friend_search_response 数据类型: %T", respData)
		return fmt.Errorf("无法解析 friend_search_response 数据类型: %T", respData)
	}
	
	// 调用响应处理函数
	h.pineconeService.mu.RLock()
	handler := h.pineconeService.friendSearchResponseHandler
	h.pineconeService.mu.RUnlock()
	
	if handler != nil {
		// 收到好友搜索响应
		handler(response.Name, response.PublicKey, msg.From)
	} else {
		h.pineconeService.logger.Warnf("收到好友搜索响应但没有设置处理函数")
	}
	
	return nil
}

func (h *FriendSearchResponseHandlerAdapter) GetMessageType() string {
	return "friend_search_response"
}

func (h *FriendSearchResponseHandlerAdapter) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FriendSearchResponseHandlerAdapter) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == "friend_search_response"
}

func (h *FriendSearchResponseHandlerAdapter) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FriendSearchResponseHandlerAdapter) Cleanup() error {
	h.initialized = false
	return nil
}

// registerMessageHandlers 注册消息处理器
func (ps *PineconeService) registerMessageHandlers() {
    // 检查消息分发器是否已初始化
    if ps.messageDispatcher == nil {
        ps.logger.Errorf("消息分发器未初始化，无法注册处理器")
        return
    }

    // 注册文本消息处理器
    textHandler := NewNewTextMessageHandler(ps, ps.logger)
    if err := ps.messageDispatcher.RegisterHandler(textHandler); err != nil {
        ps.logger.Errorf("注册文本消息处理器失败: %v", err)
    } else {
        ps.logger.Debugf("文本消息处理器注册成功")
    }

    // 注册系统消息处理器
    systemHandler := NewNewSystemMessageHandler(ps, ps.logger)
    if err := ps.messageDispatcher.RegisterHandler(systemHandler); err != nil {
        ps.logger.Errorf("注册系统消息处理器失败: %v", err)
    } else {
        ps.logger.Debugf("系统消息处理器注册成功")
    }

    // 注册用户信息交换处理器
    userInfoHandler := NewNewUserInfoExchangeHandler(ps, ps.logger)
    if err := ps.messageDispatcher.RegisterHandler(userInfoHandler); err != nil {
        ps.logger.Errorf("注册用户信息交换处理器失败: %v", err)
    } else {
        ps.logger.Debugf("用户信息交换处理器注册成功")
    }

	
	// 注册好友搜索请求处理器
	friendSearchHandler := &FriendSearchHandlerAdapter{
		pineconeService: ps,
		logger:         ps.logger,
	}
	if err := ps.messageDispatcher.RegisterHandler(friendSearchHandler); err != nil {
		ps.logger.Errorf("注册好友搜索请求处理器失败: %v", err)
	} else {
		ps.logger.Debugf("好友搜索请求处理器注册成功")
	}
	
	// 注册好友搜索响应处理器
	friendSearchResponseHandler := &FriendSearchResponseHandlerAdapter{
		pineconeService: ps,
		logger:         ps.logger,
	}
	if err := ps.messageDispatcher.RegisterHandler(friendSearchResponseHandler); err != nil {
		ps.logger.Errorf("注册好友搜索响应处理器失败: %v", err)
	} else {
		ps.logger.Debugf("好友搜索响应处理器注册成功")
	}
	
	// 注册确认消息处理器
	ackHandler := NewAckMessageHandler(ps, ps.logger)
	if err := ps.messageDispatcher.RegisterHandler(ackHandler); err != nil {
		ps.logger.Errorf("注册确认消息处理器失败: %v", err)
	}
	
	// 注册否定确认消息处理器
	// 开始创建否定确认消息处理器
	nackHandler := NewNackMessageHandler(ps, ps.logger)
	if nackHandler == nil {
		ps.logger.Errorf("创建否定确认消息处理器失败: 返回nil")
		return
	}
	// 否定确认消息处理器创建成功
	if err := ps.messageDispatcher.RegisterHandler(nackHandler); err != nil {
		ps.logger.Errorf("注册否定确认消息处理器失败: %v", err)
	}
	
	// 注意：文件传输处理器现在由FileTransferAdapter负责注册
	// 这里不再注册文件传输相关的处理器，避免重复注册
	
	// 消息处理器注册完成
}

// BroadcastFriendSearchRequest 广播好友搜索请求
func (ps *PineconeService) BroadcastFriendSearchRequest(request string) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	if ps.router == nil {
		return fmt.Errorf("Pinecone路由器未初始化")
	}
	
	// 获取当前节点的公钥
	myPubKey := hex.EncodeToString(ps.publicKey)
	
	// 创建好友搜索请求
	searchReq := FriendSearchRequest{
		Keyword: request,
		From:    myPubKey,
		Path:    []string{}, // 初始路径为空
	}
	
	// 获取所有连接的节点
	peers := ps.router.Peers()
	if len(peers) == 0 {
		// 没有连接的节点，无法广播搜索请求
		return nil
	}
	
	// 向所有连接的节点广播搜索请求
	successCount := 0
	for _, peer := range peers {
		// 跳过自己
		if peer.PublicKey == myPubKey {
			continue
		}
		
		// 创建消息包
		packet := MessagePacket{
			From: myPubKey,
			To:   peer.PublicKey,
			Type: MessageTypeFriendSearchRequest,
			Metadata: map[string]interface{}{
				"friend_search_request": map[string]interface{}{
					"keyword": searchReq.Keyword,
					"from":    searchReq.From,
					"path":    searchReq.Path,
				},
			},
		}
		
		// 发送消息包
		if err := ps.SendMessagePacket(peer.PublicKey, &packet); err != nil {
			ps.logger.Errorf("向节点 %s 发送搜索请求失败: %v", peer.PublicKey, err)
		} else {
			successCount++
			// 向节点发送搜索请求成功
		}
	}
	
	// 广播好友搜索请求完成
	return nil
}

// GetStats 获取统计信息
func (ps *PineconeService) GetStats() map[string]interface{} {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	// 从Pinecone路由器获取真实的连接数，使用超时保护防止死锁
	connectedPeers := 0
	if ps.router != nil {
		peerCountChan := make(chan int, 1)
		go func() {
			peerCountChan <- ps.router.TotalPeerCount()
		}()
		
		select {
		case count := <-peerCountChan:
			connectedPeers = count
		case <-time.After(2 * time.Second):
			// GetStats: TotalPeerCount() timeout
			connectedPeers = 0
		}
	}
	
	return map[string]interface{}{
		"connected_peers": connectedPeers,
		"total_peers":     len(ps.peers),
		"is_running":      ps.isRunning,
		"service_name":    ps.name,
	}
}

// IsConnected 检查是否已连接
func (ps *PineconeService) IsConnected() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	
	// 检查Pinecone路由器是否有连接的对等节点
	if ps.router != nil {
		return ps.router.TotalPeerCount() > 0
	}
	return false
}

// AddPeer 添加对等节点
func (ps *PineconeService) AddPeer(name string, addr string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	
	peerInfo := &PeerInfo{
		Address:  addr,
		Username: name,
	}
	ps.peers[addr] = peerInfo
	// Added peer
	return nil
}

// findAvailablePort 查找可用端口
func (ps *PineconeService) findAvailablePort(startPort int) (string, error) {
	for port := startPort; port <= startPort+100; port++ {
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			// 找到可用端口
			return addr, nil
		}
	}
	return "", fmt.Errorf("在端口范围 %d-%d 内未找到可用端口", startPort, startPort+100)
}

// getAllNetworkInterfaces 获取所有可用的网络接口
func (ps *PineconeService) getAllNetworkInterfaces() ([]net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %v", err)
	}

	var validInterfaces []net.Interface
	for _, iface := range interfaces {
		// 跳过回环接口和未启用的接口
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// 检查接口是否有有效的IP地址
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		hasValidIP := false
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil { // IPv4
					hasValidIP = true
					break
				}
			}
		}

		if hasValidIP {
			validInterfaces = append(validInterfaces, iface)
		}
	}

	return validInterfaces, nil
}

// getInterfaceIPs 获取指定接口的所有IPv4地址
func (ps *PineconeService) getInterfaceIPs(iface net.Interface) ([]string, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil { // IPv4
				ips = append(ips, ipnet.IP.String())
			}
		}
	}

	return ips, nil
}

// startMultiInterfaceListeners 在所有可用网络接口上启动监听器
func (ps *PineconeService) startMultiInterfaceListeners(defaultPort string) error {
	// 获取所有可用的网络接口
	interfaces, err := ps.getAllNetworkInterfaces()
	if err != nil {
		ps.logger.Warnf("获取网络接口失败，使用默认监听: %v", err)
		// 回退到默认监听
		return ps.startPineconeListenerSync(":" + defaultPort)
	}

	if len(interfaces) == 0 {
		ps.logger.Warnf("未找到可用的网络接口，使用默认监听")
		// 回退到默认监听
		return ps.startPineconeListenerSync(":" + defaultPort)
	}

	// 初始化监听器和地址列表
	ps.listeners = make([]net.Listener, 0)
	ps.listeningAddresses = make([]string, 0)

	var successCount int
	var lastError error

	// 为每个网络接口启动监听器
	for _, iface := range interfaces {
		ips, err := ps.getInterfaceIPs(iface)
		if err != nil {
			ps.logger.Warnf("获取接口 %s 的IP地址失败: %v", iface.Name, err)
			continue
		}

		for _, ip := range ips {
			// 尝试在当前IP和端口上启动监听器
			listenAddr := fmt.Sprintf("%s:%s", ip, defaultPort)
			listener, err := ps.startSingleListener(listenAddr)
			if err != nil {
				// 如果端口被占用，尝试查找可用端口
				if strings.Contains(err.Error(), "bind") || strings.Contains(err.Error(), "address already in use") {
					port, _ := strconv.Atoi(defaultPort)
					availablePort, findErr := ps.findAvailablePortForIP(ip, port)
					if findErr != nil {
						ps.logger.Warnf("在接口 %s (%s) 上查找可用端口失败: %v", iface.Name, ip, findErr)
						lastError = err
						continue
					}
					listenAddr = fmt.Sprintf("%s%s", ip, availablePort)
					listener, err = ps.startSingleListener(listenAddr)
				}
				if err != nil {
					ps.logger.Warnf("在接口 %s (%s) 上启动监听器失败: %v", iface.Name, ip, err)
					lastError = err
					continue
				}
			}

			ps.listeners = append(ps.listeners, listener)
			ps.listeningAddresses = append(ps.listeningAddresses, listenAddr)
			successCount++
			ps.logger.Infof("Pinecone监听器已在接口 %s (%s) 上启动", iface.Name, listenAddr)
		}
	}

	// 检查是否至少有一个监听器启动成功
	if successCount == 0 {
		ps.logger.Warnf("所有接口监听器启动失败，尝试默认监听")
		// 回退到默认监听
		err := ps.startPineconeListenerSync(":" + defaultPort)
		if err != nil && lastError != nil {
			return fmt.Errorf("默认监听也失败: %v, 最后错误: %v", err, lastError)
		}
		return err
	}

	ps.logger.Infof("Pinecone服务已在 %d 个网络接口上启动监听器", successCount)
	return nil
}

// findAvailablePortForIP 为指定IP查找可用端口
func (ps *PineconeService) findAvailablePortForIP(ip string, startPort int) (string, error) {
	for port := startPort; port <= startPort+100; port++ {
		addr := fmt.Sprintf("%s:%d", ip, port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			return fmt.Sprintf(":%d", port), nil
		}
	}
	return "", fmt.Errorf("在IP %s 上未找到可用端口（范围 %d-%d）", ip, startPort, startPort+100)
}

// startSingleListener 启动单个监听器
func (ps *PineconeService) startSingleListener(listenAddr string) (net.Listener, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}

	// 启动接受连接的goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// 检查是否是因为监听器关闭导致的错误
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				ps.logger.Errorf("接受连接失败 (%s): %v", listenAddr, err)
				continue
			}
			go ps.handleIncomingPineconeConnection(conn)
		}
	}()

	return listener, nil
}

// startPineconeListener 启动Pinecone网络监听器
func (ps *PineconeService) startPineconeListener(listenAddr string) {
	// 尝试监听指定地址
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// 如果端口被占用，尝试自动查找可用端口
		// 端口被占用，正在查找可用端口
		
		// 解析原始端口号
		originalPort := 7777 // 默认端口
		if strings.Contains(listenAddr, ":") {
			portStr := strings.Split(listenAddr, ":")[1]
			if parsedPort, parseErr := strconv.Atoi(portStr); parseErr == nil {
				originalPort = parsedPort
			}
		}
		
		// 查找可用端口
		availableAddr, findErr := ps.findAvailablePort(originalPort)
		if findErr != nil {
			ps.logger.Errorf("无法找到可用端口: %v", findErr)
			return
		}
		
		// 使用找到的可用端口重新监听
		listener, err = net.Listen("tcp", availableAddr)
		if err != nil {
			ps.logger.Errorf("启动Pinecone监听器失败: %v", err)
			return
		}
		
		// 更新配置中的监听地址
		if ps.config != nil {
			ps.config.PineconeListen = availableAddr
			// 已更新配置中的监听地址
		}
		
		listenAddr = availableAddr
	}
	defer listener.Close()
	
	// Pinecone监听器已启动
	
	for {
		select {
		case <-ps.ctx.Done():
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				ps.logger.Errorf("接受连接失败: %v", err)
				continue
			}
			
			// 将连接交给Pinecone路由器处理
			go ps.handleIncomingPineconeConnection(conn)
		}
	}
}

// startPineconeListenerSync 同步版本的监听器启动方法，返回错误状态
func (ps *PineconeService) startPineconeListenerSync(listenAddr string) error {
	// 尝试监听指定地址
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// 如果端口被占用，尝试自动查找可用端口
		// 端口被占用，正在查找可用端口
		
		// 解析原始端口号
		originalPort := 7777 // 默认端口
		if strings.Contains(listenAddr, ":") {
			portStr := strings.Split(listenAddr, ":")[1]
			if parsedPort, parseErr := strconv.Atoi(portStr); parseErr == nil {
				originalPort = parsedPort
			}
		}
		
		// 查找可用端口
		availableAddr, findErr := ps.findAvailablePort(originalPort)
		if findErr != nil {
			return fmt.Errorf("无法找到可用端口: %v", findErr)
		}
		
		// 使用找到的可用端口重新监听
		listener, err = net.Listen("tcp", availableAddr)
		if err != nil {
			return fmt.Errorf("启动Pinecone监听器失败: %v", err)
		}
		
		// 更新配置中的监听地址
		if ps.config != nil {
			ps.config.PineconeListen = availableAddr
			// 已更新配置中的监听地址
		}
		
		listenAddr = availableAddr
	}
	
	// Pinecone监听器已启动
	
	// 启动接受连接的goroutine
	go func() {
		defer listener.Close()
		for {
			select {
			case <-ps.ctx.Done():
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					ps.logger.Errorf("接受连接失败: %v", err)
					continue
				}
				
				// 将连接交给Pinecone路由器处理
				go ps.handleIncomingPineconeConnection(conn)
			}
		}
	}()
	
	return nil
}

// handleIncomingPineconeConnection 处理入站Pinecone连接
func (ps *PineconeService) handleIncomingPineconeConnection(conn net.Conn) {
	// 使用Pinecone路由器建立连接，不要在这里关闭连接
	// 路由器会管理连接的生命周期
	portID, err := ps.router.Connect(
		conn,
		router.ConnectionZone("tcp"),
		router.ConnectionPeerType(router.PeerTypeRemote),
		router.ConnectionKeepalives(true),
	)
	if err != nil {
		ps.logger.Errorf("Pinecone路由器连接失败: %v", err)
		conn.Close() // 只有在失败时才关闭连接
		return
	}
	
	// 接受新的Pinecone连接
	
	// 使用带超时的context避免goroutine泄露
	ctx, cancel := context.WithTimeout(ps.ctx, 5*time.Second)
	go func() {
		defer cancel() // 确保context被取消，避免泄露
		
		select {
		case <-time.After(2 * time.Second): // 等待连接稳定
			// 获取连接的对等节点信息
			connectedPeers := ps.router.Peers()
			for _, peer := range connectedPeers {
				if peer.Port == int(portID) && peer.PublicKey != "" {
					// 添加到心跳服务监控，被动连接的节点设为中等优先级直连节点
					// Heartbeat monitoring removed - heartbeat mechanism disabled
					
					// 发送用户信息交换消息
					ps.sendUserInfoExchange(peer.PublicKey)
					break
				}
			}
		case <-ctx.Done():
			// 超时或服务关闭，直接返回
			return
		}
	}()
}

func (ps *PineconeService) startMessageReceiver() {
	// 使用动态工作池处理消息，无需手动管理worker生命周期
	
	for {
		select {
		case <-ps.ctx.Done():
			return
		default:
			// 使用动态缓冲区管理器获取缓冲区
			buffer := globalBufferManager.getBuffer()
			
			// 使用Pinecone路由器接收消息
			n, addr, err := ps.router.ReadFrom(buffer)
			if err != nil {
				// 归还缓冲区
				globalBufferManager.putBuffer(buffer)
				if ps.ctx.Err() != nil {
					return // 服务正在关闭
				}
				ps.logger.Errorf("接收消息失败: %v", err)
				time.Sleep(200 * time.Millisecond) // 增加等待时间，减少CPU使用
				continue
			}
			
			if n > 0 {
				// 记录消息大小，用于动态调整缓冲区
				globalBufferManager.recordMessageSize(n)
				
				// 严格限制消息大小，符合Pinecone网络32KB限制
				if n > 32*1024 { // 32KB消息大小限制
					ps.logger.Warnf("丢弃过大消息 (%d bytes) 来自 %s，超过Pinecone网络32KB限制", n, addr.String())
					globalBufferManager.putBuffer(buffer)
					// 增加消息丢弃计数
					atomic.AddInt64(&ps.messageDropCount, 1)
					continue
				}
				
				// 检查消息是否超过当前缓冲区大小，需要重新读取
				if n > len(buffer) {
					ps.logger.Warnf("消息大小 (%d bytes) 超过当前缓冲区大小 (%d bytes)，重新分配缓冲区", n, len(buffer))
					// 归还当前缓冲区
					globalBufferManager.putBuffer(buffer)
					// 分配足够大的缓冲区，但不超过32KB限制
					bufferSize := n
					if bufferSize > 32*1024 {
						bufferSize = 32*1024
					}
					newBuffer := make([]byte, bufferSize)
					// 重新读取消息
					n2, _, err2 := ps.router.ReadFrom(newBuffer)
					if err2 != nil {
						ps.logger.Errorf("重新读取消息失败: %v", err2)
						continue
					}
					// 如果重新读取的消息仍然超过32KB限制，丢弃
					if n2 > 32*1024 {
						ps.logger.Warnf("重新读取的消息仍然过大 (%d bytes)，丢弃", n2)
						atomic.AddInt64(&ps.messageDropCount, 1)
						continue
					}
					buffer = newBuffer
					n = n2
				}
				
				// 安全地复制实际接收到的数据，确保不会越界
				actualSize := n
				if actualSize > len(buffer) {
					actualSize = len(buffer)
					ps.logger.Warnf("截断消息：实际大小 %d，缓冲区大小 %d", n, len(buffer))
				}
				data := make([]byte, actualSize)
				copy(data, buffer[:actualSize])
				
				// 归还缓冲区
				globalBufferManager.putBuffer(buffer)
				
				addrStr := addr.String()
				// 使用动态工作池处理消息
				if !ps.workerPool.Submit(func() {
					ps.handleReceivedMessage(data, addrStr)
				}) {
					// 工作池队列已满，丢弃消息并记录警告
					ps.logger.Warnf("动态工作池队列已满，丢弃来自 %s 的消息", addrStr)
					// 增加消息丢弃计数
					atomic.AddInt64(&ps.messageDropCount, 1)
				} else {
					// 增加消息处理计数
					atomic.AddInt64(&ps.messageProcessedCount, 1)
				}
			} else {
				// 没有接收到数据，归还缓冲区
				globalBufferManager.putBuffer(buffer)
			}
		}
	}
}

// connectToPeer 连接到指定的对等节点
func (ps *PineconeService) connectToPeer(peerAddr string) {
	ps.logger.Debugf("尝试连接到对等节点: %s", peerAddr)
	
	// 解析目标地址
	tcpAddr := peerAddr
	if strings.HasPrefix(peerAddr, "pinecone://") {
		tcpAddr = strings.TrimPrefix(peerAddr, "pinecone://")
	}
	
	// 解析目标IP和端口
	host, portStr, err := net.SplitHostPort(tcpAddr)
	if err != nil {
		ps.logger.Errorf("解析地址失败: %v", err)
		return
	}
	targetPort, err := strconv.Atoi(portStr)
	if err != nil {
		ps.logger.Errorf("解析端口失败: %v", err)
		return
	}
	
	// 标准化主机地址
	if host == "localhost" {
		host = "127.0.0.1"
		tcpAddr = fmt.Sprintf("127.0.0.1:%d", targetPort)
	}
	
	// 检查是否已经连接到这个地址
	connectedPeers := ps.router.Peers()
	for _, peer := range connectedPeers {
		if peer.RemoteIP != "" && peer.RemotePort > 0 {
			// 比较IP和端口
			if peer.RemoteIP == host && peer.RemotePort == targetPort {
				ps.logger.Debugf("已连接到节点 %s:%d，跳过重复连接", host, targetPort)
				return
			}
		}
	}
	
	ps.logger.Infof("开始连接到新节点: %s", tcpAddr)
	
	// 创建TCP连接，强制使用IPv4
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}
	
	ps.logger.Debugf("正在建立TCP连接到: %s", tcpAddr)
	netConn, err := dialer.DialContext(ps.ctx, "tcp4", tcpAddr)
	if err != nil {
		ps.logger.Errorf("TCP连接到 %s 失败: %v", tcpAddr, err)
		return
	}
	
	ps.logger.Debugf("TCP连接成功，正在建立Pinecone路由器连接")
	// 使用Pinecone路由器建立连接
	_, err = ps.router.Connect(
		netConn,
		router.ConnectionZone("tcp"),
		router.ConnectionPeerType(router.PeerTypeRemote),
		router.ConnectionKeepalives(true),
	)
	if err != nil {
		ps.logger.Errorf("Pinecone路由器连接失败: %v", err)
		netConn.Close()
		return
	}
	
	ps.logger.Infof("成功连接到对等节点: %s", tcpAddr)
	
	// 延迟一段时间后检查连接是否建立，然后添加到心跳监控
	go func() {
		time.Sleep(2 * time.Second) // 等待连接稳定
		
		// 获取连接的对等节点信息
		connectedPeers := ps.router.Peers()
		ps.logger.Debugf("连接建立后检查，当前连接的节点数: %d", len(connectedPeers))
		
		for _, peer := range connectedPeers {
			if peer.Port > 0 && peer.PublicKey != "" {
				// 检查是否是刚建立的连接
				if peer.RemoteIP == host && peer.RemotePort == targetPort {
					ps.logger.Infof("确认连接建立: %s:%d (公钥: %s...)", peer.RemoteIP, peer.RemotePort, peer.PublicKey[:8])
					
					// 发送用户信息交换消息
					ps.sendUserInfoExchange(peer.PublicKey)
					break
				}
			}
		}
	}()
}





// handleReceivedMessage 处理接收到的消息
func (ps *PineconeService) handleReceivedMessage(data []byte, fromAddr string) {
	// 背压控制已移除
	
	// 添加详细的消息接收日志
	previewLen := 200
	if len(data) < previewLen {
		previewLen = len(data)
	}
	ps.logger.Debugf("[MessageReceive] 从 %s 接收到 %d 字节数据: %s", fromAddr, len(data), string(data[:previewLen]))
	
	// 首先尝试解析为 MessagePacket
	var packet MessagePacket
	if err := UnmarshalJSONPooled(data, &packet); err == nil {
		ps.logger.Debugf("[MessageReceive] 成功解析为MessagePacket: ID=%s, Type=%s, From=%s, Content=%s", packet.ID, packet.Type, packet.From, packet.Content)
		
		// 添加消息类型和内容的一致性验证
		if err := ps.validateMessageConsistency(&packet); err != nil {
			ps.logger.Warnf("[MessageReceive] 消息一致性验证失败: %v, 消息ID=%s", err, packet.ID)
			// 对于不一致的消息，记录但不处理
			return
		}
		
		// 处理 MessagePacket
		ps.handleMessagePacket(&packet, fromAddr)
		
		// 归还MessagePacket对象到池中
		ReleaseMessagePacket(&packet)
		return
	} else {
		ps.logger.Debugf("[MessageReceive] 解析MessagePacket失败: %v", err)
	}
	
	// 尝试解析JSON消息
	var message Message
	if err := UnmarshalJSONPooled(data, &message); err != nil {
		ps.logger.Debugf("[MessageReceive] 解析Message失败: %v", err)
		// 记录消息解析失败
		if ps.errorHandler != nil {
			previewLen := 100
			if len(data) < previewLen {
				previewLen = len(data)
			}
			ps.errorHandler.HandleError(err, "handleReceivedMessage", map[string]interface{}{
				"from_addr": fromAddr,
				"data_size": len(data),
				"data_preview": string(data[:previewLen]),
			})
		}
		// 如果不是JSON格式，创建一个简单的文本消息
		message = Message{
			From:      fromAddr,
			To:        ps.GetPineconeAddr(),
			Content:   string(data),
			Timestamp: time.Now(),
			Type:      "text",
		}
		ps.logger.Debugf("[MessageReceive] 创建简单文本消息: Type=%s, Content=%s", message.Type, message.Content)
	} else {
		ps.logger.Debugf("[MessageReceive] 成功解析为Message: ID=%s, Type=%s, From=%s, Content=%s", message.ID, message.Type, message.From, message.Content)
		
		// 添加Message类型和内容的一致性验证
		if err := ps.validateMessageConsistencyForMessage(&message); err != nil {
			ps.logger.Warnf("[MessageReceive] Message一致性验证失败: %v, 消息ID=%s", err, message.ID)
			// 对于不一致的消息，记录但不处理
			return
		}
		
		// 更新消息的接收信息
		message.To = ps.GetPineconeAddr()
		message.Timestamp = time.Now()
	}
	
	// 调用消息分发器
	if ps.messageDispatcher != nil {
		ps.logger.Debugf("[MessageReceive] 分发消息到调度器: Type=%s, Content=%s", message.Type, message.Content)
		if err := ps.messageDispatcher.DispatchMessage(&message); err != nil {
			ps.logger.Errorf("消息分发失败: %v", err)
			atomic.AddInt64(&ps.messageDropCount, 1)
		} else {
			ps.logger.Debugf("[MessageReceive] 消息分发成功")
			atomic.AddInt64(&ps.messageProcessedCount, 1)
		}
	} else {
		ps.logger.Warnf("消息分发器未初始化，丢弃消息")
		atomic.AddInt64(&ps.messageDropCount, 1)
	}
}

// TextMessageHandler 文本消息处理器
type TextMessageHandler struct {
	pineconeService *PineconeService
	logger         Logger
	initialized    bool
}

func (h *TextMessageHandler) HandleMessage(msg *Message) error {
	// 添加调试日志
	h.logger.Debugf("[TextMessageHandler] 开始处理消息: Type=%s, From=%s, Content=%s", msg.Type, msg.From, msg.Content)
	
	// 处理接收到的文本消息
	// 检查是否为系统内部消息类型，如果是则不显示详细内容
	if msg.Type == "user_info_exchange" {
		// 用户信息交换消息不需要显示详细内容，已由UserInfoExchangeHandler处理
		h.logger.Debugf("[TextMessageHandler] 跳过用户信息交换消息")
		return nil
	}
	
	// 统一消息输出格式：发送者 | 内容 | 时间
	var senderDisplay string
	var pubKeyForLookup string
	
	// 添加详细的调试日志
	h.logger.Debugf("[TextMessageHandler] 原始From字段: '%s'", msg.From)
	
	// 从msg.From中提取公钥用于查找
	if strings.HasPrefix(msg.From, "pinecone://") {
		// 如果是pinecone://格式，尝试提取公钥部分
		addr := strings.TrimPrefix(msg.From, "pinecone://")
		h.logger.Debugf("[TextMessageHandler] 去除前缀后的地址: '%s'", addr)
		if strings.Contains(addr, "@") {
			// 格式: pubkey@host:port
			parts := strings.Split(addr, "@")
			if len(parts) > 0 {
				pubKeyForLookup = parts[0]
				h.logger.Debugf("[TextMessageHandler] 提取的公钥: '%s'", pubKeyForLookup)
			}
		} else {
			// 可能是localhost:port格式，直接使用整个地址作为查找键
			pubKeyForLookup = addr
			h.logger.Debugf("[TextMessageHandler] 使用完整地址作为查找键: '%s'", pubKeyForLookup)
		}
	} else {
		// 直接使用msg.From作为公钥
		pubKeyForLookup = msg.From
		h.logger.Debugf("[TextMessageHandler] 直接使用From作为公钥: '%s'", pubKeyForLookup)
	}
	
	// 尝试获取发送者的用户名
	if pubKeyForLookup != "" {
		username, found := h.pineconeService.GetUsernameByPubKey(pubKeyForLookup)
		h.logger.Debugf("[TextMessageHandler] 用户名查找结果: username='%s', found=%v", username, found)
		if found {
			senderDisplay = username
		} else {
			// 如果没有找到账号名，显示完整的公钥
			senderDisplay = pubKeyForLookup
		}
	} else {
		// 无法提取公钥，显示原始地址
		if len(msg.From) > 0 {
			senderDisplay = msg.From
		} else {
			senderDisplay = "未知发送者"
		}
	}
	
	h.logger.Debugf("[TextMessageHandler] 最终发送者显示: '%s'", senderDisplay)
	
	// 格式化时间戳
	timeStr := msg.Timestamp.Format("15:04:05")
	
	// 统一的消息输出格式 - 接收到的消息
	fmt.Printf("\033[1;36m[%s] %s → 我: %s\033[0m\n", timeStr, senderDisplay, msg.Content)
	
	// 发送消息确认
	if msg.ID != "" {
		if err := h.pineconeService.SendAckMessage(msg.From, msg.ID, "delivered"); err != nil {
			h.logger.Errorf("发送消息确认失败: %v", err)
		} else {
			h.logger.Debugf("已向 %s 发送消息确认", msg.From)
		}
	}
	
	h.logger.Debugf("[TextMessageHandler] 消息处理完成")
	return nil
}

func (h *TextMessageHandler) GetMessageType() string {
	return MessageTypeText
}

func (h *TextMessageHandler) GetPriority() int {
	return 1
}

func (h *TextMessageHandler) CanHandle(msg *Message) bool {
	// 明确排除user_info_exchange消息，确保由专用处理器处理
	if msg.Type == "user_info_exchange" {
		return false
	}
	return msg.Type == MessageTypeText
}

func (h *TextMessageHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *TextMessageHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// SystemMessageHandler 系统消息处理器
type SystemMessageHandler struct {
	pineconeService *PineconeService
	logger         Logger
	initialized    bool
}

func (h *SystemMessageHandler) HandleMessage(msg *Message) error {
	// 收到系统消息
	return nil
}

func (h *SystemMessageHandler) GetMessageType() string {
	return MessageTypeSystem
}

func (h *SystemMessageHandler) GetPriority() int {
	return 10
}

func (h *SystemMessageHandler) CanHandle(msg *Message) bool {
	return msg.Type == MessageTypeSystem
}

func (h *SystemMessageHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *SystemMessageHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// HeartbeatMessageHandler removed - heartbeat mechanism disabled

// HeartbeatResponseMessageHandler removed - heartbeat mechanism disabled

// GetRouteDiscoveryService 获取路由发现服务实例
func (ps *PineconeService) GetRouteDiscoveryService() RouteDiscoveryServiceInterface {
	return ps.routeDiscoveryService
}

// GetMDNSService 获取MDNS服务实例
func (ps *PineconeService) GetMDNSService() MDNSServiceInterface {
	return ps.mdnsService
}

// GetRouteRedundancyService 获取路由冗余服务
func (ps *PineconeService) GetRouteRedundancyService() RouteRedundancyServiceInterface {
	return ps.routeRedundancyService
}

// GetMessageDropCount 获取消息丢弃计数
func (ps *PineconeService) GetMessageDropCount() int64 {
	return atomic.LoadInt64(&ps.messageDropCount)
}

// GetMessageProcessedCount 获取消息处理计数
func (ps *PineconeService) GetMessageProcessedCount() int64 {
	return atomic.LoadInt64(&ps.messageProcessedCount)
}

// GetMessageDropRate 获取消息丢弃率
func (ps *PineconeService) GetMessageDropRate() float64 {
	dropCount := atomic.LoadInt64(&ps.messageDropCount)
	processedCount := atomic.LoadInt64(&ps.messageProcessedCount)
	totalCount := dropCount + processedCount
	
	if totalCount == 0 {
		return 0.0
	}
	return float64(dropCount) / float64(totalCount)
}

// GetQueueUsageRate 获取队列使用率
func (ps *PineconeService) GetQueueUsageRate() float64 {
	if ps.messageChannel == nil {
		return 0.0
	}
	return float64(len(ps.messageChannel)) / float64(cap(ps.messageChannel))
}

// GetMetricsStats 获取完整的监控统计信息
func (ps *PineconeService) GetMetricsStats() map[string]interface{} {
	dropCount := atomic.LoadInt64(&ps.messageDropCount)
	processedCount := atomic.LoadInt64(&ps.messageProcessedCount)
	totalCount := dropCount + processedCount
	
	dropRate := 0.0
	if totalCount > 0 {
		dropRate = float64(dropCount) / float64(totalCount)
	}
	
	queueUsage := 0.0
	if ps.messageChannel != nil {
		queueUsage = float64(len(ps.messageChannel)) / float64(cap(ps.messageChannel))
	}
	
	uptime := time.Since(ps.metricsStartTime)
	
	stats := map[string]interface{}{
		"message_drop_count":     dropCount,
		"message_processed_count": processedCount,
		"message_total_count":    totalCount,
		"message_drop_rate":      dropRate,
		"queue_usage_rate":       queueUsage,
		"queue_current_size":     len(ps.messageChannel),
		"queue_capacity":         cap(ps.messageChannel),
		"uptime_seconds":         uptime.Seconds(),
		"metrics_start_time":     ps.metricsStartTime,
	}
	
	// 添加WorkerPool统计信息
	if ps.workerPool != nil {
		workerPoolStats := ps.workerPool.GetStats()
		stats["worker_pool"] = workerPoolStats
		stats["worker_pool_queue_usage"] = ps.GetWorkerPoolQueueUsage()
		stats["worker_pool_overloaded"] = ps.IsWorkerPoolOverloaded()
	}
	
	// 背压控制器统计信息已移除
	
	return stats
}

// ResetMetrics 重置监控指标
func (ps *PineconeService) ResetMetrics() {
	atomic.StoreInt64(&ps.messageDropCount, 0)
	atomic.StoreInt64(&ps.messageProcessedCount, 0)
	ps.metricsStartTime = time.Now()
}

// GetWorkerPoolStats 获取工作池统计信息
func (ps *PineconeService) GetWorkerPoolStats() map[string]interface{} {
	if ps.workerPool == nil {
		return map[string]interface{}{}
	}
	return ps.workerPool.GetStats()
}

// GetWorkerPoolQueueUsage 获取工作池队列使用率
func (ps *PineconeService) GetWorkerPoolQueueUsage() float64 {
	if ps.workerPool == nil {
		return 0.0
	}
	return float64(ps.workerPool.GetQueueLength()) / float64(ps.workerPool.GetQueueCapacity())
}

// IsWorkerPoolOverloaded 检查工作池是否过载
func (ps *PineconeService) IsWorkerPoolOverloaded() bool {
	if ps.workerPool == nil {
		return false
	}
	return ps.workerPool.IsOverloaded()
}

// isHighLoad 检查是否处于高负载状态
func (ps *PineconeService) isHighLoad() bool {
	if ps.messageChannel == nil {
		return false
	}
	
	// 检查消息通道使用率
	channelUsage := float64(len(ps.messageChannel)) / float64(cap(ps.messageChannel))
	
	// 检查优先级队列使用率
	priorityQueueUsage := 0.0
	if ps.priorityQueue != nil {
		priorityQueueUsage = float64(ps.priorityQueue.Len()) / float64(ps.priorityQueue.Cap())
	}
	
	// 如果任一队列超过阈值，则认为是高负载
	return channelUsage > ps.highLoadThreshold || priorityQueueUsage > ps.highLoadThreshold
}

// EnableGracefulDegradation 启用优雅降级
func (ps *PineconeService) EnableGracefulDegradation(enable bool) {
	ps.gracefulDegradation = enable
}

// SetHighLoadThreshold 设置高负载阈值
func (ps *PineconeService) SetHighLoadThreshold(threshold float64) {
	if threshold > 0 && threshold <= 1.0 {
		ps.highLoadThreshold = threshold
	}
}

// ProcessPriorityQueue 处理优先级队列中的消息 - 已废弃，消息现在直接通过分发器处理
// 保留此方法以维持接口兼容性
func (ps *PineconeService) ProcessPriorityQueue() {
	// 优先级队列处理已废弃，消息现在直接通过messageDispatcher处理
	return
}

// startPriorityQueueProcessor 启动优先级队列处理器 - 已废弃
func (ps *PineconeService) startPriorityQueueProcessor() {
	// 优先级队列处理器已废弃，消息现在直接通过messageDispatcher处理
	return
}

// TriggerRouteRediscovery 触发路由重新发现
func (ps *PineconeService) TriggerRouteRediscovery(targetAddr string) {
	// 使用Debug级别日志，减少重复输出
	// 触发路由重新发现
	
	// 使用路由发现服务
	if ps.routeDiscoveryService != nil {
		ps.routeDiscoveryService.DiscoverRoute(targetAddr)
	}
	
	// 使用mDNS重新发现节点
	if ps.mdnsService != nil {
		ps.mdnsService.StartDiscovery()
	}
	
	// 触发路由冗余服务更新
	if ps.routeRedundancyService != nil {
		ps.routeRedundancyService.UpdateRoutes()
	}
}

// SendPing 发送ping请求到指定地址
func (ps *PineconeService) SendPing(targetAddr string, timeout time.Duration) (*PingResponse, error) {
	if ps.pingManager == nil {
		return nil, fmt.Errorf("ping manager not initialized")
	}
	
	return ps.pingManager.SendPing(targetAddr, timeout)
}



// UserInfoExchangeHandler 用户信息交换处理器
type UserInfoExchangeHandler struct {
	pineconeService *PineconeService
	logger         Logger
	initialized    bool
}

func (h *UserInfoExchangeHandler) HandleMessage(msg *Message) error {
	// 处理用户信息交换消息
	
	// 解析用户信息交换消息
	var userInfo UserInfoExchange
	if err := UnmarshalJSONPooled([]byte(msg.Content), &userInfo); err != nil {
		h.logger.Errorf("解析用户信息交换消息失败: %v", err)
		return err
	}
	
	// 存储用户名到公钥的映射
	h.pineconeService.peersMutex.Lock()
	if h.pineconeService.peers == nil {
		h.pineconeService.peers = make(map[string]*PeerInfo)
	}
	
	// 检查是否为新的或更新的用户信息
	isNewOrUpdated := false
	if existingPeer, exists := h.pineconeService.peers[userInfo.PublicKey]; !exists {
		// 新的对等节点
		isNewOrUpdated = true
	} else if existingPeer.Username != userInfo.Username {
		// 用户名发生变化
		isNewOrUpdated = true
	}
	
	// 创建或更新PeerInfo
	peerInfo := &PeerInfo{
		Username:  userInfo.Username,
		PublicKey: userInfo.PublicKey,
		Address:   userInfo.PineconeAddr,
		LastSeen:  time.Now(),
		IsOnline:  true,
	}
	h.pineconeService.peers[userInfo.PublicKey] = peerInfo
	h.pineconeService.peersMutex.Unlock()
	
	// 只在信息真正更新时才输出日志
	if isNewOrUpdated {
		h.logger.Infof("🔄 用户信息更新: %s (%s) 已连接到网络", userInfo.Username, userInfo.PublicKey[:8]+"...")
		
		// 只有在发现新用户或用户信息更新时才发送响应，避免死循环
		// 这样可以防止两个节点互相发送用户信息交换消息形成死循环
		go h.sendUserInfoResponse(userInfo.PublicKey)
	}
	
	return nil
}

func (h *UserInfoExchangeHandler) sendUserInfoResponse(toPubKey string) {
	h.logger.Debugf("[UserInfoExchange] 开始发送用户信息交换响应到: %s", toPubKey)
	
	// 获取当前用户信息
	currentAccount := h.pineconeService.FriendList().GetCurrentAccount()
	if currentAccount == nil {
		h.logger.Errorf("[UserInfoExchange] 无法获取当前账户信息")
		return
	}
	
	// 创建用户信息交换消息
	userInfo := UserInfoExchange{
		Username:     currentAccount.Username,
		PublicKey:    hex.EncodeToString(h.pineconeService.publicKey),
		PineconeAddr: h.pineconeService.GetPineconeAddr(),
	}
	h.logger.Debugf("[UserInfoExchange] 创建用户信息: Username=%s, PublicKey=%s, PineconeAddr=%s", userInfo.Username, userInfo.PublicKey, userInfo.PineconeAddr)
	
	userInfoData, err := MarshalJSONPooled(userInfo)
	if err != nil {
		h.logger.Errorf("[UserInfoExchange] 序列化用户信息失败: %v", err)
		return
	}
	h.logger.Debugf("[UserInfoExchange] 序列化用户信息成功，数据长度: %d 字节", len(userInfoData))
	
	// 发送用户信息交换消息
	packet := MessagePacket{
		ID:        generateMessageID(),
		From:      h.pineconeService.GetPublicKeyHex(),
		To:        toPubKey,
		Type:      MessageTypeUserInfoExchange,
		Content:   string(userInfoData),
		Timestamp: time.Now(),
	}
	h.logger.Debugf("[UserInfoExchange] 创建消息包: ID=%s, Type=%s, Content=%s", packet.ID, packet.Type, packet.Content)
	
	if err := h.pineconeService.SendMessagePacket(toPubKey, &packet); err != nil {
		h.logger.Errorf("[UserInfoExchange] 发送用户信息交换响应失败: %v", err)
	} else {
		h.logger.Debugf("[UserInfoExchange] 用户信息交换响应发送成功")
	}
}

func (h *UserInfoExchangeHandler) GetMessageType() string {
	return MessageTypeUserInfoExchange
}

func (h *UserInfoExchangeHandler) GetPriority() int {
	return 100
}

func (h *UserInfoExchangeHandler) CanHandle(msg *Message) bool {
	return msg.Type == MessageTypeUserInfoExchange
}

func (h *UserInfoExchangeHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *UserInfoExchangeHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// sendUserInfoExchange 发送用户信息交换消息
func (ps *PineconeService) sendUserInfoExchange(peerAddr string) {
	// 获取当前用户信息
	currentAccount := ps.FriendList().GetCurrentAccount()
	if currentAccount == nil {
		ps.logger.Errorf("无法获取当前账户信息")
		return
	}
	
	// 创建用户信息交换消息
	userInfo := UserInfoExchange{
		Username:     currentAccount.Username,
		PublicKey:    hex.EncodeToString(ps.publicKey),
		PineconeAddr: ps.GetPineconeAddr(),
	}
	
	userInfoData, err := MarshalJSONPooled(userInfo)
	if err != nil {
		ps.logger.Errorf("序列化用户信息失败: %v", err)
		return
	}
	
	// 发送用户信息交换消息
	packet := MessagePacket{
		ID:        generateMessageID(),
		From:      ps.GetPublicKeyHex(),
		To:        peerAddr,
		Type:      MessageTypeUserInfoExchange,
		Content:   string(userInfoData),
		Timestamp: time.Now(),
	}
	
	if err := ps.SendMessagePacket(peerAddr, &packet); err != nil {
		ps.logger.Errorf("发送用户信息交换消息失败: %v", err)
	} else {
		// 已发送用户信息交换消息
	}
}

// NotifyAckReceived 通知ACK已收到
func (ps *PineconeService) NotifyAckReceived(messageID string, success bool) {
	if ackChanInterface, exists := ps.pendingAcks.Load(messageID); exists {
		if ackChan, ok := ackChanInterface.(chan bool); ok {
			select {
			case ackChan <- success:
			default:
				// 通道已满或已关闭，忽略
			}
		}
	}
}

// RegisterAckCallback 注册ACK回调
func (ps *PineconeService) RegisterAckCallback(msgID string, callback func(interface{})) error {
    // 若分发器未初始化，进行惰性初始化并注册处理器，避免早期调用失败
    if ps.messageDispatcher == nil {
        ps.messageDispatcher = NewMessageDispatcher(ps.logger)
        ps.registerMessageHandlers()
    }
    // 获取ACK处理器并注册回调
    if ps.messageDispatcher != nil {
        handlers := ps.messageDispatcher.GetHandlers()
        for _, handler := range handlers {
            if ackHandler, ok := handler.(*AckMessageHandler); ok {
                // 将通用回调转换为MessageAck回调
                ackHandler.RegisterAckCallback(msgID, func(ack MessageAck) {
                    callback(ack)
                })
                return nil
            }
        }
    }
    return fmt.Errorf("ACK处理器未找到")
}

// handleFriendSearchRequest 处理朋友搜索请求
func (ps *PineconeService) handleFriendSearchRequest(packet *MessagePacket, fromAddr string) {
	// Received friend search request
	// 这里可以添加具体的朋友搜索逻辑
	// 例如：检查本地朋友列表，发送响应等
}

// handleMessagePacket 处理消息包
func (ps *PineconeService) handleMessagePacket(packet *MessagePacket, fromAddr string) {
	// 添加调试日志跟踪消息类型
	ps.logger.Debugf("[handleMessagePacket] 接收到消息包: Type=%s, From=%s, Content=%s", packet.Type, packet.From, packet.Content)
	ps.logger.Debugf("[handleMessagePacket] 消息类型常量对比: MessageTypeText=%s, MessageTypeUserInfoExchange=%s, MessageTypeAck=%s", MessageTypeText, MessageTypeUserInfoExchange, MessageTypeAck)
	ps.logger.Debugf("[handleMessagePacket] 消息类型匹配检查: packet.Type=='%s', 是否为文本消息: %t, 是否为用户信息交换: %t, 是否为ACK: %t", packet.Type, packet.Type == MessageTypeText, packet.Type == MessageTypeUserInfoExchange, packet.Type == MessageTypeAck)
	
	// 根据消息类型分发到相应的处理器
	switch packet.Type {
	case "friend_search_request":
		ps.logger.Debugf("[handleMessagePacket] 处理朋友搜索请求")
		ps.handleFriendSearchRequest(packet, fromAddr)
	case MessageTypeCommand:
		ps.logger.Debugf("[handleMessagePacket] 处理命令类型消息包")
		// 处理命令类型消息包，如ping/trace命令
		ps.handleCommandPacket(packet, fromAddr)
	case "ping":
		ps.logger.Debugf("[handleMessagePacket] 处理ping消息包")
		// 处理ping类型消息包
		ps.handlePingPacket(packet, fromAddr)
	case "pong":
		ps.logger.Debugf("[handleMessagePacket] 处理pong消息包")
		// 处理pong类型消息包
		ps.handlePongPacket(packet, fromAddr)
	// 心跳检测机制已禁用
	case MessageTypeUserInfoExchange:
		ps.logger.Debugf("[handleMessagePacket] 处理用户信息交换消息包")
		// 处理用户信息交换消息包
		ps.handleUserInfoExchangePacket(packet, fromAddr)
	case MessageTypeText:
		ps.logger.Debugf("[handleMessagePacket] 处理文本消息包")
		// 处理文本消息包
		ps.handleTextMessagePacket(packet, fromAddr)
	case MessageTypeFile:
		ps.logger.Debugf("[handleMessagePacket] 处理文件消息包")
		// 处理文件消息包
		ps.handleFileMessagePacket(packet, fromAddr)
	// 文件传输相关消息类型
	case "file_data", MessageTypeFileRequest, MessageTypeFileChunk, MessageTypeFileAck, MessageTypeFileComplete, MessageTypeFileNack:
		ps.logger.Debugf("[handleMessagePacket] 处理文件传输消息包: %s", packet.Type)
		// 处理文件传输相关消息包
		ps.handleFileTransferPacket(packet, fromAddr)
	case MessageTypeAck:
		ps.logger.Debugf("[handleMessagePacket] 处理ACK消息包")
		// 将ACK消息包转换为Message并分发到消息分发器
		ackMessage := &Message{
			ID:        packet.ID,
			From:      packet.From,
			To:        packet.To,
			Type:      packet.Type,
			Content:   packet.Content,
			Timestamp: time.Now(),
			Metadata:  packet.Metadata, // 重要：传递Metadata，包含ack_data
		}
		ps.logger.Debugf("[handleMessagePacket] 转换ACK消息包为Message: ID=%s, Type=%s, Content=%s", ackMessage.ID, ackMessage.Type, ackMessage.Content)
		
		// 分发ACK消息到消息分发器
		if ps.messageDispatcher != nil {
			ps.logger.Debugf("[handleMessagePacket] 分发ACK消息到调度器")
			if err := ps.messageDispatcher.DispatchMessage(ackMessage); err != nil {
				ps.logger.Errorf("[handleMessagePacket] ACK消息分发失败: %v", err)
			} else {
				ps.logger.Debugf("[handleMessagePacket] ACK消息分发成功")
			}
		} else {
			ps.logger.Warnf("[handleMessagePacket] 消息分发器未初始化，无法处理ACK消息")
		}
	case MessageTypeNack:
		ps.logger.Debugf("[handleMessagePacket] 处理NACK消息包")
		// 将NACK消息包转换为Message并分发到消息分发器
		nackMessage := &Message{
			ID:        packet.ID,
			From:      packet.From,
			To:        packet.To,
			Type:      packet.Type,
			Content:   packet.Content,
			Timestamp: time.Now(),
			Metadata:  packet.Metadata, // 重要：传递Metadata，包含nack_data
		}
		ps.logger.Debugf("[handleMessagePacket] 转换NACK消息包为Message: ID=%s, Type=%s, Content=%s", nackMessage.ID, nackMessage.Type, nackMessage.Content)
		
		// 分发NACK消息到消息分发器
		if ps.messageDispatcher != nil {
			ps.logger.Debugf("[handleMessagePacket] 分发NACK消息到调度器")
			if err := ps.messageDispatcher.DispatchMessage(nackMessage); err != nil {
				ps.logger.Errorf("[handleMessagePacket] NACK消息分发失败: %v", err)
			} else {
				ps.logger.Debugf("[handleMessagePacket] NACK消息分发成功")
			}
		} else {
			ps.logger.Warnf("[handleMessagePacket] 消息分发器未初始化，无法处理NACK消息")
		}
	default:
		ps.logger.Warnf("[handleMessagePacket] 未知消息包类型: %s", packet.Type)
	}
}

// handleCommandPacket 处理命令类型消息包
func (ps *PineconeService) handleCommandPacket(packet *MessagePacket, fromAddr string) {
	// Processing command packet
	
	// 根据命令内容进行处理
	switch packet.Content {
	case "trace":
		// 处理ping/trace命令
		ps.handleTraceCommand(packet, fromAddr)
	default:
		// Unknown command
	}
}

// handleTraceCommand 处理trace命令
func (ps *PineconeService) handleTraceCommand(packet *MessagePacket, fromAddr string) {
	// Handling trace command
	
	// 创建trace响应
	response := MessagePacket{
		ID:        generateMessageID(),
		Type:      MessageTypeCommand,
		From:      ps.GetPineconeAddr(),
		To:        fromAddr,
		Content:   "trace_response",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"trace_id":    packet.Metadata["trace_id"],
			"hop_number":  1,
			"node_id":     hex.EncodeToString(ps.publicKey),
			"timestamp":   time.Now().UnixNano(),
			"latency_ms":  0, // 本地处理，延迟为0
		},
	}
	
	// 发送trace响应
	if err := ps.SendMessagePacket(fromAddr, &response); err != nil {
		ps.logger.Errorf("Failed to send trace response to %s: %v", fromAddr, err)
	} else {
		// Sent trace response
	}
}

// handlePingPacket 处理ping类型消息包
func (ps *PineconeService) handlePingPacket(packet *MessagePacket, fromAddr string) {
	// 减少日志输出频率，避免ping-pong消息过于频繁
	// Handling ping packet
	
	// 创建ping响应
	response := MessagePacket{
		ID:        generateMessageID(),
		Type:      "pong",
		From:      ps.GetPineconeAddr(),
		To:        fromAddr,
		Content:   "pong",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"ping_id":     packet.ID,
			"node_id":     hex.EncodeToString(ps.publicKey),
			"timestamp":   time.Now().UnixNano(),
			"latency_ms":  0, // 本地处理，延迟为0
		},
	}
	
	// 发送响应
	if err := ps.SendMessagePacket(fromAddr, &response); err != nil {
		ps.logger.Errorf("Failed to send ping response to %s: %v", fromAddr, err)
	} else {
		// 减少日志输出频率
		// Sent ping response
	}
}

// handlePongPacket 处理pong类型消息包
func (ps *PineconeService) handlePongPacket(packet *MessagePacket, fromAddr string) {
	// 提取ping ID
	pingID, ok := packet.Metadata["ping_id"].(string)
	if !ok {
		ps.logger.Warnf("收到pong响应但缺少ping_id: %s", fromAddr)
		return
	}
	
	// 使用PingManager处理pong响应
	if ps.pingManager != nil {
		ps.pingManager.HandlePongResponse(packet)
		ps.logger.Debugf("处理pong响应: ping_id=%s, from=%s", pingID, fromAddr)
	} else {
		ps.logger.Warnf("PingManager未初始化，无法处理pong响应")
	}
}

// generateMessageID 生成消息ID
// handleHeartbeatPacket 处理心跳消息包
// 心跳检测机制已禁用 - handleHeartbeatPacket 和 handleHeartbeatResponsePacket 函数已移除

// handleUserInfoExchangePacket 处理用户信息交换消息包
func (ps *PineconeService) handleUserInfoExchangePacket(packet *MessagePacket, fromAddr string) {
	// 将MessagePacket转换为Message并转发给消息调度器
	msg := &Message{
		ID:        packet.ID,
		Type:      packet.Type,
		From:      packet.From,
		To:        packet.To,
		Content:   packet.Content,
		Timestamp: packet.Timestamp,
		Metadata:  packet.Metadata,
	}
	
	if ps.messageDispatcher != nil {
		ps.messageDispatcher.DispatchMessage(msg)
	}
	
	// 清理MessagePacket对象的字段，为归还到对象池做准备
	packet.ID = ""
	packet.Type = ""
	packet.From = ""
	packet.To = ""
	packet.Content = ""
	packet.Data = nil
	packet.Timestamp = time.Time{}
	packet.Priority = 0
	packet.ReplyTo = ""
	for k := range packet.Metadata {
		delete(packet.Metadata, k)
	}
}

// handleTextMessagePacket 处理文本消息包
func (ps *PineconeService) handleTextMessagePacket(packet *MessagePacket, fromAddr string) {
	// 将MessagePacket转换为Message并转发给消息调度器
	msg := &Message{
		ID:        packet.ID,
		Type:      packet.Type,
		From:      packet.From,
		To:        packet.To,
		Content:   packet.Content,
		Timestamp: packet.Timestamp,
		Metadata:  packet.Metadata,
		// Message.Data 是 map[string]interface{}，不直接赋值 packet.Data ([]byte)
	}
	
	ps.logger.Debugf("📨 接收到文本消息包: ID=%s, Type=%s, From=%s, Content=%s", packet.ID, packet.Type, packet.From, packet.Content)
	ps.logger.Debugf("[handleTextMessagePacket] 转换前packet.Type=%s, 转换后msg.Type=%s", packet.Type, msg.Type)
	ps.logger.Debugf("[handleTextMessagePacket] 转换后的Message: Type=%s, From=%s", msg.Type, msg.From)
	
	if ps.messageDispatcher != nil {
		ps.logger.Debugf("[handleTextMessagePacket] 开始分发消息到消息调度器")
		if err := ps.messageDispatcher.DispatchMessage(msg); err != nil {
			ps.logger.Errorf("[handleTextMessagePacket] 消息分发失败: %v", err)
		} else {
			ps.logger.Debugf("[handleTextMessagePacket] 消息分发成功")
		}
	} else {
		ps.logger.Errorf("[handleTextMessagePacket] 消息调度器未初始化")
	}
	
	// 将消息放入消息通道
	select {
	case ps.messageChannel <- msg:
		ps.logger.Debugf("[handleTextMessagePacket] 消息已放入消息通道")
		atomic.AddInt64(&ps.messageProcessedCount, 1)
	default:
		ps.logger.Warnf("[handleTextMessagePacket] 消息通道已满，丢弃消息")
		atomic.AddInt64(&ps.messageDropCount, 1)
	}
	
	// 清理MessagePacket对象的字段，确保归还到对象池时完全清理
	packet.ID = ""
	packet.Type = ""
	packet.From = ""
	packet.To = ""
	packet.Content = ""
	packet.Timestamp = time.Time{}
	packet.Metadata = nil
	packet.Data = nil
}

// handleFileMessagePacket 处理文件消息包
func (ps *PineconeService) handleFileMessagePacket(packet *MessagePacket, fromAddr string) {
	// 将MessagePacket转换为Message并转发给消息调度器
	msg := &Message{
		ID:        packet.ID,
		Type:      packet.Type,
		From:      packet.From,
		To:        packet.To,
		Content:   packet.Content,
		Timestamp: packet.Timestamp,
		Metadata:  packet.Metadata,
		// Message.Data 是 map[string]interface{}，不直接赋值 packet.Data ([]byte)
	}
	
	ps.logger.Debugf("📁 接收到文件消息包: ID=%s, From=%s, Content=%s", packet.ID, packet.From, packet.Content)
	
	if ps.messageDispatcher != nil {
		ps.messageDispatcher.DispatchMessage(msg)
	}
	
	// 将消息放入消息通道
	select {
	case ps.messageChannel <- msg:
		atomic.AddInt64(&ps.messageProcessedCount, 1)
	default:
		// 消息通道已满，静默丢弃消息以避免阻塞
		atomic.AddInt64(&ps.messageDropCount, 1)
	}
	
	// 清理MessagePacket对象的字段，确保归还到对象池时完全清理
	packet.ID = ""
	packet.Type = ""
	packet.From = ""
	packet.To = ""
	packet.Content = ""
	packet.Timestamp = time.Time{}
	packet.Metadata = nil
	packet.Data = nil
}

// handleFileTransferPacket 处理文件传输相关的消息包
func (ps *PineconeService) handleFileTransferPacket(packet *MessagePacket, fromAddr string) {
	ps.logger.Debugf("处理文件传输消息包: %s -> %s, 类型: %s", fromAddr, packet.To, packet.Type)
	
	// 创建Message对象
	msg := &Message{
		ID:        packet.ID,
		Type:      packet.Type,
		From:      packet.From,
		To:        packet.To,
		Content:   packet.Content,
		Timestamp: packet.Timestamp,
		Metadata:  packet.Metadata,
		// Message.Data 是 map[string]interface{}，不直接赋值 packet.Data ([]byte)
	}
	
	// 增加消息处理计数
	atomic.AddInt64(&ps.messageProcessedCount, 1)
	
	// 将消息分发给消息调度器
	if ps.messageDispatcher != nil {
		ps.messageDispatcher.DispatchMessage(msg)
	}
	
	// 将消息放入消息通道
	select {
	case ps.messageChannel <- msg:
		// 消息成功放入通道
	default:
		// 消息通道已满，静默丢弃消息以避免阻塞
		atomic.AddInt64(&ps.messageDropCount, 1)
		ps.logger.Warnf("文件传输消息通道已满，丢弃消息: %s", packet.ID)
	}
	
	// 清理MessagePacket对象的字段，确保归还到对象池时完全清理
	packet.ID = ""
	packet.Type = ""
	packet.From = ""
	packet.To = ""
	packet.Content = ""
	packet.Timestamp = time.Time{}
	packet.Metadata = nil
	packet.Data = nil
}

// startPeerCleanup 启动定期清理过期节点的协程
func (ps *PineconeService) startPeerCleanup() {
	for {
		select {
		case <-ps.cleanupTicker.C:
			ps.cleanupExpiredPeers()
			// 检查连接数变化，如果减少则可能有节点断开
			ps.checkConnectionChanges()
		case <-ps.ctx.Done():
			return
		}
	}
}

// checkConnectionChanges 检查连接数变化并在连接减少时清理mDNS缓存
func (ps *PineconeService) checkConnectionChanges() {
	if ps.router == nil {
		return
	}
	
	currentConnections := len(ps.router.Peers())
	
	// 如果连接数减少，说明有节点断开
	if currentConnections < ps.lastConnectionCount {
		ps.logger.Debugf("检测到连接数减少: %d -> %d，清理mDNS缓存", ps.lastConnectionCount, currentConnections)
		
		// 清理mDNS缓存中的过期节点
		if ps.mdnsService != nil {
			ps.mdnsService.CleanupExpiredPeers()
		}
		
		// 触发路由重新发现
		if ps.routeDiscoveryService != nil {
			go func() {
				time.Sleep(2 * time.Second) // 等待一段时间再触发重新发现
				// 通过类型断言调用内部方法
				if rds, ok := ps.routeDiscoveryService.(*RouteDiscoveryService); ok {
					rds.triggerRouteDiscovery("")
				}
			}()
		}
	}
	
	ps.lastConnectionCount = currentConnections
}

// cleanupExpiredPeers 清理超过10分钟未活跃的节点
func (ps *PineconeService) cleanupExpiredPeers() {
	ps.peersMutex.Lock()
	defer ps.peersMutex.Unlock()
	
	now := time.Now()
	expiredThreshold := 10 * time.Minute
	cleanedCount := 0
	
	for pubkey, peer := range ps.peers {
		// 检查节点是否超过10分钟未活跃
		if now.Sub(peer.LastSeen) > expiredThreshold {
			// 检查节点是否仍然连接
			if ps.router != nil {
				connectedPeers := ps.router.Peers()
				isConnected := false
				for _, connectedPeer := range connectedPeers {
					if connectedPeer.PublicKey == pubkey {
						isConnected = true
						break
					}
				}
				
				// 如果节点未连接且超时，则删除
				if !isConnected {
					delete(ps.peers, pubkey)
					cleanedCount++
				}
			}
		}
	}
	
	if cleanedCount > 0 {
		// 清理了过期节点
	}
}

func generateMessageID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// NewCorrectTextMessageHandler 创建正确的文本消息处理器
func NewCorrectTextMessageHandler(ps *PineconeService, logger Logger) *TextMessageHandler {
	return &TextMessageHandler{
		pineconeService: ps,
		logger:         logger,
	}
}

// ImprovedTextMessageHandler 改进的文本消息处理器，包含正确的ACK发送逻辑
type ImprovedTextMessageHandler struct {
	pineconeService *PineconeService
	logger         Logger
	initialized    bool
}

func (h *ImprovedTextMessageHandler) HandleMessage(msg *Message) error {
	// 处理接收到的文本消息
	// 检查是否为系统内部消息类型，如果是则不显示详细内容
	if msg.Type == "user_info_exchange" {
		// 用户信息交换消息不需要显示详细内容，已由UserInfoExchangeHandler处理
		return nil
	}
	
	// 格式化时间戳
	timeStr := msg.Timestamp.Format("15:04:05")
	
	// 尝试获取发送者的用户名
	if username, found := h.pineconeService.GetUsernameByPubKey(msg.From); found {
		// 专业友好的消息显示格式
		fmt.Printf("\n\033[36m[%s]\033[0m \033[32m%s\033[0m: %s\n", timeStr, username, msg.Content)
	} else {
		// 使用公钥前8位作为标识
		fmt.Printf("\n\033[36m[%s]\033[0m \033[33m%s\033[0m: %s\n", timeStr, msg.From[:8]+"...", msg.Content)
	}
	
	// 发送消息确认
	if msg.ID != "" {
		h.logger.Debugf("[ImprovedTextHandler] 📤 准备发送ACK确认: To=%s, OriginalMsgID=%s", msg.From, msg.ID)
		if err := h.pineconeService.SendAckMessage(msg.From, msg.ID, "delivered"); err != nil {
			h.logger.Errorf("[ImprovedTextHandler] ❌ 发送消息确认失败: %v", err)
		} else {
			h.logger.Debugf("[ImprovedTextHandler] ✅ 已向 %s 发送消息确认 (MsgID: %s)", msg.From, msg.ID)
		}
	}
	
	return nil
}

func (h *ImprovedTextMessageHandler) GetMessageType() string {
	return MessageTypeText
}

func (h *ImprovedTextMessageHandler) GetPriority() int {
	return MessagePriorityNormal
}

func (h *ImprovedTextMessageHandler) CanHandle(msg *Message) bool {
	// 明确排除user_info_exchange消息，确保由专用处理器处理
	if msg != nil && msg.Type == "user_info_exchange" {
		return false
	}
	return msg != nil && msg.Type == MessageTypeText
}

func (h *ImprovedTextMessageHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *ImprovedTextMessageHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// NewTextMessageHandlerFromHandler 创建改进的TextMessageHandler实例
func NewTextMessageHandlerFromHandler(ps *PineconeService, logger Logger) MessageHandlerInterface {
	// 创建改进的TextMessageHandler实例
	return &ImprovedTextMessageHandler{
		pineconeService: ps,
		logger:         logger,
	}
}



// validateMessageConsistency 验证MessagePacket的类型和内容一致性
func (ps *PineconeService) validateMessageConsistency(packet *MessagePacket) error {
	if packet == nil {
		return fmt.Errorf("消息包为空")
	}
	
	// 验证用户信息交换消息
	if packet.Type == "user_info_exchange" {
		// 用户信息交换消息的Content应该是JSON格式的UserInfoExchange数据
		if packet.Content == "" {
			return fmt.Errorf("用户信息交换消息Content不能为空")
		}
		
		// 尝试解析Content为UserInfoExchange结构
		var userInfo map[string]interface{}
		if err := json.Unmarshal([]byte(packet.Content), &userInfo); err != nil {
			return fmt.Errorf("用户信息交换消息Content格式无效: %v", err)
		}
		
		// 检查必要字段
		if _, ok := userInfo["username"]; !ok {
			return fmt.Errorf("用户信息交换消息缺少username字段")
		}
		if _, ok := userInfo["public_key"]; !ok {
			return fmt.Errorf("用户信息交换消息缺少public_key字段")
		}
		
		if logger := getDebugLogger(); logger != nil {
			logger.Debugf("[MessageValidation] 用户信息交换消息验证通过: ID=%s", packet.ID)
		}
		return nil
	}
	
	// 验证文本消息
	if packet.Type == MessageTypeText {
		// 文本消息的Content不应该是JSON格式的用户信息
		if strings.Contains(packet.Content, "username") && strings.Contains(packet.Content, "public_key") {
			// 检查是否误将用户信息作为文本消息内容
			var userInfo map[string]interface{}
			if err := json.Unmarshal([]byte(packet.Content), &userInfo); err == nil {
				if _, hasUsername := userInfo["username"]; hasUsername {
					if _, hasPublicKey := userInfo["public_key"]; hasPublicKey {
						return fmt.Errorf("文本消息Content包含用户信息数据，可能存在消息类型混淆")
					}
				}
			}
		}
		
		if logger := getDebugLogger(); logger != nil {
			logger.Debugf("[MessageValidation] 文本消息验证通过: ID=%s, Content=%s", packet.ID, packet.Content)
		}
		return nil
	}
	
	// 其他消息类型暂时通过验证
	if logger := getDebugLogger(); logger != nil {
		logger.Debugf("[MessageValidation] 消息类型 %s 验证通过: ID=%s", packet.Type, packet.ID)
	}
	return nil
}

// validateMessageConsistencyForMessage 验证Message的类型和内容一致性
func (ps *PineconeService) validateMessageConsistencyForMessage(msg *Message) error {
	if msg == nil {
		return fmt.Errorf("消息为空")
	}
	
	// 验证用户信息交换消息
	if msg.Type == "user_info_exchange" {
		// 用户信息交换消息的Content应该是JSON格式的UserInfoExchange数据
		if msg.Content == "" {
			return fmt.Errorf("用户信息交换消息Content不能为空")
		}
		
		// 尝试解析Content为UserInfoExchange结构
		var userInfo map[string]interface{}
		if err := json.Unmarshal([]byte(msg.Content), &userInfo); err != nil {
			return fmt.Errorf("用户信息交换消息Content格式无效: %v", err)
		}
		
		// 检查必要字段
		if _, ok := userInfo["username"]; !ok {
			return fmt.Errorf("用户信息交换消息缺少username字段")
		}
		if _, ok := userInfo["public_key"]; !ok {
			return fmt.Errorf("用户信息交换消息缺少public_key字段")
		}
		
		if logger := getDebugLogger(); logger != nil {
			logger.Debugf("[MessageValidation] 用户信息交换Message验证通过: ID=%s", msg.ID)
		}
		return nil
	}
	
	// 验证文本消息
	if msg.Type == MessageTypeText {
		// 文本消息的Content不应该是JSON格式的用户信息
		if strings.Contains(msg.Content, "username") && strings.Contains(msg.Content, "public_key") {
			// 检查是否误将用户信息作为文本消息内容
			var userInfo map[string]interface{}
			if err := json.Unmarshal([]byte(msg.Content), &userInfo); err == nil {
				if _, hasUsername := userInfo["username"]; hasUsername {
					if _, hasPublicKey := userInfo["public_key"]; hasPublicKey {
						return fmt.Errorf("文本消息Content包含用户信息数据，可能存在消息类型混淆")
					}
				}
			}
		}
		
		if logger := getDebugLogger(); logger != nil {
			logger.Debugf("[MessageValidation] 文本Message验证通过: ID=%s, Content=%s", msg.ID, msg.Content)
		}
		return nil
	}
	
	// 其他消息类型暂时通过验证
	if logger := getDebugLogger(); logger != nil {
		logger.Debugf("[MessageValidation] Message类型 %s 验证通过: ID=%s", msg.Type, msg.ID)
	}
	return nil
}