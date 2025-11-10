package file

import (
	"sync"
	"time"
)

// TransferSession 传输会话
type TransferSession struct {
	FileID       string
	WindowConfig *WindowConfig
	Window       *SlidingWindow
	Progress     *Progress
	StartTime    time.Time
	LastUpdate   time.Time
	Status       string
	Accepted     bool
	Rejected     bool
	ConfirmationChan chan bool
	CancelChan   chan struct{}
	mu           sync.RWMutex
}

// SessionStatus 会话状态
const (
	SessionStatusIdle     = "idle"
	SessionStatusStarting = "starting"
	SessionStatusRunning  = "running"
	SessionStatusPaused   = "paused"
	SessionStatusComplete = "complete"
	SessionStatusFailed   = "failed"
	SessionStatusCanceled = "canceled"
	SessionStatusAccepted = "accepted"
	SessionStatusRejected = "rejected"
)

// NewTransferSession 创建传输会话
func NewTransferSession(fileID string, windowConfig *WindowConfig, logger Logger) *TransferSession {
	return &TransferSession{
		FileID:       fileID,
		WindowConfig: windowConfig,
		Window:       NewSlidingWindow(windowConfig.WindowSize),
		Progress:     NewProgress(0, logger),
		StartTime:    time.Now(),
		LastUpdate:   time.Now(),
		Status:       SessionStatusIdle,
		Accepted:     false,
		Rejected:     false,
		ConfirmationChan: make(chan bool, 1),
		CancelChan:   make(chan struct{}, 1),
	}
}

// Start 开始会话
func (ts *TransferSession) Start() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusStarting
	ts.StartTime = time.Now()
	ts.LastUpdate = time.Now()
}

// SetRunning 设置为运行状态
func (ts *TransferSession) SetRunning() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusRunning
	ts.LastUpdate = time.Now()
}

// SetPaused 设置为暂停状态
func (ts *TransferSession) SetPaused() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusPaused
	ts.LastUpdate = time.Now()
}

// SetComplete 设置为完成状态
func (ts *TransferSession) SetComplete() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusComplete
	ts.LastUpdate = time.Now()
}

// SetFailed 设置为失败状态
func (ts *TransferSession) SetFailed() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusFailed
	ts.LastUpdate = time.Now()
}

// SetCanceled 设置为取消状态
func (ts *TransferSession) SetCanceled() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusCanceled
	ts.LastUpdate = time.Now()
}

// SetCompleted 设置为完成状态（别名方法）
func (ts *TransferSession) SetCompleted() {
	ts.SetComplete()
}

// GetStatus 获取会话状态
func (ts *TransferSession) GetStatus() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Status
}

// GetStartTime 获取开始时间
func (ts *TransferSession) GetStartTime() time.Time {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.StartTime
}

// GetLastUpdate 获取最后更新时间
func (ts *TransferSession) GetLastUpdate() time.Time {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.LastUpdate
}

// GetDuration 获取会话持续时间
func (ts *TransferSession) GetDuration() time.Duration {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return time.Since(ts.StartTime)
}

// UpdateProgress 更新进度
func (ts *TransferSession) UpdateProgress(completed, total int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if total > 0 {
		ts.Progress.SetTotal(total)
	}
	ts.Progress.Update(completed)
	ts.LastUpdate = time.Now()
}

// GetProgress 获取进度
func (ts *TransferSession) GetProgress() (completed, total int) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Progress.GetProgress()
}

// GetProgressPercent 获取进度百分比
func (ts *TransferSession) GetProgressPercent() float64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Progress.GetPercent()
}

// GetWindowStats 获取窗口统计信息
func (ts *TransferSession) GetWindowStats() map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Window.GetStats()
}

// Reset 重置会话
func (ts *TransferSession) Reset() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusIdle
	ts.StartTime = time.Now()
	ts.LastUpdate = time.Now()
	ts.Window.Reset()
	ts.Progress = NewProgress(0, nil) // 需要传入logger
}

// GetSessionInfo 获取会话信息
func (ts *TransferSession) GetSessionInfo() map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	completed, total := ts.Progress.GetProgress()
	duration := time.Since(ts.StartTime)

	return map[string]interface{}{
		"file_id":            ts.FileID,
		"status":             ts.Status,
		"start_time":         ts.StartTime,
		"last_update":        ts.LastUpdate,
		"duration":           duration,
		"progress_completed": completed,
		"progress_total":     total,
		"progress_percent":   ts.Progress.GetPercent(),
		"window_stats":       ts.Window.GetStats(),
	}
}

// IsActive 检查会话是否活跃
func (ts *TransferSession) IsActive() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Status == SessionStatusStarting || ts.Status == SessionStatusRunning
}

// IsComplete 检查会话是否完成
func (ts *TransferSession) IsComplete() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Status == SessionStatusComplete
}

// IsFailed 检查会话是否失败
func (ts *TransferSession) IsFailed() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Status == SessionStatusFailed
}

// IsCanceled 检查会话是否取消
func (ts *TransferSession) IsCanceled() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Status == SessionStatusCanceled
}

// IsAccepted 检查是否已接受
func (ts *TransferSession) IsAccepted() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Accepted
}

// IsRejected 检查是否已拒绝
func (ts *TransferSession) IsRejected() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.Rejected
}

// SetAccepted 设置为已接受
func (ts *TransferSession) SetAccepted() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Accepted = true
	ts.Status = SessionStatusAccepted
}

// SetRejected 设置为已拒绝
func (ts *TransferSession) SetRejected() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Rejected = true
	ts.Status = SessionStatusRejected
}

// Cancel 取消传输
func (ts *TransferSession) Cancel() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = SessionStatusCanceled
	select {
	case ts.CancelChan <- struct{}{}:
	default:
	}
}

// MarkChunkAcked 标记分片已确认
func (ts *TransferSession) MarkChunkAcked(chunkIndex int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	// 这里可以添加分片确认的逻辑
	ts.LastUpdate = time.Now()
}

// MarkChunkFailed 标记分片发送失败
func (ts *TransferSession) MarkChunkFailed(chunkIndex int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	// 这里可以添加分片失败的逻辑，比如标记需要重传
	ts.LastUpdate = time.Now()
}
