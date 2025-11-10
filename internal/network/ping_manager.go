package network

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PingResponse 表示ping响应
type PingResponse struct {
	PingID    string        `json:"ping_id"`
	From      string        `json:"from"`
	To        string        `json:"to"`
	Latency   time.Duration `json:"latency"`
	Success   bool          `json:"success"`
	Timestamp time.Time     `json:"timestamp"`
	Error     string        `json:"error,omitempty"`
}

// PingRequest 表示ping请求
type PingRequest struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	Timeout   time.Duration `json:"timeout"`
	StartTime time.Time `json:"start_time"`
	ResponseChan chan *PingResponse `json:"-"`
}

// PingManager 管理ping请求和响应
type PingManager struct {
	mu            sync.RWMutex
	pendingPings  map[string]*PingRequest
	pineconeService PineconeServiceLike
	logger        Logger
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewPingManager 创建新的ping管理器
func NewPingManager(pineconeService PineconeServiceLike, logger Logger) *PingManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	pm := &PingManager{
		pendingPings:    make(map[string]*PingRequest),
		pineconeService: pineconeService,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
	}
	
	// 启动清理goroutine
	go pm.cleanupExpiredPings()
	
	return pm
}

// SendPing 发送ping请求并等待响应
func (pm *PingManager) SendPing(target string, timeout time.Duration) (*PingResponse, error) {
	// 生成ping ID
	pingID := fmt.Sprintf("ping_%d_%s", time.Now().UnixNano(), target[:min(8, len(target))])
	
	// 创建ping请求
	request := &PingRequest{
		ID:           pingID,
		Target:       target,
		Timeout:      timeout,
		StartTime:    time.Now(),
		ResponseChan: make(chan *PingResponse, 1),
	}
	
	// 注册pending ping
	pm.mu.Lock()
	pm.pendingPings[pingID] = request
	pm.mu.Unlock()
	
	// 发送ping消息
	pingPacket := MessagePacket{
		ID:        pingID,
		Type:      "ping",
		From:      pm.pineconeService.GetPineconeAddr(),
		To:        target,
		Content:   "ping",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"ping_id":    pingID,
			"start_time": request.StartTime.UnixNano(),
		},
	}
	
	pm.logger.Debugf("[PingManager] 发送ping到 %s, ID: %s", target, pingID)
	
	err := pm.pineconeService.SendMessagePacket(target, &pingPacket)
	if err != nil {
		// 清理pending ping
		pm.mu.Lock()
		delete(pm.pendingPings, pingID)
		pm.mu.Unlock()
		
		return &PingResponse{
			PingID:    pingID,
			From:      pm.pineconeService.GetPineconeAddr(),
			To:        target,
			Success:   false,
			Timestamp: time.Now(),
			Error:     fmt.Sprintf("发送ping失败: %v", err),
		}, err
	}
	
	// 等待响应或超时
	select {
	case response := <-request.ResponseChan:
		// 清理pending ping
		pm.mu.Lock()
		delete(pm.pendingPings, pingID)
		pm.mu.Unlock()
		
		pm.logger.Debugf("[PingManager] 收到ping响应 %s, 延迟: %v", pingID, response.Latency)
		return response, nil
		
	case <-time.After(timeout):
		// 超时，清理pending ping
		pm.mu.Lock()
		delete(pm.pendingPings, pingID)
		pm.mu.Unlock()
		
		pm.logger.Debugf("[PingManager] Ping超时 %s", pingID)
		return &PingResponse{
			PingID:    pingID,
			From:      pm.pineconeService.GetPineconeAddr(),
			To:        target,
			Success:   false,
			Timestamp: time.Now(),
			Error:     "ping超时",
		}, fmt.Errorf("ping超时")
		
	case <-pm.ctx.Done():
		// 服务关闭
		pm.mu.Lock()
		delete(pm.pendingPings, pingID)
		pm.mu.Unlock()
		
		return &PingResponse{
			PingID:    pingID,
			From:      pm.pineconeService.GetPineconeAddr(),
			To:        target,
			Success:   false,
			Timestamp: time.Now(),
			Error:     "服务已关闭",
		}, fmt.Errorf("服务已关闭")
	}
}

// HandlePongResponse 处理pong响应
func (pm *PingManager) HandlePongResponse(packet *MessagePacket) {
	pingID, ok := packet.Metadata["ping_id"].(string)
	if !ok {
		pm.logger.Warnf("[PingManager] Pong响应缺少ping_id")
		return
	}
	
	pm.mu.RLock()
	request, exists := pm.pendingPings[pingID]
	pm.mu.RUnlock()
	
	if !exists {
		pm.logger.Debugf("[PingManager] 收到未知ping_id的pong响应: %s", pingID)
		return
	}
	
	// 计算延迟
	latency := time.Since(request.StartTime)
	
	// 创建响应
	response := &PingResponse{
		PingID:    pingID,
		From:      packet.From,
		To:        packet.To,
		Latency:   latency,
		Success:   true,
		Timestamp: time.Now(),
	}
	
	// 发送响应到等待的goroutine
	select {
	case request.ResponseChan <- response:
		// 响应已发送
	default:
		// 通道已满或已关闭，忽略
		pm.logger.Warnf("[PingManager] 无法发送ping响应到通道: %s", pingID)
	}
}

// cleanupExpiredPings 清理过期的ping请求
func (pm *PingManager) cleanupExpiredPings() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.mu.Lock()
			now := time.Now()
			for pingID, request := range pm.pendingPings {
				if now.Sub(request.StartTime) > request.Timeout+10*time.Second {
					// 超时10秒后清理
					delete(pm.pendingPings, pingID)
					pm.logger.Debugf("[PingManager] 清理过期ping请求: %s", pingID)
				}
			}
			pm.mu.Unlock()
		}
	}
}

// GetPendingPingsCount 获取待处理ping数量
func (pm *PingManager) GetPendingPingsCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.pendingPings)
}



// Stop 停止ping管理器
func (pm *PingManager) Stop() {
	pm.cancel()
	
	// 关闭所有pending ping的响应通道
	pm.mu.Lock()
	for _, request := range pm.pendingPings {
		close(request.ResponseChan)
	}
	pm.pendingPings = make(map[string]*PingRequest)
	pm.mu.Unlock()
}