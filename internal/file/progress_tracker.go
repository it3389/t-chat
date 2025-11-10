package file

import (
	"sync"
)

// ProgressTracker 进度跟踪器实现
type ProgressTracker struct {
	progress map[string]float64
	callback func(sessionID string, progress float64)
	mutex    sync.RWMutex
}

// NewProgressTracker 创建进度跟踪器
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		progress: make(map[string]float64),
	}
}

// UpdateProgress 更新进度
func (pt *ProgressTracker) UpdateProgress(sessionID string, progress float64) {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()
	
	// 确保进度在 0-100 范围内
	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}
	
	pt.progress[sessionID] = progress
	
	// 调用回调函数
	if pt.callback != nil {
		go pt.callback(sessionID, progress)
	}
}

// GetProgress 获取进度
func (pt *ProgressTracker) GetProgress(sessionID string) float64 {
	pt.mutex.RLock()
	defer pt.mutex.RUnlock()
	
	if progress, exists := pt.progress[sessionID]; exists {
		return progress
	}
	return 0.0
}

// SetCallback 设置进度回调函数
func (pt *ProgressTracker) SetCallback(callback func(sessionID string, progress float64)) {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()
	pt.callback = callback
}

// RemoveProgress 移除进度记录
func (pt *ProgressTracker) RemoveProgress(sessionID string) {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()
	delete(pt.progress, sessionID)
}

// GetAllProgress 获取所有进度
func (pt *ProgressTracker) GetAllProgress() map[string]float64 {
	pt.mutex.RLock()
	defer pt.mutex.RUnlock()
	
	result := make(map[string]float64)
	for sessionID, progress := range pt.progress {
		result[sessionID] = progress
	}
	return result
}

// Clear 清空所有进度记录
func (pt *ProgressTracker) Clear() {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()
	pt.progress = make(map[string]float64)
}