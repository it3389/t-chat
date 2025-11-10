package network

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"t-chat/internal/file"
)

// PendingTransfer 待处理的传输请求
type PendingTransfer struct {
	FileID      string
	SenderID    string
	ReceiverID  string
	FileName    string
	FileSize    int64
	FilePath    string
	RequestTime time.Time
	Timeout     time.Duration
	CancelChan  chan struct{}
	mu          sync.RWMutex
}

// FileTransferService 文件传输统一服务
// 集成文件管理、会话管理、协议转换和网络通信
type FileTransferService struct {
	fileManager     file.FileManagerInterface
	sessionManager  *FileTransferSessionManager
	protocolAdapter *FileProtocolAdapter
	pineconeService PineconeServiceInterface
	logger          Logger
	config          *FileTransferConfig
	mu              sync.RWMutex
	running         bool
	retryManager    *RetryManager
	errorHandler    *ErrorHandler
	confirmationHandler *UserConfirmationHandler
	stateManager    *TransferStateManager
	pendingTransfers map[string]*PendingTransfer
}

// FileTransferConfig 文件传输配置
type FileTransferConfig struct {
	DataDir         string        `json:"data_dir"`         // 数据目录
	TempDir         string        `json:"temp_dir"`         // 临时目录
	ChunkSize       int           `json:"chunk_size"`       // 分片大小
	MaxConcurrent   int           `json:"max_concurrent"`   // 最大并发传输数
	Timeout         time.Duration `json:"timeout"`          // 超时时间
	RetryCount      int           `json:"retry_count"`      // 重试次数
	MaxFileSize     int64         `json:"max_file_size"`    // 最大文件大小
	EnableEncryption bool         `json:"enable_encryption"` // 是否启用加密
	RetryDelay      time.Duration `json:"retry_delay"`      // 重试延迟
	MaxRetryDelay   time.Duration `json:"max_retry_delay"`  // 最大重试延迟
	BackoffFactor   float64       `json:"backoff_factor"`   // 退避因子
	ConfirmationTimeout time.Duration `json:"confirmation_timeout"` // 用户确认超时时间
}

// DefaultFileTransferConfig 默认配置
func DefaultFileTransferConfig() *FileTransferConfig {
	return &FileTransferConfig{
		DataDir:         "./data/files",
		TempDir:         "./data/temp",
		ChunkSize:       17 * 1024, // 17KB - 最大安全分片大小，确保Base64编码后不超过32KB
		MaxConcurrent:   1, // 单线程发送，避免Pinecone网络过载
		Timeout:         120 * time.Second, // 增加超时时间适应低速传输
		RetryCount:      5, // 适度重试次数
		MaxFileSize:     100 * 1024 * 1024, // 100MB - 降低文件大小限制
		EnableEncryption: false,
		RetryDelay:      5 * time.Second, // 增加重试延迟
		MaxRetryDelay:   60 * time.Second, // 适度最大重试延迟
		BackoffFactor:   2.0, // 标准退避因子
		ConfirmationTimeout: 180 * time.Second, // 用户确认超时时间
	}
}

// FileTransferSessionManager 文件传输会话管理器
type FileTransferSessionManager struct {
	sessions map[string]*file.TransferSession
	mu       sync.RWMutex
	logger   Logger
}

// NewFileTransferSessionManager 创建会话管理器
func NewFileTransferSessionManager(logger Logger) *FileTransferSessionManager {
	return &FileTransferSessionManager{
		sessions: make(map[string]*file.TransferSession),
		logger:   logger,
	}
}

// CreateSession 创建传输会话
func (sm *FileTransferSessionManager) CreateSession(fileID string) *file.TransferSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	windowConfig := &file.WindowConfig{
		WindowSize: 10,
		Timeout:    30 * time.Second,
	}
	
	session := file.NewTransferSession(fileID, windowConfig, sm.logger)
	sm.sessions[fileID] = session
	sm.logger.Debugf("[SessionManager] 创建传输会话: FileID=%s", fileID)
	return session
}

// GetSession 获取传输会话
func (sm *FileTransferSessionManager) GetSession(fileID string) *file.TransferSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[fileID]
}

// RemoveSession 移除传输会话
func (sm *FileTransferSessionManager) RemoveSession(fileID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, fileID)
	sm.logger.Debugf("[SessionManager] 移除传输会话: FileID=%s", fileID)
}

// GetAllSessions 获取所有会话
func (sm *FileTransferSessionManager) GetAllSessions() map[string]*file.TransferSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	result := make(map[string]*file.TransferSession)
	for k, v := range sm.sessions {
		result[k] = v
	}
	return result
}

// NewFileTransferService 创建文件传输服务
func NewFileTransferService(pineconeService PineconeServiceInterface, logger Logger, config *FileTransferConfig) *FileTransferService {
	if config == nil {
		config = DefaultFileTransferConfig()
	}
	
	// 创建文件管理器（这里需要实现一个简单的文件管理器）
	fileManager := NewSimpleFileManager(config.DataDir, config.TempDir, logger)
	
	return &FileTransferService{
		fileManager:     fileManager,
		sessionManager:  NewFileTransferSessionManager(logger),
		protocolAdapter: NewFileProtocolAdapter(logger),
		pineconeService: pineconeService,
		logger:          logger,
		config:          config,
		running:         false,
		retryManager:    NewRetryManager(config, logger),
		errorHandler:    NewErrorHandler(logger),
		confirmationHandler: NewUserConfirmationHandler(nil, logger),
		stateManager:    NewTransferStateManager(logger),
		pendingTransfers: make(map[string]*PendingTransfer),
	}
}

// Start 启动文件传输服务
func (fts *FileTransferService) Start(ctx context.Context) error {
	fts.mu.Lock()
	defer fts.mu.Unlock()
	
	if fts.running {
		return fmt.Errorf("文件传输服务已经在运行")
	}
	
	fts.running = true
	
	// 启动超时监控器
	go fts.startTimeoutMonitor(ctx)
	
	// 启动状态管理器的定期清理
	go fts.stateManager.StartCleanup(ctx)
	
	fts.logger.Infof("[FileTransferService] 文件传输服务已启动")
	return nil
}

// calculateFileChecksum 计算文件的SHA256校验和
func (fts *FileTransferService) calculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("读取文件失败: %v", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Stop 停止文件传输服务
func (fts *FileTransferService) Stop() error {
	fts.mu.Lock()
	defer fts.mu.Unlock()
	
	if !fts.running {
		return fmt.Errorf("文件传输服务未在运行")
	}
	
	fts.running = false
	fts.logger.Infof("[FileTransferService] 文件传输服务已停止")
	return nil
}

// IsRunning 检查服务是否运行
func (fts *FileTransferService) IsRunning() bool {
	fts.mu.RLock()
	defer fts.mu.RUnlock()
	return fts.running
}

// SendFile 发送文件
func (fts *FileTransferService) SendFile(filePath, toAddr string) error {
	if !fts.IsRunning() {
		return fmt.Errorf("文件传输服务未运行")
	}
	
	// 生成文件ID
	fileID := fmt.Sprintf("file_%d", time.Now().UnixNano())
	fileName := filepath.Base(filePath)
	
	fts.logger.Infof("[FileTransferService] 开始发送文件: %s -> %s (FileID: %s)", filePath, toAddr, fileID)
	
	// 创建传输会话
	session := fts.sessionManager.CreateSession(fileID)
	session.Start()
	
	// 读取文件并分片
	chunks, err := file.SplitFile(filePath, fts.config.ChunkSize)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 文件分片失败: %v", err)
		session.SetFailed()
		return err
	}
	
	// 计算文件大小和校验和
	var fileSize int64
	for _, chunk := range chunks {
		fileSize += int64(chunk.Size)
	}
	
	// 计算完整文件的校验和
	fileChecksum, err := fts.calculateFileChecksum(filePath)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 计算文件校验和失败: %v", err)
		session.SetFailed()
		return err
	}
	
	fts.logger.Debugf("[FileTransferService] 文件校验和计算完成: FileID=%s, Checksum=%s", fileID, fileChecksum)
	
	// 创建状态管理
	fts.stateManager.CreateTransfer(fileID, fts.pineconeService.GetPineconeAddr(), toAddr, fileName, fileSize)
	fts.stateManager.SetTransferState(fileID, TransferStatePending)
	
	// 创建文件传输请求
	transferReq := &file.TransferRequest{
		FileID:      fileID,
		FileName:    fileName,
		FileSize:    fileSize,
		ChunkSize:   fts.config.ChunkSize,
		ChunkCount:  len(chunks),
		Checksum:    fileChecksum,
		Encrypted:   fts.config.EnableEncryption,
		RequestTime: time.Now().Unix(),
		ResumePoint: 0,
	}
	
	// 发送文件传输请求
	requestPacket := fts.protocolAdapter.CreateMessagePacketFromFileRequest(
		transferReq,
		fts.pineconeService.GetPublicKeyHex(),
		toAddr,
	)
	
	err = fts.pineconeService.SendMessagePacket(toAddr, requestPacket)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 发送文件请求失败: %v", err)
		session.SetFailed()
		fts.stateManager.SetTransferState(fileID, TransferStateFailed)
		return err
	}
	
	fts.logger.Infof("[FileTransferService] 文件请求已发送，等待接收方确认: FileID=%s, ChunkCount=%d", fileID, len(chunks))
	
	// 等待接收方确认后再发送分片
	go fts.waitForConfirmationAndSend(fileID, chunks, fileName, toAddr)
	
	return nil
}

// waitForConfirmationAndSend 等待确认后发送文件分片
func (fts *FileTransferService) waitForConfirmationAndSend(fileID string, chunks []*file.Chunk, fileName, toAddr string) {
	session := fts.sessionManager.GetSession(fileID)
	if session == nil {
		fts.logger.Errorf("[FileTransferService] 找不到传输会话: FileID=%s", fileID)
		return
	}

	// 等待确认
	ctx, cancel := context.WithTimeout(context.Background(), fts.config.ConfirmationTimeout)
	defer cancel()

	fts.waitForConfirmation(ctx, session, fileID)

	// 如果确认成功，开始发送分片
	if session.IsAccepted() {
		fts.sendFileChunks(fileID, chunks, fileName, toAddr)
	}
}

// sendFileChunks 发送文件分片
func (fts *FileTransferService) sendFileChunks(fileID string, chunks []*file.Chunk, fileName, toAddr string) {
	session := fts.sessionManager.GetSession(fileID)
	if session == nil {
		fts.logger.Errorf("[FileTransferService] 找不到传输会话: FileID=%s", fileID)
		return
	}

	session.SetRunning()
	total := len(chunks)
	// 初始化Progress的total值
	session.UpdateProgress(0, total)

	// Pinecone网络速率控制参数
	const (
		pineconeChunkInterval = 200 * time.Millisecond // 每个分片间隔200ms
		pineconeBatchSize = 5 // 每批发送5个分片后暂停
		pineconeBatchInterval = 2 * time.Second // 每批间隔2秒
	)

	for i, chunk := range chunks {
		// Pinecone网络速率控制：每个分片发送前等待
		if i > 0 {
			time.Sleep(pineconeChunkInterval)
		}
		
		// Pinecone网络批量控制：每批分片后暂停
		if i > 0 && i%pineconeBatchSize == 0 {
			fts.logger.Debugf("[FileTransferService] Pinecone网络批量暂停: 已发送%d个分片，暂停%v", i, pineconeBatchInterval)
			time.Sleep(pineconeBatchInterval)
		}
		
		// 使用重试机制发送分片
		err := fts.sendChunkWithRetry(chunk, fileID, fileName, toAddr, total, i)
		if err != nil {
			fts.logger.Errorf("[FileTransferService] 发送分片失败: FileID=%s, ChunkIndex=%d, Error=%v", fileID, i, err)
			
			// 处理错误
			fts.errorHandler.HandleError(err, fmt.Sprintf("send_chunk_%d", i), map[string]interface{}{
				"file_id": fileID,
				"chunk_index": i,
				"to_addr": toAddr,
			})
			
			// 如果是严重错误，停止传输
			session.SetFailed()
			fts.retryManager.RemoveRetry(fileID)
			return
			
			// 更新会话进度（即使失败也要记录）
			session.UpdateProgress(i, total)
			continue
		}
		
		// 更新会话进度
		session.UpdateProgress(i+1, total)
		fts.logger.Debugf("[FileTransferService] 分片发送成功: FileID=%s, ChunkIndex=%d/%d", fileID, i+1, total)
	}

	// 发送完成消息
	completeMsg := &file.TransferComplete{
		FileID: fileID,
		Time:   time.Now().Unix(),
	}
	completePacket := fts.protocolAdapter.CreateMessagePacketFromFileComplete(
		completeMsg,
		fts.pineconeService.GetPineconeAddr(),
		toAddr,
	)

	err := fts.pineconeService.SendMessagePacket(toAddr, completePacket)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 发送完成消息失败: %v", err)
		session.SetFailed()
	} else {
		session.SetCompleted()
		fts.logger.Debugf("[FileTransferService] 文件传输完成: FileID=%s", fileID)
	}

	// 清理重试信息
	fts.retryManager.RemoveRetry(fileID)
}

// sendChunkWithRetry 使用重试机制发送分片
func (fts *FileTransferService) sendChunkWithRetry(chunk *file.Chunk, fileID, fileName, toAddr string, total, index int) error {
	operation := fmt.Sprintf("send_chunk_%d", index)
	maxRetries := 3 // 适度重试次数，避免过度重试
	
	return fts.errorHandler.RetryWithPolicy(func() error {
		// 创建分片消息包
		chunkPacket := fts.protocolAdapter.CreateMessagePacketFromFileChunk(
			chunk,
			fileID,
			fileName,
			fts.pineconeService.GetPineconeAddr(),
			toAddr,
			total,
		)
		
		// 发送分片前的Pinecone网络适配延迟
		time.Sleep(100 * time.Millisecond)
		
		// 发送分片
		err := fts.pineconeService.SendMessagePacket(toAddr, chunkPacket)
		if err != nil {
			// 检查是否是连接相关错误
			errorStr := strings.ToLower(err.Error())
			isConnectionError := strings.Contains(errorStr, "eof") || 
							   strings.Contains(errorStr, "connection") ||
							   strings.Contains(errorStr, "broken pipe") ||
							   strings.Contains(errorStr, "forcibly closed") ||
							   strings.Contains(errorStr, "network is unreachable") ||
							   strings.Contains(errorStr, "no route to host")
			
			if isConnectionError {
				fts.logger.Debugf("检测到连接错误，触发路由重新发现: %v", err)
				// 触发路由重新发现
				fts.pineconeService.TriggerRouteRediscovery(toAddr)
				// Pinecone网络重连等待时间
				time.Sleep(8 * time.Second)
			} else {
				// 非连接错误，短暂等待后重试
				time.Sleep(1 * time.Second)
			}
			return fmt.Errorf("发送分片失败: %w", err)
		}
		
		return nil
	}, operation, map[string]interface{}{
		"file_id": fileID,
		"chunk_index": index,
		"to_addr": toAddr,
		"max_retries": maxRetries,
	})
}

// sendFileNack 发送文件传输拒绝消息
func (fts *FileTransferService) sendFileNack(toAddr, fileID, reason string) {
	nackMsg := &Message{
		Type:       MessageTypeFileNack,
		From:       fts.pineconeService.GetNodeID(),
		To:         toAddr,
		FileID:     fileID,
		ChunkIndex: -1, // -1表示文件请求响应
		Data:       map[string]interface{}{"reason": reason},
		Timestamp:  time.Now(),
		Priority:   MessagePriorityHigh,
	}
	
	err := fts.pineconeService.SendMessage(toAddr, nackMsg)
	if err != nil {
		fts.logger.Errorf("Failed to send NACK: %v, fileID: %s", err, fileID)
	}
}

// removePendingTransfer 移除待处理传输
func (fts *FileTransferService) removePendingTransfer(fileID string) {
	fts.mu.Lock()
	defer fts.mu.Unlock()
	delete(fts.pendingTransfers, fileID)
}

// waitForConfirmation 等待接收方确认
func (fts *FileTransferService) waitForConfirmation(ctx context.Context, session *file.TransferSession, fileID string) {
	// 创建超时上下文
	confirmCtx, cancel := context.WithTimeout(ctx, fts.config.ConfirmationTimeout)
	defer cancel()

	// 等待确认或超时
	select {
	case <-confirmCtx.Done():
		// 超时处理
		fts.logger.Errorf("File transfer confirmation timeout, fileID: %s", fileID)
		fts.stateManager.SetTransferState(fileID, TransferStateFailed)
		fts.stateManager.SetTransferError(fileID, "confirmation timeout")
		session.SetFailed()
		return
	case <-session.ConfirmationChan:
		// 收到确认，检查是否被接受
		if session.IsAccepted() {
			fts.logger.Infof("File transfer confirmed, starting to send chunks, fileID: %s", fileID)
			fts.stateManager.SetTransferState(fileID, TransferStateTransferring)
			// 开始发送文件分片
			// 注意：这里需要从上下文中获取chunks、fileName和toAddr信息
			// 暂时注释掉，需要重构此方法
			// go fts.sendFileChunks(fileID, chunks, fileName, toAddr)
		} else {
			// 被拒绝
			fts.logger.Infof("File transfer rejected by receiver, fileID: %s", fileID)
			fts.stateManager.SetTransferState(fileID, TransferStateRejected)
			fts.stateManager.SetTransferError(fileID, "rejected by receiver")
			session.SetFailed()
		}
	case <-session.CancelChan:
		// 传输被取消
		fts.logger.Infof("File transfer cancelled, fileID: %s", fileID)
		fts.stateManager.SetTransferState(fileID, TransferStateCancelled)
		session.SetCanceled()
	}
}

// CancelTransfer 取消文件传输
func (fts *FileTransferService) CancelTransfer(fileID string) error {
	fts.logger.Infof("Cancelling file transfer, fileID: %s", fileID)

	// 获取会话
	session := fts.sessionManager.GetSession(fileID)
	if session == nil {
		return fmt.Errorf("session not found for fileID: %s", fileID)
	}

	// 获取待处理传输信息
	fts.mu.RLock()
	pendingTransfer := fts.pendingTransfers[fileID]
	fts.mu.RUnlock()

	// 更新状态
	fts.stateManager.SetTransferState(fileID, TransferStateCancelled)

	// 发送取消消息给对方（如果有待处理传输信息）
	if pendingTransfer != nil {
		cancelMsg := &Message{
			Type:      MessageTypeFileCancel,
			From:      fts.pineconeService.GetNodeID(),
			To:        pendingTransfer.ReceiverID,
			FileID:    fileID,
			Data:      map[string]interface{}{"reason": "transfer cancelled by user"},
			Timestamp: time.Now(),
			Priority:  MessagePriorityHigh,
		}

		if err := fts.pineconeService.SendMessage(pendingTransfer.ReceiverID, cancelMsg); err != nil {
			fts.logger.Errorf("Failed to send cancel message: %v, fileID: %s", err, fileID)
		}
	}

	// 取消会话
	session.Cancel()

	// 清理待处理传输
	fts.removePendingTransfer(fileID)

	return nil
}

// HandleFileCancel 处理文件传输取消消息
func (fts *FileTransferService) HandleFileCancel(msg *Message) error {
	fts.logger.Infof("Handling file cancel from: %s, fileID: %s", msg.From, msg.FileID)

	// 获取会话
	session := fts.sessionManager.GetSession(msg.FileID)
	if session == nil {
		fts.logger.Warnf("Session not found for cancel, fileID: %s", msg.FileID)
		return nil
	}

	// 解析取消原因
	reason := "unknown"
	if reasonData, ok := msg.Data["reason"]; ok {
		if reasonStr, ok := reasonData.(string); ok {
			reason = reasonStr
		}
	}
	fts.logger.Infof("File transfer cancelled by peer, fileID: %s, reason: %s", msg.FileID, reason)

	// 更新状态
	fts.stateManager.SetTransferState(msg.FileID, TransferStateCancelled)
	fts.stateManager.SetTransferError(msg.FileID, reason)

	// 取消会话
	session.Cancel()

	// 清理待处理传输
	fts.removePendingTransfer(msg.FileID)

	return nil
}

// SetUserConfirmationCallback 设置用户确认回调
func (fts *FileTransferService) SetUserConfirmationCallback(callback FileTransferConfirmationCallback) {
	fts.confirmationHandler.SetCallback(callback)
}

// GetTransferState 获取传输状态
func (fts *FileTransferService) GetTransferState(fileID string) (*TransferStateInfo, bool) {
	return fts.stateManager.GetTransferState(fileID)
}

// GetAllTransfers 获取所有传输状态
func (fts *FileTransferService) GetAllTransfers() map[string]*TransferStateInfo {
	return fts.stateManager.GetAllTransfers()
}

// GetTransferStats 获取传输统计信息
func (fts *FileTransferService) GetTransferStats() map[string]int {
	return fts.stateManager.GetTransferStats()
}

// startTimeoutMonitor 启动超时监控
func (fts *FileTransferService) startTimeoutMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // 每30秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fts.checkTimeouts()
			fts.retryFailedTransfers()
		}
	}
}

// checkTimeouts 检查超时的传输
func (fts *FileTransferService) checkTimeouts() {
	fts.mu.Lock()
	defer fts.mu.Unlock()

	now := time.Now()
	for fileID, pending := range fts.pendingTransfers {
		if now.Sub(pending.RequestTime) > pending.Timeout {
			fts.logger.Warnf("Transfer request timeout, fileID: %s, elapsed: %v", fileID, now.Sub(pending.RequestTime))
			
			// 更新状态为超时
			fts.stateManager.SetTransferState(fileID, TransferStateFailed)
			fts.stateManager.SetTransferError(fileID, "request timeout")
			
			// 取消待处理传输
			close(pending.CancelChan)
			delete(fts.pendingTransfers, fileID)
			
			// 如果有会话，也要取消
			if session := fts.sessionManager.GetSession(fileID); session != nil {
				session.Cancel()
			}
		}
	}
}

// retryFailedTransfers 重试失败的传输
func (fts *FileTransferService) retryFailedTransfers() {
	allTransfers := fts.stateManager.GetAllTransfers()
	
	for fileID, state := range allTransfers {
		// 只重试失败的传输，且重试次数未超限
		if state.State == TransferStateFailed && state.RetryCount < fts.config.RetryCount {
			// 检查是否到了重试时间
			if time.Since(state.LastRetryTime) >= fts.calculateRetryDelay(state.RetryCount) {
				fts.logger.Infof("Retrying failed transfer, fileID: %s, retryCount: %d", fileID, state.RetryCount+1)
				
				// 更新重试信息
				fts.stateManager.IncrementRetryCount(fileID)
				
				// 根据传输方向决定重试策略
				if state.SenderID == fts.pineconeService.GetNodeID() {
					// 发送方重试：重新发送文件请求
					go fts.retrySendFile(fileID, state)
				} else {
					// 接收方重试：重新请求确认
					go fts.retryReceiveFile(fileID, state)
				}
			}
		}
	}
}

// calculateRetryDelay 计算重试延迟
func (fts *FileTransferService) calculateRetryDelay(retryCount int) time.Duration {
	delay := fts.config.RetryDelay
	for i := 0; i < retryCount; i++ {
		delay = time.Duration(float64(delay) * fts.config.BackoffFactor)
		if delay > fts.config.MaxRetryDelay {
			delay = fts.config.MaxRetryDelay
			break
		}
	}
	return delay
}

// retrySendFile 重试发送文件
func (fts *FileTransferService) retrySendFile(fileID string, state *TransferStateInfo) {
	// 重新设置为等待状态
	fts.stateManager.SetTransferState(fileID, TransferStatePending)
	
	// 重新发送文件请求
	reqMsg := &Message{
		Type:      MessageTypeFileRequest,
		From:      fts.pineconeService.GetNodeID(),
		To:        state.ReceiverID,
		FileID:    fileID,
		Data:      map[string]interface{}{"file_info": fmt.Sprintf("%s:%d", state.FileName, state.FileSize)},
		Timestamp: time.Now(),
		Priority:  MessagePriorityHigh,
	}
	
	if err := fts.pineconeService.SendMessage(state.ReceiverID, reqMsg); err != nil {
		fts.logger.Errorf("Failed to retry send file request: %v, fileID: %s", err, fileID)
		fts.stateManager.SetTransferState(fileID, TransferStateFailed)
		fts.stateManager.SetTransferError(fileID, fmt.Sprintf("retry failed: %v", err))
	} else {
		fts.logger.Infof("File request retry sent, fileID: %s", fileID)
	}
}

// retryReceiveFile 重试接收文件
func (fts *FileTransferService) retryReceiveFile(fileID string, state *TransferStateInfo) {
	// 接收方重试逻辑：重新请求用户确认或恢复接收
	fts.logger.Infof("Retrying receive file, fileID: %s", fileID)
	
	// 检查是否有会话存在
		if session := fts.sessionManager.GetSession(fileID); session != nil {
			// 重置会话状态
			session.Reset()
			fts.stateManager.SetTransferState(fileID, TransferStateTransferring)
		} else {
			// 会话不存在，标记为失败
			fts.stateManager.SetTransferState(fileID, TransferStateFailed)
			fts.stateManager.SetTransferError(fileID, "session not found for retry")
		}
}

// HandleFileRequest 处理文件传输请求
func (fts *FileTransferService) HandleFileRequest(msg *Message) error {
	transferReq, err := fts.protocolAdapter.ExtractFileRequestFromMessage(msg)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 解析文件请求失败: %v", err)
		return err
	}
	
	fts.logger.Infof("[FileTransferService] 收到文件传输请求: FileID=%s, FileName=%s, Size=%d",
		transferReq.FileID, transferReq.FileName, transferReq.FileSize)
	
	// 检查确认处理器是否已设置回调
	if fts.confirmationHandler == nil {
		fts.logger.Errorf("[FileTransferService] 确认处理器未初始化")
		return fmt.Errorf("confirmation handler not initialized")
	}
	
	// 创建待处理传输请求
	pendingTransfer := &PendingTransfer{
		FileID:      transferReq.FileID,
		SenderID:    msg.From,
		ReceiverID:  fts.pineconeService.GetPineconeAddr(),
		FileName:    transferReq.FileName,
		FileSize:    transferReq.FileSize,
		RequestTime: time.Now(),
		Timeout:     fts.config.Timeout,
		CancelChan:  make(chan struct{}),
	}
	
	// 添加到待处理列表
	fts.mu.Lock()
	fts.pendingTransfers[transferReq.FileID] = pendingTransfer
	fts.mu.Unlock()
	
	// 创建用户确认请求
	transferRequest := &FileTransferRequest{
		FileID:     transferReq.FileID,
		SenderID:   msg.From,
		ReceiverID: fts.pineconeService.GetPineconeAddr(),
		FileName:   transferReq.FileName,
		FileSize:   transferReq.FileSize,
		FilePath:   "", // 接收方暂时不知道保存路径
	}
	
	// 请求用户确认
	go func() {
		// 再次检查确认处理器的回调是否已设置
		if fts.confirmationHandler.GetCallback() == nil {
			fts.logger.Errorf("[FileTransferService] 用户确认回调未设置，自动拒绝文件传输")
			fts.sendFileNack(msg.From, transferReq.FileID, "no_confirmation_callback")
			fts.removePendingTransfer(transferReq.FileID)
			return
		}
		
		response, err := fts.confirmationHandler.RequestUserConfirmation(transferRequest)
			if err != nil {
				fts.logger.Errorf("[FileTransferService] 用户确认失败: %v", err)
				fts.sendFileNack(msg.From, transferReq.FileID, "confirmation_timeout")
				fts.removePendingTransfer(transferReq.FileID)
				return
			}
		
		if response.Accepted {
			// 用户接受，创建接收会话
			session := fts.sessionManager.CreateSession(transferReq.FileID)
			
			// 计算总分片数
			totalChunks := int((transferReq.FileSize + int64(fts.config.ChunkSize) - 1) / int64(fts.config.ChunkSize))
			
			// 初始化进度，设置总分片数
			session.UpdateProgress(0, totalChunks)
			session.Start()
			
			// 更新状态管理器
			fts.stateManager.CreateTransfer(transferReq.FileID, msg.From, fts.pineconeService.GetPineconeAddr(), transferReq.FileName, transferReq.FileSize)
			fts.stateManager.SetTransferState(transferReq.FileID, TransferStateAccepted)
			
			fts.logger.Infof("[FileTransferService] 用户接受文件传输: FileID=%s", transferReq.FileID)
			
			// 发送ACK确认
			ackMsg := &file.ChunkAck{
				FileID:     transferReq.FileID,
				ChunkIndex: -1, // -1表示确认文件请求
				Success:    true,
				Message:    "文件请求已接受",
				Timestamp:  time.Now().Unix(),
			}
			
			ackPacket := fts.protocolAdapter.CreateMessagePacketFromFileAck(
				ackMsg,
				fts.pineconeService.GetPublicKeyHex(),
				msg.From,
			)
			
			err = fts.pineconeService.SendMessagePacket(msg.From, ackPacket)
			if err != nil {
				fts.logger.Errorf("[FileTransferService] 发送ACK失败: %v", err)
				session.SetFailed()
			}
		} else {
			// 用户拒绝，发送NACK
			fts.logger.Infof("[FileTransferService] 用户拒绝文件传输: FileID=%s, Reason=%s", transferReq.FileID, response.Reason)
			fts.sendFileNack(msg.From, transferReq.FileID, response.Reason)
		}
		
		// 清理待处理传输
		fts.removePendingTransfer(transferReq.FileID)
	}()
	
	return nil
}

// HandleFileChunk 处理文件分片
func (fts *FileTransferService) HandleFileChunk(msg *Message) error {
	chunk, fileID, err := fts.protocolAdapter.ExtractFileChunkFromMessage(msg)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 解析文件分片失败: %v", err)
		return err
	}
	
	fts.logger.Debugf("[FileTransferService] 收到文件分片: FileID=%s, Index=%d, Size=%d",
		fileID, chunk.Index, chunk.Size)
	
	// 获取会话
	session := fts.sessionManager.GetSession(fileID)
	if session == nil {
		fts.logger.Errorf("[FileTransferService] 找不到传输会话: FileID=%s", fileID)
		return fmt.Errorf("找不到传输会话: %s", fileID)
	}
	
	session.SetRunning()
	
	// 验证分片校验和
	if !file.VerifyChunk(chunk) {
		fts.logger.Errorf("[FileTransferService] 分片校验和验证失败: FileID=%s, Index=%d", fileID, chunk.Index)
		session.SetFailed()
		
		// 发送分片NACK
		nackMsg := &file.ChunkAck{
			FileID:     fileID,
			ChunkIndex: chunk.Index,
			Success:    false,
			Message:    "分片校验和验证失败",
			Timestamp:  time.Now().Unix(),
		}
		
		nackPacket := fts.protocolAdapter.CreateMessagePacketFromFileAck(
			nackMsg,
			fts.pineconeService.GetPineconeAddr(),
			msg.From,
		)
		
		fts.pineconeService.SendMessagePacket(msg.From, nackPacket)
		return fmt.Errorf("分片校验和验证失败: FileID=%s, Index=%d", fileID, chunk.Index)
	}
	
	fts.logger.Debugf("[FileTransferService] 分片校验和验证成功: FileID=%s, Index=%d", fileID, chunk.Index)
	
	// 保存分片（这里简化处理）
	// 将哈希字节数组转换为十六进制字符串
	var hashStr string
	if len(chunk.Hash) > 0 {
		hashStr = hex.EncodeToString(chunk.Hash)
	}
	err = fts.fileManager.SaveFileChunk(fileID, chunk.Index, chunk.Data, hashStr)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 保存分片失败: FileID=%s, Index=%d, Error=%v",
			fileID, chunk.Index, err)
		session.SetFailed()
		return err
	}
	
	// 更新传输进度
	if simpleFileManager, ok := fts.fileManager.(*SimpleFileManager); ok {
		received, total := simpleFileManager.GetTransferProgress(fileID)
		session.UpdateProgress(received, total)
		fts.logger.Debugf("[FileTransferService] 进度更新: FileID=%s, 已接收=%d, 总计=%d", fileID, received, total)
	}
	
	// 发送分片ACK
	ackMsg := &file.ChunkAck{
		FileID:     fileID,
		ChunkIndex: chunk.Index,
		Success:    true,
		Message:    "分片接收成功",
		Timestamp:  time.Now().Unix(),
	}
	
	ackPacket := fts.protocolAdapter.CreateMessagePacketFromFileAck(
		ackMsg,
		fts.pineconeService.GetPineconeAddr(),
		msg.From,
	)
	
	err = fts.pineconeService.SendMessagePacket(msg.From, ackPacket)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 发送分片ACK失败: %v", err)
		return err
	}
	
	fts.logger.Debugf("[FileTransferService] 分片ACK已发送: FileID=%s, Index=%d", fileID, chunk.Index)
	return nil
}

// HandleFileNack 处理文件NACK消息
func (fts *FileTransferService) HandleFileNack(msg *Message) error {
	fts.logger.Infof("Handling file NACK from: %s, fileID: %s", msg.From, msg.FileID)

	// 获取发送会话
	session := fts.sessionManager.GetSession(msg.FileID)
	if session == nil {
		fts.logger.Warnf("Session not found for NACK, fileID: %s", msg.FileID)
		return nil
	}

	// 解析拒绝原因
	reason := "unknown"
	if reasonData, ok := msg.Data["reason"]; ok {
		if reasonStr, ok := reasonData.(string); ok {
			reason = reasonStr
		}
	}
	fts.logger.Infof("File transfer rejected, fileID: %s, reason: %s", msg.FileID, reason)

	// 更新状态
	fts.stateManager.SetTransferState(msg.FileID, TransferStateRejected)
	fts.stateManager.SetTransferError(msg.FileID, reason)

	// 设置会话为拒绝状态
	session.SetRejected()

	// 通知等待确认的协程
	select {
	case session.ConfirmationChan <- false:
	default:
	}

	return nil
}

// HandleFileAck 处理文件确认
func (fts *FileTransferService) HandleFileAck(msg *Message) error {
	ackMsg, err := fts.protocolAdapter.ExtractFileAckFromMessage(msg)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 解析文件ACK失败: %v", err)
		return err
	}
	
	fts.logger.Debugf("[FileTransferService] 收到文件ACK: FileID=%s, Index=%d",
		ackMsg.FileID, ackMsg.ChunkIndex)
	
	// 获取发送会话
	session := fts.sessionManager.GetSession(ackMsg.FileID)
	if session == nil {
		fts.logger.Errorf("[FileTransferService] 找不到传输会话: FileID=%s", ackMsg.FileID)
		return nil
	}
	
	// 处理文件请求确认 (ChunkIndex == -1)
	if ackMsg.ChunkIndex == -1 {
		if ackMsg.Success {
			// 接收方接受文件传输
			fts.logger.Infof("[FileTransferService] 文件传输被接收方接受: FileID=%s", ackMsg.FileID)
			session.SetAccepted()
			// 更新状态管理器
			fts.stateManager.SetTransferState(ackMsg.FileID, TransferStateTransferring)
			// 通知等待确认的协程
			select {
			case session.ConfirmationChan <- true:
			default:
			}
		} else {
			// 接收方拒绝文件传输
			fts.logger.Infof("[FileTransferService] 文件传输被接收方拒绝: FileID=%s, Reason=%s", ackMsg.FileID, ackMsg.Message)
			session.SetRejected()
			// 更新状态管理器
			fts.stateManager.SetTransferState(ackMsg.FileID, TransferStateRejected)
			fts.stateManager.SetTransferError(ackMsg.FileID, ackMsg.Message)
			// 通知等待确认的协程
			select {
			case session.ConfirmationChan <- false:
			default:
			}
		}
		return nil
	}
	
	// 处理分片ACK
	if ackMsg.Success {
		// 分片发送成功，更新发送窗口
		session.MarkChunkAcked(ackMsg.ChunkIndex)
		fts.logger.Debugf("[FileTransferService] 分片ACK已接收: FileID=%s, ChunkIndex=%d", ackMsg.FileID, ackMsg.ChunkIndex)
	} else {
		// 分片发送失败，标记需要重传
		session.MarkChunkFailed(ackMsg.ChunkIndex)
		fts.logger.Errorf("[FileTransferService] 分片发送失败，将重试: FileID=%s, ChunkIndex=%d, Reason=%s", ackMsg.FileID, ackMsg.ChunkIndex, ackMsg.Message)
	}
	
	return nil
}

// HandleFileComplete 处理文件传输完成
func (fts *FileTransferService) HandleFileComplete(msg *Message) error {
	completeMsg, err := fts.protocolAdapter.ExtractFileCompleteFromMessage(msg)
	if err != nil {
		fts.logger.Errorf("[FileTransferService] 解析文件完成消息失败: %v", err)
		return err
	}
	
	fts.logger.Infof("[FileTransferService] 文件传输完成: FileID=%s", completeMsg.FileID)
	
	// 获取会话并标记完成
	session := fts.sessionManager.GetSession(completeMsg.FileID)
	if session != nil {
		session.SetComplete()
	}
	
	// 清理会话
	fts.sessionManager.RemoveSession(completeMsg.FileID)
	
	return nil
}

// GetTransferSessions 获取所有传输会话
func (fts *FileTransferService) GetTransferSessions() map[string]*file.TransferSession {
	return fts.sessionManager.GetAllSessions()
}

// GetConfig 获取配置
func (fts *FileTransferService) GetConfig() *FileTransferConfig {
	return fts.config
}

// sendFileComplete 发送文件完成消息
func (fts *FileTransferService) sendFileComplete(toAddr, fileID string) error {
	return fts.errorHandler.RetryWithPolicy(func() error {
		completePacket := &MessagePacket{
			ID:        fmt.Sprintf("complete_%d", time.Now().UnixNano()),
			Type:      "file_complete",
			From:      fts.pineconeService.GetPublicKeyHex(),
			To:        toAddr,
			Timestamp: time.Now(),
			Content:   fmt.Sprintf(`{"file_id":"%s"}`, fileID),
		}

		err := fts.pineconeService.SendMessagePacket(toAddr, completePacket)
		if err != nil {
			// 检查是否是连接相关错误
			errorStr := strings.ToLower(err.Error())
			isConnectionError := strings.Contains(errorStr, "eof") || 
							   strings.Contains(errorStr, "connection") ||
							   strings.Contains(errorStr, "broken pipe") ||
							   strings.Contains(errorStr, "forcibly closed") ||
							   strings.Contains(errorStr, "network is unreachable") ||
							   strings.Contains(errorStr, "no route to host")
			
			if isConnectionError {
				fts.logger.Debugf("检测到连接错误，触发路由重新发现: %v", err)
				// 触发路由重新发现
				fts.pineconeService.TriggerRouteRediscovery(toAddr)
				// 等待一段时间让网络重新连接
				time.Sleep(3 * time.Second)
			}
			return fmt.Errorf("发送文件完成消息失败: %w", err)
		}
		return nil
	}, "sendFileComplete", map[string]interface{}{
		"toAddr": toAddr,
		"fileID": fileID,
	})
}