package network

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FileTransferIntegration 文件传输流程集成器
// 实现完整的文件传输流程：请求->确认->传输->完成
type FileTransferIntegration struct {
	pineconeService *PineconeService
	adapter         *FileTransferAdapter
	logger          Logger
	mu              sync.RWMutex
	initialized     bool
}

// NewFileTransferIntegration 创建文件传输流程集成器
func NewFileTransferIntegration(pineconeService *PineconeService, logger Logger) *FileTransferIntegration {
	// 创建文件传输服务（暂时不传入confirmationHandler，稍后设置）
	fileTransferService := NewFileTransferService(pineconeService, logger, nil)
	
	// 创建文件传输适配器
	adapter := NewFileTransferAdapter(pineconeService, fileTransferService, logger)
	
	return &FileTransferIntegration{
		pineconeService: pineconeService,
		adapter:         adapter,
		logger:          logger,
		initialized:     false,
	}
}

// Initialize 初始化文件传输集成器
func (fti *FileTransferIntegration) Initialize() error {
	fti.mu.Lock()
	defer fti.mu.Unlock()
	
	if fti.initialized {
		return fmt.Errorf("文件传输集成器已经初始化")
	}
	
	// 启动文件传输服务
	fileTransferService := fti.adapter.GetFileTransferService()
	if fileTransferService != nil {
		ctx := context.Background()
		err := fileTransferService.Start(ctx)
		if err != nil {
			fti.logger.Errorf("[FileTransferIntegration] 启动文件传输服务失败: %v", err)
			return err
		}
	}
	
	// 初始化适配器
	err := fti.adapter.Initialize()
	if err != nil {
		fti.logger.Errorf("[FileTransferIntegration] 初始化适配器失败: %v", err)
		return err
	}
	
	fti.initialized = true
	fti.logger.Infof("[FileTransferIntegration] 文件传输集成器初始化完成")
	return nil
}

// Cleanup 清理文件传输集成器
func (fti *FileTransferIntegration) Cleanup() error {
	fti.mu.Lock()
	defer fti.mu.Unlock()
	
	if !fti.initialized {
		return fmt.Errorf("文件传输集成器未初始化")
	}
	
	// 清理适配器
	err := fti.adapter.Cleanup()
	if err != nil {
		fti.logger.Warnf("[FileTransferIntegration] 清理适配器失败: %v", err)
	}
	
	// 停止文件传输服务
	fileTransferService := fti.adapter.GetFileTransferService()
	if fileTransferService != nil {
		err := fileTransferService.Stop()
		if err != nil {
			fti.logger.Warnf("[FileTransferIntegration] 停止文件传输服务失败: %v", err)
		}
	}
	
	fti.initialized = false
	fti.logger.Infof("[FileTransferIntegration] 文件传输集成器已清理")
	return nil
}

// IsInitialized 检查是否已初始化
func (fti *FileTransferIntegration) IsInitialized() bool {
	fti.mu.RLock()
	defer fti.mu.RUnlock()
	return fti.initialized
}

// SendFile 发送文件 - 实现完整流程
func (fti *FileTransferIntegration) SendFile(filePath, toAddr string) error {
	if !fti.IsInitialized() {
		return fmt.Errorf("文件传输集成器未初始化")
	}
	
	fti.logger.Infof("[FileTransferIntegration] 开始文件传输流程: %s -> %s", filePath, toAddr)
	
	// 第一步：发送文件请求
	err := fti.adapter.SendFile(filePath, toAddr)
	if err != nil {
		fti.logger.Errorf("[FileTransferIntegration] 文件传输失败: %v", err)
		return err
	}
	
	fti.logger.Infof("[FileTransferIntegration] 文件传输流程已启动: %s -> %s", filePath, toAddr)
	return nil
}

// GetTransferStatus 获取传输状态
func (fti *FileTransferIntegration) GetTransferStatus() map[string]interface{} {
	if !fti.IsInitialized() {
		return map[string]interface{}{
			"initialized": false,
			"error":       "文件传输集成器未初始化",
		}
	}
	
	status := map[string]interface{}{
		"initialized": true,
		"adapter":     fti.adapter.GetStats(),
		"sessions":    fti.adapter.GetTransferSessions(),
	}
	
	return status
}

// GetAdapter 获取适配器
func (fti *FileTransferIntegration) GetAdapter() *FileTransferAdapter {
	return fti.adapter
}

// GetFileTransferService 获取文件传输服务
func (fti *FileTransferIntegration) GetFileTransferService() *FileTransferService {
	return fti.adapter.GetFileTransferService()
}

// SetUserConfirmationCallback 设置用户确认回调
func (fti *FileTransferIntegration) SetUserConfirmationCallback(callback FileTransferConfirmationCallback) {
	fileTransferService := fti.adapter.GetFileTransferService()
	if fileTransferService != nil {
		fileTransferService.SetUserConfirmationCallback(callback)
		fti.logger.Debugf("[FileTransferIntegration] 用户确认回调已设置")
	} else {
		fti.logger.Warnf("[FileTransferIntegration] 文件传输服务不可用，无法设置用户确认回调")
	}
}

// ListActiveTransfers 列出活跃的传输
func (fti *FileTransferIntegration) ListActiveTransfers() []map[string]interface{} {
	if !fti.IsInitialized() {
		return []map[string]interface{}{}
	}
	
	sessions := fti.adapter.GetTransferSessions()
	var activeTransfers []map[string]interface{}
	
	for _, sessionData := range sessions {
		// sessionData 已经是 map[string]interface{} 类型，不需要类型断言
		fileID, hasFileID := sessionData["file_id"]
		status := sessionData["status"]
		if (status == "running" || status == "pending") && hasFileID {
			activeTransfers = append(activeTransfers, map[string]interface{}{
				"file_id":    fileID,
				"status":     status,
				"progress":   sessionData["progress"],
				"start_time": sessionData["start_time"],
				"duration":   sessionData["duration"],
			})
		}
	}
	
	return activeTransfers
}

// CancelTransfer 取消传输
func (fti *FileTransferIntegration) CancelTransfer(fileID string) error {
	if !fti.IsInitialized() {
		return fmt.Errorf("文件传输集成器未初始化")
	}
	
	fileTransferService := fti.GetFileTransferService()
	if fileTransferService == nil {
		return fmt.Errorf("文件传输服务不可用")
	}
	
	// 获取会话并取消
	sessions := fileTransferService.GetTransferSessions()
	session := sessions[fileID]
	if session == nil {
		return fmt.Errorf("传输会话不存在: %s", fileID)
	}
	
	session.SetCanceled()
	fti.logger.Infof("[FileTransferIntegration] 传输已取消: FileID=%s", fileID)
	return nil
}

// PauseTransfer 暂停传输
func (fti *FileTransferIntegration) PauseTransfer(fileID string) error {
	if !fti.IsInitialized() {
		return fmt.Errorf("文件传输集成器未初始化")
	}
	
	fileTransferService := fti.GetFileTransferService()
	if fileTransferService == nil {
		return fmt.Errorf("文件传输服务不可用")
	}
	
	// 获取会话并暂停
	sessions := fileTransferService.GetTransferSessions()
	session := sessions[fileID]
	if session == nil {
		return fmt.Errorf("传输会话不存在: %s", fileID)
	}
	
	session.SetPaused()
	fti.logger.Infof("[FileTransferIntegration] 传输已暂停: FileID=%s", fileID)
	return nil
}

// ResumeTransfer 恢复传输
func (fti *FileTransferIntegration) ResumeTransfer(fileID string) error {
	if !fti.IsInitialized() {
		return fmt.Errorf("文件传输集成器未初始化")
	}
	
	fileTransferService := fti.GetFileTransferService()
	if fileTransferService == nil {
		return fmt.Errorf("文件传输服务不可用")
	}
	
	// 获取会话并恢复
	sessions := fileTransferService.GetTransferSessions()
	session := sessions[fileID]
	if session == nil {
		return fmt.Errorf("传输会话不存在: %s", fileID)
	}
	
	session.SetRunning()
	fti.logger.Infof("[FileTransferIntegration] 传输已恢复: FileID=%s", fileID)
	return nil
}

// GetTransferProgress 获取传输进度
func (fti *FileTransferIntegration) GetTransferProgress(fileID string) (float64, error) {
	if !fti.IsInitialized() {
		return 0, fmt.Errorf("文件传输集成器未初始化")
	}
	
	fileTransferService := fti.GetFileTransferService()
	if fileTransferService == nil {
		return 0, fmt.Errorf("文件传输服务不可用")
	}
	
	// 获取会话进度
	sessions := fileTransferService.GetTransferSessions()
	session := sessions[fileID]
	if session == nil {
		return 0, fmt.Errorf("传输会话不存在: %s", fileID)
	}
	
	return session.GetProgressPercent(), nil
}

// WaitForTransferComplete 等待传输完成
func (fti *FileTransferIntegration) WaitForTransferComplete(fileID string, timeout time.Duration) error {
	if !fti.IsInitialized() {
		return fmt.Errorf("文件传输集成器未初始化")
	}
	
	fileTransferService := fti.GetFileTransferService()
	if fileTransferService == nil {
		return fmt.Errorf("文件传输服务不可用")
	}
	
	startTime := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			sessions := fileTransferService.GetTransferSessions()
			session := sessions[fileID]
			if session == nil {
				return fmt.Errorf("传输会话不存在: %s", fileID)
			}
			
			status := session.GetStatus()
			if status == "completed" {
				fti.logger.Infof("[FileTransferIntegration] 传输完成: FileID=%s", fileID)
				return nil
			} else if status == "failed" || status == "canceled" {
				return fmt.Errorf("传输失败或被取消: FileID=%s, Status=%s", fileID, status)
			}
			
			if time.Since(startTime) > timeout {
				return fmt.Errorf("传输超时: FileID=%s", fileID)
			}
		}
	}
}

// GetStats 获取集成器统计信息
func (fti *FileTransferIntegration) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"initialized": fti.IsInitialized(),
		"timestamp":   time.Now().Unix(),
	}
	
	if fti.IsInitialized() {
		stats["adapter"] = fti.adapter.GetStats()
		stats["active_transfers"] = len(fti.ListActiveTransfers())
		stats["total_sessions"] = len(fti.adapter.GetTransferSessions())
	} else {
		stats["error"] = "未初始化"
	}
	
	return stats
}