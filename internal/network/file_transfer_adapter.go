package network

import (
	"fmt"
	"sync"
)

// FileTransferAdapter 文件传输网络适配器
// 连接PineconeService和FileTransferService，实现完整的文件传输流程
type FileTransferAdapter struct {
	pineconeService     *PineconeService
	fileTransferService *FileTransferService
	logger              Logger
	handlers            map[string]MessageHandlerInterface
	mu                  sync.RWMutex
	initialized         bool
}

// NewFileTransferAdapter 创建文件传输适配器
func NewFileTransferAdapter(pineconeService *PineconeService, fileTransferService *FileTransferService, logger Logger) *FileTransferAdapter {
	adapter := &FileTransferAdapter{
		pineconeService:     pineconeService,
		fileTransferService: fileTransferService,
		logger:              logger,
		handlers:            make(map[string]MessageHandlerInterface),
		initialized:         false,
	}
	
	// 初始化文件传输处理器
	adapter.initializeHandlers()
	
	return adapter
}

// initializeHandlers 初始化文件传输处理器
func (fta *FileTransferAdapter) initializeHandlers() {
	// 创建文件传输处理器
	fileRequestHandler := &FileRequestHandler{
		pineconeService:     fta.pineconeService,
		fileTransferService: fta.fileTransferService,
		logger:              fta.logger,
	}
	
	fileChunkHandler := &FileChunkHandler{
		pineconeService:     fta.pineconeService,
		fileTransferService: fta.fileTransferService,
		logger:              fta.logger,
	}
	
	fileAckHandler := &FileAckHandler{
		pineconeService:     fta.pineconeService,
		fileTransferService: fta.fileTransferService,
		logger:              fta.logger,
	}
	
	fileCompleteHandler := &FileCompleteHandler{
		pineconeService:     fta.pineconeService,
		fileTransferService: fta.fileTransferService,
		logger:              fta.logger,
	}
	
	fileNackHandler := &FileNackHandler{
		pineconeService:     fta.pineconeService,
		fileTransferService: fta.fileTransferService,
		logger:              fta.logger,
	}
	
	fileDataHandler := &FileDataHandler{
		pineconeService:     fta.pineconeService,
		fileTransferService: fta.fileTransferService,
		logger:              fta.logger,
	}
	
	// 注册处理器
	fta.handlers[MessageTypeFileRequest] = fileRequestHandler
	fta.handlers[MessageTypeFileChunk] = fileChunkHandler
	fta.handlers[MessageTypeFileAck] = fileAckHandler
	fta.handlers[MessageTypeFileComplete] = fileCompleteHandler
	fta.handlers[MessageTypeFileNack] = fileNackHandler
	fta.handlers["file_data"] = fileDataHandler
	
	fta.logger.Debugf("[FileTransferAdapter] 文件传输处理器已初始化，共注册 %d 个处理器", len(fta.handlers))
}

// Initialize 初始化适配器
func (fta *FileTransferAdapter) Initialize() error {
	fta.mu.Lock()
	defer fta.mu.Unlock()
	
	if fta.initialized {
		return fmt.Errorf("文件传输适配器已经初始化")
	}
	
	// 初始化所有处理器
	for msgType, handler := range fta.handlers {
		err := handler.Initialize()
		if err != nil {
			fta.logger.Errorf("[FileTransferAdapter] 初始化处理器失败: Type=%s, Error=%v", msgType, err)
			return err
		}
	}
	
	// 注册处理器到PineconeService的消息分发器
	for msgType, handler := range fta.handlers {
		// 通过PineconeService的消息分发器注册处理器
		if fta.pineconeService.messageDispatcher != nil {
			err := fta.pineconeService.messageDispatcher.RegisterHandler(handler)
			if err != nil {
				fta.logger.Errorf("[FileTransferAdapter] 注册处理器失败: Type=%s, Error=%v", msgType, err)
				return err
			}
		} else {
			fta.logger.Warnf("[FileTransferAdapter] PineconeService消息分发器未初始化，跳过注册: Type=%s", msgType)
		}
	}
	
	fta.initialized = true
	fta.logger.Infof("[FileTransferAdapter] 文件传输适配器初始化完成")
	return nil
}

// Cleanup 清理适配器
func (fta *FileTransferAdapter) Cleanup() error {
	fta.mu.Lock()
	defer fta.mu.Unlock()
	
	if !fta.initialized {
		return fmt.Errorf("文件传输适配器未初始化")
	}
	
	// 注销处理器（跳过，因为PineconeService没有UnregisterMessageHandler方法）
	for msgType := range fta.handlers {
		fta.logger.Debugf("[FileTransferAdapter] 跳过注销处理器: Type=%s", msgType)
	}
	
	// 清理所有处理器
	for msgType, handler := range fta.handlers {
		if cleanupErr := handler.Cleanup(); cleanupErr != nil {
			fta.logger.Warnf("[FileTransferAdapter] 清理处理器失败: Type=%s, Error=%v", msgType, cleanupErr)
		}
	}
	
	fta.initialized = false
	fta.logger.Infof("[FileTransferAdapter] 文件传输适配器已清理")
	return nil
}

// IsInitialized 检查是否已初始化
func (fta *FileTransferAdapter) IsInitialized() bool {
	fta.mu.RLock()
	defer fta.mu.RUnlock()
	return fta.initialized
}

// SendFile 发送文件
func (fta *FileTransferAdapter) SendFile(filePath, toAddr string) error {
	if !fta.IsInitialized() {
		return fmt.Errorf("文件传输适配器未初始化")
	}
	
	if fta.fileTransferService == nil {
		return fmt.Errorf("文件传输服务未设置")
	}
	
	fta.logger.Infof("[FileTransferAdapter] 开始发送文件: %s -> %s", filePath, toAddr)
	return fta.fileTransferService.SendFile(filePath, toAddr)
}

// GetTransferSessions 获取传输会话列表
func (fta *FileTransferAdapter) GetTransferSessions() []map[string]interface{} {
	fta.mu.RLock()
	defer fta.mu.RUnlock()
	
	if fta.fileTransferService == nil {
		return []map[string]interface{}{}
	}
	
	// 获取所有传输状态
	allTransfers := fta.fileTransferService.GetAllTransfers()
	result := make([]map[string]interface{}, 0, len(allTransfers))
	
	for _, transfer := range allTransfers {
		transferInfo := map[string]interface{}{
			"file_id":    transfer.FileID,
			"file_name":  transfer.FileName,
			"file_size":  transfer.FileSize,
			"state":      transfer.State.String(),
			"progress":   transfer.Progress,
			"created_at": transfer.CreatedAt,
			"updated_at": transfer.UpdatedAt,
			"from_addr":  transfer.FromAddr,
			"to_addr":    transfer.ToAddr,
		}
		if transfer.ErrorMessage != "" {
			transferInfo["error"] = transfer.ErrorMessage
		}
		result = append(result, transferInfo)
	}
	
	return result
}

// GetFileTransferService 获取文件传输服务
func (fta *FileTransferAdapter) GetFileTransferService() *FileTransferService {
	return fta.fileTransferService
}

// SetFileTransferService 设置文件传输服务
func (fta *FileTransferAdapter) SetFileTransferService(service *FileTransferService) {
	fta.mu.Lock()
	defer fta.mu.Unlock()
	
	fta.fileTransferService = service
	
	// 更新处理器中的服务引用
	for _, handler := range fta.handlers {
		switch h := handler.(type) {
		case *FileRequestHandler:
			h.fileTransferService = service
		case *FileChunkHandler:
			h.fileTransferService = service
		case *FileAckHandler:
			h.fileTransferService = service
		case *FileCompleteHandler:
			h.fileTransferService = service
		case *FileNackHandler:
			h.fileTransferService = service
		case *FileDataHandler:
			h.fileTransferService = service
		}
	}
	
	fta.logger.Debugf("[FileTransferAdapter] 文件传输服务已更新")
}

// GetHandlers 获取所有处理器
func (fta *FileTransferAdapter) GetHandlers() map[string]MessageHandlerInterface {
	fta.mu.RLock()
	defer fta.mu.RUnlock()
	
	result := make(map[string]MessageHandlerInterface)
	for k, v := range fta.handlers {
		result[k] = v
	}
	return result
}

// GetHandler 获取指定类型的处理器
func (fta *FileTransferAdapter) GetHandler(msgType string) MessageHandlerInterface {
	fta.mu.RLock()
	defer fta.mu.RUnlock()
	return fta.handlers[msgType]
}

// RegisterHandler 注册自定义处理器
func (fta *FileTransferAdapter) RegisterHandler(msgType string, handler MessageHandlerInterface) error {
	fta.mu.Lock()
	defer fta.mu.Unlock()
	
	fta.handlers[msgType] = handler
	
	// 如果已初始化，立即注册到PineconeService
	if fta.initialized {
		err := handler.Initialize()
		if err != nil {
			return err
		}
		
		// 通过SetMessageHandler注册处理器
		fta.pineconeService.SetMessageHandler(handler)
	}
	
	fta.logger.Debugf("[FileTransferAdapter] 已注册自定义处理器: Type=%s", msgType)
	return nil
}

// UnregisterHandler 注销处理器
func (fta *FileTransferAdapter) UnregisterHandler(msgType string) error {
	fta.mu.Lock()
	defer fta.mu.Unlock()
	
	if !fta.initialized {
		return fmt.Errorf("文件传输适配器未初始化")
	}
	
	handler, exists := fta.handlers[msgType]
	if !exists {
		return fmt.Errorf("处理器不存在: %s", msgType)
	}
	
	// 从PineconeService注销处理器（跳过，因为没有相应方法）
	fta.logger.Debugf("[FileTransferAdapter] 跳过从PineconeService注销处理器: Type=%s", msgType)
	
	if cleanupErr := handler.Cleanup(); cleanupErr != nil {
		fta.logger.Warnf("[FileTransferAdapter] 清理处理器失败: Type=%s, Error=%v", msgType, cleanupErr)
	}
	
	delete(fta.handlers, msgType)
	fta.logger.Infof("[FileTransferAdapter] 处理器已注销: Type=%s", msgType)
	return nil
}

// GetStats 获取适配器统计信息
func (fta *FileTransferAdapter) GetStats() map[string]interface{} {
	fta.mu.RLock()
	defer fta.mu.RUnlock()
	
	stats := map[string]interface{}{
		"initialized":    fta.initialized,
		"handler_count":  len(fta.handlers),
		"handler_types":  make([]string, 0, len(fta.handlers)),
	}
	
	for msgType := range fta.handlers {
		stats["handler_types"] = append(stats["handler_types"].([]string), msgType)
	}
	
	// 添加文件传输服务统计
	if fta.fileTransferService != nil {
		stats["service_running"] = fta.fileTransferService.IsRunning()
		stats["transfer_sessions"] = len(fta.fileTransferService.GetTransferSessions())
		if config := fta.fileTransferService.GetConfig(); config != nil {
			stats["config"] = map[string]interface{}{
				"chunk_size":     config.ChunkSize,
				"max_concurrent": config.MaxConcurrent,
				"timeout":        config.Timeout.String(),
				"retry_count":    config.RetryCount,
			}
		}
	} else {
		stats["service_running"] = false
		stats["transfer_sessions"] = 0
	}
	
	return stats
}