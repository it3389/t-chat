package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// ConnectionManager 连接管理器
// 实现 ConnectionManagerInterface 接口
type ConnectionManager struct {
	connections map[string]Connection
	listeners   map[string]Listener
	config      *NetworkConfig
	logger      Logger
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	
	// 清理队列
	cleanupQueue chan cleanupTask
	
	// 连接速率限制
	connectionLimiter chan struct{} // 限制并发连接数
	lastConnectTime   sync.Map       // 记录每个地址的最后连接时间
	connectInterval   time.Duration  // 连接间隔限制
}

type cleanupTask struct {
	taskType string // "connection" or "listener"
	id       string // peerID or address
}

// 确保 ConnectionManager 实现了 ConnectionManagerInterface 接口
var _ ConnectionManagerInterface = (*ConnectionManager)(nil)

// NewConnectionManager 创建连接管理器
func NewConnectionManager(config *NetworkConfig, logger Logger) *ConnectionManager {
	ctx, cancel := context.WithCancel(context.Background())
	cm := &ConnectionManager{
		connections:       make(map[string]Connection),
		listeners:         make(map[string]Listener),
		config:            config,
		logger:            logger,
		ctx:               ctx,
		cancel:            cancel,
		cleanupQueue:      make(chan cleanupTask, 50), // 减少缓冲从100到50个清理任务
		connectionLimiter: make(chan struct{}, 10),     // 限制最多10个并发连接
		connectInterval:   time.Second * 2,             // 2秒连接间隔限制
	}
	
	// 启动清理处理器
	go cm.cleanupProcessor()
	
	return cm
}

// CreateConnection 创建连接
func (cm *ConnectionManager) CreateConnection(peerID string, address string) (Connection, error) {
	if peerID == "" {
		return nil, fmt.Errorf("对等节点ID不能为空")
	}
	
	if address == "" {
		return nil, fmt.Errorf("地址不能为空")
	}
	
	// 检查连接间隔限制
	if lastTime, exists := cm.lastConnectTime.Load(address); exists {
		if time.Since(lastTime.(time.Time)) < cm.connectInterval {
			return nil, fmt.Errorf("连接到 %s 过于频繁，请等待 %v", address, cm.connectInterval)
		}
	}
	
	// 获取连接限制器令牌
	select {
	case cm.connectionLimiter <- struct{}{}:
		defer func() { <-cm.connectionLimiter }() // 确保释放令牌
	default:
		return nil, fmt.Errorf("连接数已达上限，无法连接到 %s", address)
	}
	
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	// 检查是否已存在连接
	if conn, exists := cm.connections[peerID]; exists {
		if !conn.IsClosed() {
			// 验证连接健康状态
			if cm.isConnectionHealthy(conn) {
				return conn, nil
			}
			// 连接不健康，关闭并重新创建
			conn.Close()
		}
		// 清理已关闭的连接
		delete(cm.connections, peerID)
	}
	
	// 记录连接时间
	cm.lastConnectTime.Store(address, time.Now())
	
	// 创建新连接，增加重试机制
	var conn Connection
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		
		dialer := &net.Dialer{
			Timeout: time.Duration(cm.config.ConnectionTimeout) * time.Second,
			KeepAlive: 30 * time.Second, // 启用TCP Keep-Alive
		}
		
		netConn, dialErr := dialer.DialContext(cm.ctx, "tcp", address)
		if dialErr != nil {
			err = dialErr
			continue
		}
		
		// 设置连接参数
		if tcpConn, ok := netConn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(30 * time.Second)
			tcpConn.SetNoDelay(true) // 禁用Nagle算法，减少延迟
		}
		
		conn = NewConnWrapper(netConn, peerID, cm.logger)
		err = nil
		break
	}
	
	if err != nil {
		return nil, fmt.Errorf("连接失败: %v", err)
	}
	
	cm.connections[peerID] = conn
	
	// 启动连接健康监控
	go cm.monitorConnection(conn, peerID)
	
	return conn, nil
}

// GetConnection 获取连接
func (cm *ConnectionManager) GetConnection(peerID string) (Connection, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	conn, exists := cm.connections[peerID]
	if !exists {
		return nil, false
	}
	
	// 检查连接是否有效
	if conn.IsClosed() {
		// 异步清理无效连接
		select {
		case cm.cleanupQueue <- cleanupTask{taskType: "connection", id: peerID}:
		default:
			// 队列满时直接清理
			delete(cm.connections, peerID)
		}
		return nil, false
	}
	
	return conn, true
}

// CloseConnection 关闭连接
func (cm *ConnectionManager) CloseConnection(peerID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	conn, exists := cm.connections[peerID]
	if !exists {
		return fmt.Errorf("连接不存在: %s", peerID)
	}
	
	err := conn.Close()
	delete(cm.connections, peerID)
	
	return err
}

// isConnectionHealthy 检查连接健康状态
func (cm *ConnectionManager) isConnectionHealthy(conn Connection) bool {
	if conn == nil || conn.IsClosed() {
		return false
	}
	
	// 尝试写入一个小的心跳包来测试连接
	deadline := time.Now().Add(5 * time.Second)
	conn.SetWriteDeadline(deadline)
	
	// 发送心跳包（空字节）
	_, err := conn.Write([]byte{})
	if err != nil {
		return false
	}
	
	return true
}

// monitorConnection 监控连接健康状态
func (cm *ConnectionManager) monitorConnection(conn Connection, peerID string) {
	ticker := time.NewTicker(30 * time.Second) // 每30秒检查一次
	defer ticker.Stop()
	
	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			if !cm.isConnectionHealthy(conn) {
				cm.logger.Printf("检测到不健康连接，清理: %s", peerID)
				cm.mu.Lock()
				if existingConn, exists := cm.connections[peerID]; exists && existingConn == conn {
					conn.Close()
					delete(cm.connections, peerID)
				}
				cm.mu.Unlock()
				return
			}
		}
	}
}

// ListConnections 列出所有连接
func (cm *ConnectionManager) ListConnections() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	var peerIDs []string
	for peerID, conn := range cm.connections {
		if !conn.IsClosed() {
			peerIDs = append(peerIDs, peerID)
		}
	}
	
	return peerIDs
}

// CreateListener 创建监听器
func (cm *ConnectionManager) CreateListener(address string) (Listener, error) {
	if address == "" {
		return nil, fmt.Errorf("监听地址不能为空")
	}
	
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	// 检查是否已存在监听器
	if listener, exists := cm.listeners[address]; exists {
		if !listener.IsClosed() {
			return listener, nil
		}
		// 清理已关闭的监听器
		delete(cm.listeners, address)
	}
	
	// 创建新监听器
	netListener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("创建监听器失败: %v", err)
	}
	
	listener := NewListenerWrapper(netListener, cm.logger)
	cm.listeners[address] = listener
	
	// 创建监听器
	
	return listener, nil
}

// GetListener 获取监听器
func (cm *ConnectionManager) GetListener(address string) (Listener, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	listener, exists := cm.listeners[address]
	if !exists {
		return nil, false
	}
	
	// 检查监听器是否有效
	if listener.IsClosed() {
		// 异步清理无效监听器
		select {
		case cm.cleanupQueue <- cleanupTask{taskType: "listener", id: address}:
		default:
			// 队列满时直接清理
			delete(cm.listeners, address)
		}
		return nil, false
	}
	
	return listener, true
}

// CloseListener 关闭监听器
func (cm *ConnectionManager) CloseListener(address string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	listener, exists := cm.listeners[address]
	if !exists {
		return fmt.Errorf("监听器不存在: %s", address)
	}
	
	err := listener.Close()
	delete(cm.listeners, address)
	

	
	return err
}

// GetConnectionCount 获取连接数量
func (cm *ConnectionManager) GetConnectionCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	count := 0
	for _, conn := range cm.connections {
		if !conn.IsClosed() {
			count++
		}
	}
	
	return count
}

// cleanupProcessor 清理处理器
func (cm *ConnectionManager) cleanupProcessor() {
	for {
		select {
		case <-cm.ctx.Done():
			return
		case task := <-cm.cleanupQueue:
			cm.mu.Lock()
			switch task.taskType {
			case "connection":
				delete(cm.connections, task.id)
			case "listener":
				delete(cm.listeners, task.id)
			}
			cm.mu.Unlock()
		}
	}
}

// Cleanup 清理管理器
func (cm *ConnectionManager) Cleanup() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	// 取消上下文
	cm.cancel()
	
	// 关闭所有连接
	for peerID, conn := range cm.connections {
		if err := conn.Close(); err != nil {
			if cm.logger != nil {
				cm.logger.Errorf("关闭连接 %s 失败: %v", peerID, err)
			}
		}
	}
	
	// 关闭所有监听器
	for address, listener := range cm.listeners {
		if err := listener.Close(); err != nil {
			if cm.logger != nil {
				cm.logger.Errorf("关闭监听器 %s 失败: %v", address, err)
			}
		}
	}
	
	cm.connections = make(map[string]Connection)
	cm.listeners = make(map[string]Listener)
	
	if cm.logger != nil {
		// 连接管理器已清理
	}
	
	return nil
}

// ConnWrapper 连接包装器
type ConnWrapper struct {
	conn   net.Conn
	peerID string
	logger Logger
	closed bool
	mu     sync.RWMutex
}

// NewConnWrapper 创建连接包装器
func NewConnWrapper(conn net.Conn, peerID string, logger Logger) *ConnWrapper {
	return &ConnWrapper{
		conn:   conn,
		peerID: peerID,
		logger: logger,
	}
}

// Read 读取数据
func (cw *ConnWrapper) Read(b []byte) (n int, err error) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	
	if cw.closed {
		return 0, fmt.Errorf("连接已关闭")
	}
	
	return cw.conn.Read(b)
}

// Write 写入数据
func (cw *ConnWrapper) Write(b []byte) (n int, err error) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	
	if cw.closed {
		return 0, fmt.Errorf("连接已关闭")
	}
	
	return cw.conn.Write(b)
}

// Close 关闭连接
func (cw *ConnWrapper) Close() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	
	if cw.closed {
		return nil
	}
	
	cw.closed = true
	return cw.conn.Close()
}

// LocalAddr 获取本地地址
func (cw *ConnWrapper) LocalAddr() net.Addr {
	return cw.conn.LocalAddr()
}

// RemoteAddr 获取远程地址
func (cw *ConnWrapper) RemoteAddr() net.Addr {
	return cw.conn.RemoteAddr()
}

// SetDeadline 设置截止时间
func (cw *ConnWrapper) SetDeadline(t time.Time) error {
	return cw.conn.SetDeadline(t)
}

// SetReadDeadline 设置读取截止时间
func (cw *ConnWrapper) SetReadDeadline(t time.Time) error {
	return cw.conn.SetReadDeadline(t)
}

// SetWriteDeadline 设置写入截止时间
func (cw *ConnWrapper) SetWriteDeadline(t time.Time) error {
	return cw.conn.SetWriteDeadline(t)
}

// GetPeerID 获取对等节点ID
func (cw *ConnWrapper) GetPeerID() string {
	return cw.peerID
}

// IsClosed 检查是否已关闭
func (cw *ConnWrapper) IsClosed() bool {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.closed
}

// ListenerWrapper 监听器包装器
type ListenerWrapper struct {
	listener net.Listener
	logger   Logger
	closed   bool
	mu       sync.RWMutex
}

// NewListenerWrapper 创建监听器包装器
func NewListenerWrapper(listener net.Listener, logger Logger) *ListenerWrapper {
	return &ListenerWrapper{
		listener: listener,
		logger:   logger,
	}
}

// Accept 接受连接
func (lw *ListenerWrapper) Accept() (net.Conn, error) {
	lw.mu.RLock()
	defer lw.mu.RUnlock()
	
	if lw.closed {
		return nil, fmt.Errorf("监听器已关闭")
	}
	
	return lw.listener.Accept()
}

// Close 关闭监听器
func (lw *ListenerWrapper) Close() error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	
	if lw.closed {
		return nil
	}
	
	lw.closed = true
	return lw.listener.Close()
}

// Addr 获取监听地址
func (lw *ListenerWrapper) Addr() net.Addr {
	return lw.listener.Addr()
}

// IsClosed 检查是否已关闭
func (lw *ListenerWrapper) IsClosed() bool {
	lw.mu.RLock()
	defer lw.mu.RUnlock()
	return lw.closed
}