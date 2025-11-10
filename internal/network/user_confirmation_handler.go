package network

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TransferState 传输状态枚举
type TransferState int

const (
	StateIdle TransferState = iota
	StateWaitingConfirmation
	StatePendingUserConfirmation
	StateConfirmed
	StateRejected
	StateTransferring
	StateCompleted
	StateFailed
	StateCanceled
)

// 别名常量，用于向后兼容
const (
	TransferStatePending      = StatePendingUserConfirmation
	TransferStateAccepted     = StateConfirmed
	TransferStateRejected     = StateRejected
	TransferStateTransferring = StateTransferring
	TransferStateFailed       = StateFailed
	TransferStateCancelled    = StateCanceled
	TransferStateCompleted    = StateCompleted
)

// String 返回状态的字符串表示
func (s TransferState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateWaitingConfirmation:
		return "waiting_confirmation"
	case StatePendingUserConfirmation:
		return "pending_user_confirmation"
	case StateConfirmed:
		return "confirmed"
	case StateRejected:
		return "rejected"
	case StateTransferring:
		return "transferring"
	case StateCompleted:
		return "completed"
	case StateFailed:
		return "failed"
	case StateCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// FileTransferRequest 文件传输请求信息
type FileTransferRequest struct {
	FileID      string    `json:"file_id"`
	SenderID    string    `json:"sender_id"`
	ReceiverID  string    `json:"receiver_id"`
	FileName    string    `json:"file_name"`
	FileSize    int64     `json:"file_size"`
	FilePath    string    `json:"file_path"`
	ChunkCount  int       `json:"chunk_count"`
	FromAddr    string    `json:"from_addr"`
	FromUser    string    `json:"from_user"`
	Checksum    string    `json:"checksum"`
	Encrypted   bool      `json:"encrypted"`
	RequestTime time.Time `json:"request_time"`
}

// FileTransferConfirmationCallback 用户确认回调接口
type FileTransferConfirmationCallback interface {
	// OnFileTransferRequest 当收到文件传输请求时调用
	// 返回 true 表示用户确认接收，false 表示拒绝
	OnFileTransferRequest(request *FileTransferRequest) (bool, string, error)
	
	// OnTransferProgress 传输进度回调
	OnTransferProgress(fileID string, progress float64)
	
	// OnTransferComplete 传输完成回调
	OnTransferComplete(fileID string, success bool, err error)
	
	// OnTransferCanceled 传输取消回调
	OnTransferCanceled(fileID string, reason string)
}

// ConfirmationRequest 确认请求
type ConfirmationRequest struct {
	FileID      string
	Request     *FileTransferRequest
	ResponseCh  chan *ConfirmationResponse
	TimeoutCh   chan struct{}
	CreatedAt   time.Time
}

// ConfirmationResponse 确认响应
type ConfirmationResponse struct {
	Accepted bool
	Reason   string
	Error    error
}

// UserConfirmationHandler 用户确认处理器
type UserConfirmationHandler struct {
	mu                sync.RWMutex
	callback          FileTransferConfirmationCallback
	pendingRequests   map[string]*ConfirmationRequest
	confirmationTimeout time.Duration
	logger            Logger
	ctx               context.Context
	cancel            context.CancelFunc
}

// NewUserConfirmationHandler 创建用户确认处理器
func NewUserConfirmationHandler(callback FileTransferConfirmationCallback, logger Logger) *UserConfirmationHandler {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &UserConfirmationHandler{
		callback:            callback,
		pendingRequests:     make(map[string]*ConfirmationRequest),
		confirmationTimeout: 30 * time.Second, // 默认30秒超时
		logger:              logger,
		ctx:                 ctx,
		cancel:              cancel,
	}
}

// SetCallback 设置确认回调
func (uch *UserConfirmationHandler) SetCallback(callback FileTransferConfirmationCallback) {
	uch.mu.Lock()
	uch.callback = callback
	uch.mu.Unlock()
}

// GetCallback 获取当前设置的回调
func (uch *UserConfirmationHandler) GetCallback() FileTransferConfirmationCallback {
	uch.mu.RLock()
	defer uch.mu.RUnlock()
	return uch.callback
}

// SetConfirmationTimeout 设置确认超时时间
func (uch *UserConfirmationHandler) SetConfirmationTimeout(timeout time.Duration) {
	uch.mu.Lock()
	defer uch.mu.Unlock()
	uch.confirmationTimeout = timeout
}

// RequestUserConfirmation 请求用户确认
func (uch *UserConfirmationHandler) RequestUserConfirmation(request *FileTransferRequest) (*ConfirmationResponse, error) {
	if uch.callback == nil {
		return nil, fmt.Errorf("no confirmation callback set")
	}
	
	uch.mu.Lock()
	
	// 检查是否已有相同文件的确认请求
	if _, exists := uch.pendingRequests[request.FileID]; exists {
		uch.mu.Unlock()
		return nil, fmt.Errorf("confirmation request already pending for file %s", request.FileID)
	}
	
	// 创建确认请求
	confirmReq := &ConfirmationRequest{
		FileID:     request.FileID,
		Request:    request,
		ResponseCh: make(chan *ConfirmationResponse, 1),
		TimeoutCh:  make(chan struct{}, 1),
		CreatedAt:  time.Now(),
	}
	
	uch.pendingRequests[request.FileID] = confirmReq
	uch.mu.Unlock()
	
	uch.logger.Infof("[UserConfirmationHandler] 请求用户确认文件传输: FileID=%s, FileName=%s, Size=%d",
		request.FileID, request.FileName, request.FileSize)
	
	// 启动超时计时器
	go uch.startConfirmationTimeout(request.FileID)
	
	// 异步调用用户回调
	go func() {
		defer func() {
			if r := recover(); r != nil {
				uch.logger.Errorf("[UserConfirmationHandler] 用户确认回调发生panic: %v", r)
				uch.sendResponse(request.FileID, &ConfirmationResponse{
					Accepted: false,
					Reason:   "internal error",
					Error:    fmt.Errorf("callback panic: %v", r),
				})
			}
		}()
		
		accepted, reason, err := uch.callback.OnFileTransferRequest(request)
		uch.sendResponse(request.FileID, &ConfirmationResponse{
			Accepted: accepted,
			Reason:   reason,
			Error:    err,
		})
	}()
	
	// 等待响应或超时
	select {
	case response := <-confirmReq.ResponseCh:
		uch.cleanupRequest(request.FileID)
		return response, nil
	case <-confirmReq.TimeoutCh:
		uch.cleanupRequest(request.FileID)
		return &ConfirmationResponse{
			Accepted: false,
			Reason:   "confirmation timeout",
			Error:    fmt.Errorf("user confirmation timeout after %v", uch.confirmationTimeout),
		}, nil
	case <-uch.ctx.Done():
		uch.cleanupRequest(request.FileID)
		return &ConfirmationResponse{
			Accepted: false,
			Reason:   "handler stopped",
			Error:    fmt.Errorf("confirmation handler stopped"),
		}, nil
	}
}

// sendResponse 发送确认响应
func (uch *UserConfirmationHandler) sendResponse(fileID string, response *ConfirmationResponse) {
	uch.mu.RLock()
	req, exists := uch.pendingRequests[fileID]
	uch.mu.RUnlock()
	
	if !exists {
		uch.logger.Warnf("[UserConfirmationHandler] 尝试发送响应到不存在的确认请求: FileID=%s", fileID)
		return
	}
	
	uch.logger.Infof("[UserConfirmationHandler] 准备发送确认响应: FileID=%s, Accepted=%v", fileID, response.Accepted)
	
	select {
	case req.ResponseCh <- response:
		uch.logger.Infof("[UserConfirmationHandler] 确认响应已成功发送: FileID=%s, Accepted=%v", fileID, response.Accepted)
	case <-time.After(5 * time.Second):
		uch.logger.Errorf("[UserConfirmationHandler] 发送确认响应超时: FileID=%s, Accepted=%v", fileID, response.Accepted)
	default:
		uch.logger.Warnf("[UserConfirmationHandler] 确认响应通道已满或已关闭: FileID=%s", fileID)
	}
}

// startConfirmationTimeout 启动确认超时计时器
func (uch *UserConfirmationHandler) startConfirmationTimeout(fileID string) {
	timer := time.NewTimer(uch.confirmationTimeout)
	defer timer.Stop()
	
	select {
	case <-timer.C:
		uch.logger.Warnf("[UserConfirmationHandler] 用户确认超时: FileID=%s", fileID)
		uch.mu.RLock()
		req, exists := uch.pendingRequests[fileID]
		uch.mu.RUnlock()
		
		if exists {
			select {
			case req.TimeoutCh <- struct{}{}:
			default:
			}
		}
	case <-uch.ctx.Done():
		return
	}
}

// cleanupRequest 清理确认请求
func (uch *UserConfirmationHandler) cleanupRequest(fileID string) {
	uch.mu.Lock()
	defer uch.mu.Unlock()
	
	if req, exists := uch.pendingRequests[fileID]; exists {
		close(req.ResponseCh)
		close(req.TimeoutCh)
		delete(uch.pendingRequests, fileID)
		uch.logger.Debugf("[UserConfirmationHandler] 清理确认请求: FileID=%s", fileID)
	}
}

// CancelConfirmation 取消确认请求
func (uch *UserConfirmationHandler) CancelConfirmation(fileID string) error {
	uch.mu.RLock()
	_, exists := uch.pendingRequests[fileID]
	uch.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("no pending confirmation request for file %s", fileID)
	}
	
	uch.sendResponse(fileID, &ConfirmationResponse{
		Accepted: false,
		Reason:   "canceled",
		Error:    fmt.Errorf("confirmation canceled"),
	})
	
	uch.logger.Infof("[UserConfirmationHandler] 取消确认请求: FileID=%s", fileID)
	return nil
}

// GetPendingRequests 获取待确认的请求列表
func (uch *UserConfirmationHandler) GetPendingRequests() map[string]*FileTransferRequest {
	uch.mu.RLock()
	defer uch.mu.RUnlock()
	
	result := make(map[string]*FileTransferRequest)
	for fileID, req := range uch.pendingRequests {
		result[fileID] = req.Request
	}
	return result
}

// Stop 停止确认处理器
func (uch *UserConfirmationHandler) Stop() {
	uch.cancel()
	
	uch.mu.Lock()
	defer uch.mu.Unlock()
	
	// 清理所有待确认的请求
	for fileID := range uch.pendingRequests {
		uch.cleanupRequest(fileID)
	}
	
	uch.logger.Infof("[UserConfirmationHandler] 确认处理器已停止")
}

// NotifyProgress 通知传输进度
func (uch *UserConfirmationHandler) NotifyProgress(fileID string, progress float64) {
	if uch.callback != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					uch.logger.Errorf("[UserConfirmationHandler] 进度回调发生panic: %v", r)
				}
			}()
			uch.callback.OnTransferProgress(fileID, progress)
		}()
	}
}

// NotifyComplete 通知传输完成
func (uch *UserConfirmationHandler) NotifyComplete(fileID string, success bool, err error) {
	if uch.callback != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					uch.logger.Errorf("[UserConfirmationHandler] 完成回调发生panic: %v", r)
				}
			}()
			uch.callback.OnTransferComplete(fileID, success, err)
		}()
	}
}

// NotifyCanceled 通知传输取消
func (uch *UserConfirmationHandler) NotifyCanceled(fileID string, reason string) {
	if uch.callback != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					uch.logger.Errorf("[UserConfirmationHandler] 取消回调发生panic: %v", r)
				}
			}()
			uch.callback.OnTransferCanceled(fileID, reason)
		}()
	}
}