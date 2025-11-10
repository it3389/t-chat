package network

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// RetryManager 重试管理器
type RetryManager struct {
	config   *FileTransferConfig
	logger   Logger
	retries  map[string]*RetryInfo
	mu       sync.RWMutex
}

// RetryInfo 重试信息
type RetryInfo struct {
	FileID       string
	Attempts     int
	LastAttempt  time.Time
	NextAttempt  time.Time
	Delay        time.Duration
	MaxAttempts  int
	Operation    string
	Context      context.Context
	Cancel       context.CancelFunc
}

// NewRetryManager 创建重试管理器
func NewRetryManager(config *FileTransferConfig, logger Logger) *RetryManager {
	return &RetryManager{
		config:  config,
		logger:  logger,
		retries: make(map[string]*RetryInfo),
	}
}

// ShouldRetry 判断是否应该重试
func (rm *RetryManager) ShouldRetry(fileID string, operation string, err error) bool {
	rm.mu.RLock()
	retryInfo, exists := rm.retries[fileID]
	rm.mu.RUnlock()

	if !exists {
		// 首次失败，创建重试信息
		rm.createRetryInfo(fileID, operation)
		return true
	}

	// 检查是否超过最大重试次数
	if retryInfo.Attempts >= retryInfo.MaxAttempts {
		rm.logger.Errorf("[RetryManager] 文件 %s 操作 %s 重试次数已达上限: %d", fileID, operation, retryInfo.MaxAttempts)
		return false
	}

	// 检查是否到了重试时间
	if time.Now().Before(retryInfo.NextAttempt) {
		return false
	}

	return true
}

// RecordRetry 记录重试
func (rm *RetryManager) RecordRetry(fileID string, operation string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	retryInfo, exists := rm.retries[fileID]
	if !exists {
		rm.logger.Errorf("[RetryManager] 找不到重试信息: FileID=%s", fileID)
		return
	}

	retryInfo.Attempts++
	retryInfo.LastAttempt = time.Now()
	
	// 计算下次重试时间（指数退避）
	delay := time.Duration(float64(rm.config.RetryDelay) * math.Pow(rm.config.BackoffFactor, float64(retryInfo.Attempts-1)))
	if delay > rm.config.MaxRetryDelay {
		delay = rm.config.MaxRetryDelay
	}
	retryInfo.Delay = delay
	retryInfo.NextAttempt = time.Now().Add(delay)

	rm.logger.Debugf("[RetryManager] 记录重试: FileID=%s, Operation=%s, Attempts=%d, NextAttempt=%v", 
		fileID, operation, retryInfo.Attempts, retryInfo.NextAttempt)
}

// createRetryInfo 创建重试信息
func (rm *RetryManager) createRetryInfo(fileID string, operation string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), rm.config.Timeout)
	retryInfo := &RetryInfo{
		FileID:      fileID,
		Attempts:    0,
		LastAttempt: time.Time{},
		NextAttempt: time.Now(),
		Delay:       rm.config.RetryDelay,
		MaxAttempts: rm.config.RetryCount,
		Operation:   operation,
		Context:     ctx,
		Cancel:      cancel,
	}

	rm.retries[fileID] = retryInfo
	rm.logger.Debugf("[RetryManager] 创建重试信息: FileID=%s, Operation=%s, MaxAttempts=%d", 
		fileID, operation, retryInfo.MaxAttempts)
}

// RemoveRetry 移除重试信息
func (rm *RetryManager) RemoveRetry(fileID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if retryInfo, exists := rm.retries[fileID]; exists {
		if retryInfo.Cancel != nil {
			retryInfo.Cancel()
		}
		delete(rm.retries, fileID)
		rm.logger.Debugf("[RetryManager] 移除重试信息: FileID=%s", fileID)
	}
}

// GetRetryInfo 获取重试信息
func (rm *RetryManager) GetRetryInfo(fileID string) *RetryInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.retries[fileID]
}

// GetAllRetries 获取所有重试信息
func (rm *RetryManager) GetAllRetries() map[string]*RetryInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[string]*RetryInfo)
	for k, v := range rm.retries {
		result[k] = v
	}
	return result
}

// WaitForRetry 等待重试时间
func (rm *RetryManager) WaitForRetry(fileID string) error {
	retryInfo := rm.GetRetryInfo(fileID)
	if retryInfo == nil {
		return fmt.Errorf("找不到重试信息: FileID=%s", fileID)
	}

	waitTime := time.Until(retryInfo.NextAttempt)
	if waitTime <= 0 {
		return nil
	}

	rm.logger.Debugf("[RetryManager] 等待重试: FileID=%s, WaitTime=%v", fileID, waitTime)

	select {
	case <-time.After(waitTime):
		return nil
	case <-retryInfo.Context.Done():
		return retryInfo.Context.Err()
	}
}

// CancelRetry 取消重试
func (rm *RetryManager) CancelRetry(fileID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if retryInfo, exists := rm.retries[fileID]; exists {
		if retryInfo.Cancel != nil {
			retryInfo.Cancel()
		}
		rm.logger.Debugf("[RetryManager] 取消重试: FileID=%s", fileID)
	}
}

// Cleanup 清理过期的重试信息
func (rm *RetryManager) Cleanup() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	for fileID, retryInfo := range rm.retries {
		// 清理超时的重试信息
		if retryInfo.Context.Err() != nil || 
		   (retryInfo.Attempts >= retryInfo.MaxAttempts && now.Sub(retryInfo.LastAttempt) > time.Hour) {
			if retryInfo.Cancel != nil {
				retryInfo.Cancel()
			}
			delete(rm.retries, fileID)
			rm.logger.Debugf("[RetryManager] 清理过期重试信息: FileID=%s", fileID)
		}
	}
}