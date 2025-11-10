package network

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TransferStateInfo 传输状态信息
type TransferStateInfo struct {
	FileID       string        `json:"file_id"`
	State        TransferState `json:"state"`
	PreviousState TransferState `json:"previous_state"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	FromAddr     string        `json:"from_addr"`
	ToAddr       string        `json:"to_addr"`
	SenderID     string        `json:"sender_id"`
	ReceiverID   string        `json:"receiver_id"`
	FileName     string        `json:"file_name"`
	FileSize     int64         `json:"file_size"`
	Progress     float64       `json:"progress"`
	ErrorMessage string        `json:"error_message,omitempty"`
	RetryCount   int           `json:"retry_count"`
	LastRetryTime time.Time    `json:"last_retry_time,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// TransferStateChangeCallback 状态变更回调
type TransferStateChangeCallback func(fileID string, oldState, newState TransferState, info *TransferStateInfo)

// TransferStateManager 传输状态管理器
type TransferStateManager struct {
	mu              sync.RWMutex
	states          map[string]*TransferStateInfo
	stateCallbacks  []TransferStateChangeCallback
	logger          Logger
	cleanupInterval time.Duration
	stateRetention  time.Duration
	stopCh          chan struct{}
}

// NewTransferStateManager 创建传输状态管理器
func NewTransferStateManager(logger Logger) *TransferStateManager {
	return &TransferStateManager{
		states:          make(map[string]*TransferStateInfo),
		stateCallbacks:  make([]TransferStateChangeCallback, 0),
		logger:          logger,
		cleanupInterval: 5 * time.Minute,  // 每5分钟清理一次
		stateRetention:  24 * time.Hour,   // 保留24小时的状态记录
		stopCh:          make(chan struct{}),
	}
}

// Start 启动状态管理器
func (tsm *TransferStateManager) Start() {
	go tsm.cleanupLoop()
	tsm.logger.Infof("[TransferStateManager] 状态管理器已启动")
}

// StartCleanup 启动清理任务（带上下文）
func (tsm *TransferStateManager) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(tsm.cleanupInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				tsm.cleanup()
			case <-ctx.Done():
				return
			case <-tsm.stopCh:
				return
			}
		}
	}()
	tsm.logger.Infof("[TransferStateManager] 清理任务已启动")
}

// Stop 停止状态管理器
func (tsm *TransferStateManager) Stop() {
	close(tsm.stopCh)
	tsm.logger.Infof("[TransferStateManager] 状态管理器已停止")
}

// CreateTransfer 创建新的传输状态
func (tsm *TransferStateManager) CreateTransfer(fileID, fromAddr, toAddr, fileName string, fileSize int64) *TransferStateInfo {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	now := time.Now()
	info := &TransferStateInfo{
		FileID:        fileID,
		State:         StateIdle,
		PreviousState: StateIdle,
		CreatedAt:     now,
		UpdatedAt:     now,
		FromAddr:      fromAddr,
		ToAddr:        toAddr,
		FileName:      fileName,
		FileSize:      fileSize,
		Progress:      0.0,
		Metadata:      make(map[string]interface{}),
	}
	
	tsm.states[fileID] = info
	tsm.logger.Debugf("[TransferStateManager] 创建传输状态: FileID=%s, State=%s", fileID, info.State)
	
	return info
}

// SetTransferState 设置传输状态（别名方法）
func (tsm *TransferStateManager) SetTransferState(fileID string, newState TransferState) error {
	return tsm.SetState(fileID, newState)
}

// GetTransferState 获取传输状态信息
func (tsm *TransferStateManager) GetTransferState(fileID string) (*TransferStateInfo, bool) {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return nil, false
	}
	
	// 返回副本以避免并发修改
	copy := *info
	if info.Metadata != nil {
		copy.Metadata = make(map[string]interface{})
		for k, v := range info.Metadata {
			copy.Metadata[k] = v
		}
	}
	
	return &copy, true
}

// SetState 设置传输状态
func (tsm *TransferStateManager) SetState(fileID string, newState TransferState) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return fmt.Errorf("transfer not found: %s", fileID)
	}
	
	oldState := info.State
	if oldState == newState {
		return nil // 状态未变更
	}
	
	// 验证状态转换是否合法
	if !tsm.isValidStateTransition(oldState, newState) {
		return fmt.Errorf("invalid state transition from %s to %s for file %s", oldState, newState, fileID)
	}
	
	info.PreviousState = oldState
	info.State = newState
	info.UpdatedAt = time.Now()
	
	tsm.logger.Infof("[TransferStateManager] 状态变更: FileID=%s, %s -> %s", fileID, oldState, newState)
	
	// 异步调用状态变更回调
	go tsm.notifyStateChange(fileID, oldState, newState, info)
	
	return nil
}

// GetState 获取传输状态
func (tsm *TransferStateManager) GetState(fileID string) (TransferState, error) {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return StateIdle, fmt.Errorf("transfer not found: %s", fileID)
	}
	
	return info.State, nil
}

// GetTransferInfo 获取传输信息
func (tsm *TransferStateManager) GetTransferInfo(fileID string) (*TransferStateInfo, error) {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return nil, fmt.Errorf("transfer not found: %s", fileID)
	}
	
	// 返回副本以避免并发修改
	copy := *info
	if info.Metadata != nil {
		copy.Metadata = make(map[string]interface{})
		for k, v := range info.Metadata {
			copy.Metadata[k] = v
		}
	}
	
	return &copy, nil
}

// UpdateProgress 更新传输进度
func (tsm *TransferStateManager) UpdateProgress(fileID string, progress float64) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return fmt.Errorf("transfer not found: %s", fileID)
	}
	
	if progress < 0 {
		progress = 0
	} else if progress > 1.0 {
		progress = 1.0
	}
	
	info.Progress = progress
	info.UpdatedAt = time.Now()
	
	tsm.logger.Debugf("[TransferStateManager] 更新进度: FileID=%s, Progress=%.2f%%", fileID, progress*100)
	
	return nil
}

// SetTransferError 设置传输错误信息（别名方法）
func (tsm *TransferStateManager) SetTransferError(fileID string, errorMsg string) error {
	return tsm.SetError(fileID, errorMsg)
}

// SetError 设置错误信息
func (tsm *TransferStateManager) SetError(fileID string, errorMsg string) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return fmt.Errorf("transfer not found: %s", fileID)
	}
	
	info.ErrorMessage = errorMsg
	info.UpdatedAt = time.Now()
	
	tsm.logger.Errorf("[TransferStateManager] 设置错误: FileID=%s, Error=%s", fileID, errorMsg)
	
	return nil
}

// IncrementRetryCount 增加重试次数
func (tsm *TransferStateManager) IncrementRetryCount(fileID string) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return fmt.Errorf("transfer not found: %s", fileID)
	}
	
	info.RetryCount++
	info.LastRetryTime = time.Now()
	info.UpdatedAt = time.Now()
	
	return nil
}

// SetMetadata 设置元数据
func (tsm *TransferStateManager) SetMetadata(fileID string, key string, value interface{}) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return fmt.Errorf("transfer not found: %s", fileID)
	}
	
	if info.Metadata == nil {
		info.Metadata = make(map[string]interface{})
	}
	
	info.Metadata[key] = value
	info.UpdatedAt = time.Now()
	
	return nil
}

// GetMetadata 获取元数据
func (tsm *TransferStateManager) GetMetadata(fileID string, key string) (interface{}, error) {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()
	
	info, exists := tsm.states[fileID]
	if !exists {
		return nil, fmt.Errorf("transfer not found: %s", fileID)
	}
	
	if info.Metadata == nil {
		return nil, fmt.Errorf("metadata key not found: %s", key)
	}
	
	value, exists := info.Metadata[key]
	if !exists {
		return nil, fmt.Errorf("metadata key not found: %s", key)
	}
	
	return value, nil
}

// RemoveTransfer 移除传输状态
func (tsm *TransferStateManager) RemoveTransfer(fileID string) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	if _, exists := tsm.states[fileID]; !exists {
		return fmt.Errorf("transfer not found: %s", fileID)
	}
	
	delete(tsm.states, fileID)
	tsm.logger.Debugf("[TransferStateManager] 移除传输状态: FileID=%s", fileID)
	
	return nil
}

// GetAllTransfers 获取所有传输状态
func (tsm *TransferStateManager) GetAllTransfers() map[string]*TransferStateInfo {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()
	
	result := make(map[string]*TransferStateInfo)
	for fileID, info := range tsm.states {
		copy := *info
		if info.Metadata != nil {
			copy.Metadata = make(map[string]interface{})
			for k, v := range info.Metadata {
				copy.Metadata[k] = v
			}
		}
		result[fileID] = &copy
	}
	
	return result
}

// GetTransfersByState 根据状态获取传输列表
func (tsm *TransferStateManager) GetTransfersByState(state TransferState) []*TransferStateInfo {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()
	
	var result []*TransferStateInfo
	for _, info := range tsm.states {
		if info.State == state {
			copy := *info
			if info.Metadata != nil {
				copy.Metadata = make(map[string]interface{})
				for k, v := range info.Metadata {
					copy.Metadata[k] = v
				}
			}
			result = append(result, &copy)
		}
	}
	
	return result
}

// AddStateChangeCallback 添加状态变更回调
func (tsm *TransferStateManager) AddStateChangeCallback(callback TransferStateChangeCallback) {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	tsm.stateCallbacks = append(tsm.stateCallbacks, callback)
}

// isValidStateTransition 验证状态转换是否合法
func (tsm *TransferStateManager) isValidStateTransition(from, to TransferState) bool {
	// 定义合法的状态转换
	validTransitions := map[TransferState][]TransferState{
		StateIdle: {StateWaitingConfirmation, StatePendingUserConfirmation, StateFailed, StateCanceled},
		StateWaitingConfirmation: {StateConfirmed, StateRejected, StateFailed, StateCanceled},
		StatePendingUserConfirmation: {StateConfirmed, StateRejected, StateTransferring, StateFailed, StateCanceled},
		StateConfirmed: {StateTransferring, StateFailed, StateCanceled},
		StateRejected: {StateIdle, StateFailed, StateCanceled},
		StateTransferring: {StateCompleted, StateFailed, StateCanceled},
		StateCompleted: {StateIdle},
		StateFailed: {StateIdle, StateCanceled},
		StateCanceled: {StateIdle},
	}
	
	allowedStates, exists := validTransitions[from]
	if !exists {
		return false
	}
	
	for _, allowedState := range allowedStates {
		if allowedState == to {
			return true
		}
	}
	
	return false
}

// notifyStateChange 通知状态变更
func (tsm *TransferStateManager) notifyStateChange(fileID string, oldState, newState TransferState, info *TransferStateInfo) {
	tsm.mu.RLock()
	callbacks := make([]TransferStateChangeCallback, len(tsm.stateCallbacks))
	copy(callbacks, tsm.stateCallbacks)
	tsm.mu.RUnlock()
	
	for _, callback := range callbacks {
		go func(cb TransferStateChangeCallback) {
			defer func() {
				if r := recover(); r != nil {
					tsm.logger.Errorf("[TransferStateManager] 状态变更回调发生panic: %v", r)
				}
			}()
			cb(fileID, oldState, newState, info)
		}(callback)
	}
}

// cleanupLoop 清理循环
func (tsm *TransferStateManager) cleanupLoop() {
	ticker := time.NewTicker(tsm.cleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			tsm.cleanup()
		case <-tsm.stopCh:
			return
		}
	}
}

// cleanup 清理过期的状态记录
func (tsm *TransferStateManager) cleanup() {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()
	
	now := time.Now()
	var toDelete []string
	
	for fileID, info := range tsm.states {
		// 清理已完成、失败或取消的传输，且超过保留时间
		if (info.State == StateCompleted || info.State == StateFailed || info.State == StateCanceled) &&
			now.Sub(info.UpdatedAt) > tsm.stateRetention {
			toDelete = append(toDelete, fileID)
		}
	}
	
	for _, fileID := range toDelete {
		delete(tsm.states, fileID)
		tsm.logger.Debugf("[TransferStateManager] 清理过期状态: FileID=%s", fileID)
	}
	
	if len(toDelete) > 0 {
		tsm.logger.Infof("[TransferStateManager] 清理了 %d 个过期状态记录", len(toDelete))
	}
}

// GetTransferStats 获取传输统计信息（别名方法）
func (tsm *TransferStateManager) GetTransferStats() map[string]int {
	return tsm.GetStats()
}

// GetStats 获取统计信息
func (tsm *TransferStateManager) GetStats() map[string]int {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()
	
	stats := make(map[string]int)
	for _, info := range tsm.states {
		stats[info.State.String()]++
	}
	
	return stats
}