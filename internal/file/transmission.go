package file

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
	"t-chat/internal/performance"
)

// TransmissionManager 传输管理器，实现 TransmissionManagerInterface 接口
// 负责文件的发送、接收和传输会话管理
type TransmissionManager struct {
	logger          Logger                         // 日志记录器
	protocolHandler ProtocolHandlerInterface      // 协议处理器
	sessions        map[string]*TransferSession   // 传输会话映射
	mu              sync.RWMutex                  // 会话操作锁
}

// 确保 TransmissionManager 实现了 TransmissionManagerInterface 接口
var _ TransmissionManagerInterface = (*TransmissionManager)(nil)

// NewTransmissionManager 创建传输管理器
func NewTransmissionManager(logger Logger) *TransmissionManager {
	return &TransmissionManager{
		logger:          logger,
		protocolHandler: NewProtocolHandler(),
		sessions:        make(map[string]*TransferSession),
	}
}

// SendFile 发送文件（支持断点续传和滑动窗口）
func (tm *TransmissionManager) SendFile(conn net.Conn, filePath, toUser string, chunkSize int) error {
	// 读取文件信息
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %v", err)
	}

	// 生成文件ID
	fileID := tm.generateFileID(filePath, fileInfo.Size())

	// 自适应分片大小
	adaptiveChunkSize := tm.calculateAdaptiveChunkSize(fileInfo.Size(), chunkSize)

	// 计算分片信息
	totalChunks := int((fileInfo.Size() + int64(adaptiveChunkSize) - 1) / int64(adaptiveChunkSize))

	// 计算文件校验和
	checksum, err := tm.calculateChecksum(file)
	if err != nil {
		return fmt.Errorf("计算校验和失败 %v", err)
	}

	// 检查断点续传
	resumePoint := tm.checkResumePoint(fileID)

	// 创建传输请求
	request := &TransferRequest{
		FileID:      fileID,
		FileName:    filepath.Base(filePath),
		FileSize:    fileInfo.Size(),
		ChunkSize:   adaptiveChunkSize,
		ChunkCount:  totalChunks,
		Checksum:    checksum,
		Encrypted:   true,
		RequestTime: time.Now().Unix(),
		ResumePoint: resumePoint,
	}

	// 发送传输请求
	if err := tm.sendRequest(conn, request); err != nil {
		return fmt.Errorf("发送传输请求失败 %v", err)
	}

	// 等待响应
	response, err := tm.receiveResponse(conn)
	if err != nil {
		return fmt.Errorf("接收响应失败: %v", err)
	}

	if !response.Accepted {
		return fmt.Errorf("传输被拒绝 %s", response.Message)
	}

	// 创建分片管理器
	chunkManager := NewChunkManager(fileID, filePath, adaptiveChunkSize, totalChunks, tm.logger)

	// 设置断点续传位置
	if response.ResumePoint > 0 {
		chunkManager.SetResumePoint(response.ResumePoint)
	}

	// 创建传输会话
	windowConfig := NewWindowConfig()
	session := NewTransferSession(fileID, windowConfig, tm.logger)
	tm.sessions[fileID] = session

	// 开始传输
	return tm.transferChunksWithWindow(conn, chunkManager, session)
}

// ReceiveFile 接收文件（支持断点续传）
func (tm *TransmissionManager) ReceiveFile(conn net.Conn, savePath string) error {
    // 接收传输请求
    request, err := tm.receiveRequest(conn)
    if err != nil {
        return fmt.Errorf("接收传输请求失败: %v", err)
    }

	// 检查文件是否已存在
	if _, err := os.Stat(savePath); err == nil {
		// 文件已存在，检查是否需要断点续传
		return tm.handleResumeTransfer(conn, request, savePath)
	}

	// 创建保存目录
	saveDir := filepath.Dir(savePath)
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fmt.Errorf("创建保存目录失败: %v", err)
	}

	// 发送接受响应
	response := &TransferResponse{
		FileID:       request.FileID,
		Accepted:     true,
		Message:      "文件传输已接收",
		ChunkSize:    request.ChunkSize,
		SavePath:     savePath,
		ResponseTime: time.Now().Unix(),
		ResumePoint:  0,
	}

	if err := tm.sendResponse(conn, response); err != nil {
		return fmt.Errorf("发送响应失败 %v", err)
	}

    // 创建接收器
    receiver := NewReceiver(request.FileID, savePath, request.FileSize, request.ChunkSize, tm.logger)

    // 开始接收
    if err := tm.receiveChunksWithAck(conn, receiver, request.ChunkCount); err != nil {
        return err
    }

    // 分片接收完成后进行最终文件校验和验证
    file, err := os.Open(savePath)
    if err != nil {
        return fmt.Errorf("打开保存文件失败: %v", err)
    }
    defer file.Close()
    finalChecksum, err := tm.calculateChecksum(file)
    if err != nil {
        return fmt.Errorf("计算最终校验和失败: %v", err)
    }
    if finalChecksum != request.Checksum {
        return fmt.Errorf("文件校验和不匹配: 期望=%s, 实际=%s", request.Checksum, finalChecksum)
    }
    tm.logger.Infof("文件校验通过: fileID=%s, checksum=%s", request.FileID, finalChecksum)
    return nil
}

// handleResumeTransfer 处理断点续传
func (tm *TransmissionManager) handleResumeTransfer(conn net.Conn, request *TransferRequest, savePath string) error {
	// 检查已存在的文件大小
	fileInfo, err := os.Stat(savePath)
	if err != nil {
		return fmt.Errorf("检查文件状态失败 %v", err)
	}

	// 计算已接收的分片数
	receivedChunks := int(fileInfo.Size() / int64(request.ChunkSize))

	// 发送断点续传响应
	response := &TransferResponse{
		FileID:       request.FileID,
		Accepted:     true,
		Message:      fmt.Sprintf("断点续传，已接收 %d 个分片", receivedChunks),
		ChunkSize:    request.ChunkSize,
		SavePath:     savePath,
		ResponseTime: time.Now().Unix(),
		ResumePoint:  receivedChunks,
	}

	if err := tm.sendResponse(conn, response); err != nil {
		return fmt.Errorf("发送响应失败 %v", err)
	}

	// 创建接收器，从断点开始
	receiver := NewReceiver(request.FileID, savePath, request.FileSize, request.ChunkSize, tm.logger)
	receiver.SetResumePoint(receivedChunks)

	// 继续接收剩余分片
	return tm.receiveChunksWithAck(conn, receiver, request.ChunkCount-receivedChunks)
}

// transferChunksWithWindow 使用滑动窗口传输分片
func (tm *TransmissionManager) transferChunksWithWindow(conn net.Conn, chunkManager ChunkManagerInterface, session *TransferSession) error {
	helper := NewTransferHelper(tm.logger)
	return helper.SendChunksWithWindow(conn, chunkManager, session, tm)
}

// receiveChunksWithAck 接收分片并发送确认
func (tm *TransmissionManager) receiveChunksWithAck(conn net.Conn, receiver ReceiverInterface, totalChunks int) error {
	helper := NewTransferHelper(tm.logger)
	return helper.ReceiveChunksWithAck(conn, receiver, totalChunks, tm)
}

// calculateAdaptiveChunkSize 计算自适应分片大小
func (tm *TransmissionManager) calculateAdaptiveChunkSize(fileSize int64, baseChunkSize int) int {
	// 根据文件大小自适应调整分片大小，严格限制在Pinecone网络32KB限制内
	const pineconeMaxChunkSize = 17 * 1024 // 17KB - 最大安全分片大小，确保Base64编码后不超过32KB
	
	if fileSize < 1024*1024 { // 小于1MB
		return 12 * 1024 // 12KB - 小文件使用较小分片，确保Base64编码后不超过32KB
	} else if fileSize < 100*1024*1024 { // 小于100MB
		return 15 * 1024 // 15KB - 中等文件，确保Base64编码后不超过32KB
	} else {
		return pineconeMaxChunkSize // 17KB - 大文件使用最大允许分片
	}
}

// adaptChunkSize 自适应调整分片大小
func (tm *TransmissionManager) adaptChunkSize(chunkManager *ChunkManager, config *WindowConfig, startTime time.Time) {
	elapsed := time.Since(startTime)
	if elapsed > 5*time.Second { // 每5秒调整一次
		// 根据传输速度调整分片大小
		// 这里可以实现更复杂的自适应算法
	}
}

// checkResumePoint 检查断点续传位置
func (tm *TransmissionManager) checkResumePoint(fileID string) int {
	// 从持久化存储中读取断点信息
	// 这里简化实现，实际应该从数据库或文件读取
	return 0
}

// waitForAck 等待ACK确认
func (tm *TransmissionManager) waitForAck(conn net.Conn, timeout time.Duration) error {
	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(timeout))

	// 读取ACK消息
	ack, err := tm.receiveAck(conn)
	if err != nil {
		return err
	}

	if !ack.Success {
		return fmt.Errorf("分片传输失败: %s", ack.Message)
	}

	return nil
}

// sendAck 发送ACK确认
func (tm *TransmissionManager) sendAck(conn net.Conn, ack *ChunkAck) error {
	message, err := tm.protocolHandler.EncodeAck(ack)
	if err != nil {
		return fmt.Errorf("编码ACK失败: %v", err)
	}

	_, err = conn.Write([]byte(message + "\n"))
	return err
}

// receiveAck 接收ACK确认
func (tm *TransmissionManager) receiveAck(conn net.Conn) (*ChunkAck, error) {
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		return nil, err
	}

	message := string(buffer[:n])
	messageType, data, err := tm.protocolHandler.DecodeMessage(message)
	if err != nil {
		return nil, fmt.Errorf("解码消息失败: %v", err)
	}

	if messageType != MessageTypeAck {
		return nil, fmt.Errorf("无效的ACK消息类型: %s", messageType)
	}

	return tm.protocolHandler.DecodeAck(data)
}

// generateFileID 生成文件ID
func (tm *TransmissionManager) generateFileID(filePath string, fileSize int64) string {
	// 使用文件路径、大小和时间戳生成唯一ID
	timestamp := time.Now().Unix()
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s_%d_%d", filePath, fileSize, timestamp)))
	return base64.URLEncoding.EncodeToString(hash[:16])
}

// calculateChecksum 计算文件校验和（已优化）
func (tm *TransmissionManager) calculateChecksum(file *os.File) (string, error) {
	// 使用流式计算校验和，避免将整个文件加载到内存
	hash := sha256.New()
	
	// 重置文件指针到开始位置
	file.Seek(0, 0)
	
	// 使用优化的内存池
	memPool := performance.GetMemoryPool()
	buffer := memPool.GetMediumBuffer() // 16KB缓冲区
	defer memPool.PutMediumBuffer(buffer)
	
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return "", err
		}
		if n == 0 {
			break
		}
		hash.Write(buffer[:n])
	}
	
	// 重置文件指针
	file.Seek(0, 0)
	
	// 统一使用hex编码，与util.CalculateChecksum保持一致
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// sendRequest 发送传输请求
func (tm *TransmissionManager) sendRequest(conn net.Conn, request *TransferRequest) error {
	message, err := tm.protocolHandler.EncodeTransferRequest(request)
	if err != nil {
		return fmt.Errorf("编码请求失败: %v", err)
	}

	_, err = conn.Write([]byte(message + "\n"))
	return err
}

// receiveRequest 接收传输请求
func (tm *TransmissionManager) receiveRequest(conn net.Conn) (*TransferRequest, error) {
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil {
		return nil, err
	}

	message := string(buffer[:n])
	messageType, data, err := tm.protocolHandler.DecodeMessage(message)
	if err != nil {
		return nil, fmt.Errorf("解码消息失败: %v", err)
	}

	if messageType != MessageTypeTransferRequest {
		return nil, fmt.Errorf("无效的传输请求类型: %s", messageType)
	}

	return tm.protocolHandler.DecodeTransferRequest(data)
}

// sendResponse 发送传输响应
func (tm *TransmissionManager) sendResponse(conn net.Conn, response *TransferResponse) error {
	message, err := tm.protocolHandler.EncodeTransferResponse(response)
	if err != nil {
		return fmt.Errorf("编码响应失败: %v", err)
	}

	_, err = conn.Write([]byte(message + "\n"))
	return err
}

// receiveResponse 接收传输响应
func (tm *TransmissionManager) receiveResponse(conn net.Conn) (*TransferResponse, error) {
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil {
		return nil, err
	}

	message := string(buffer[:n])
	messageType, data, err := tm.protocolHandler.DecodeMessage(message)
	if err != nil {
		return nil, fmt.Errorf("解码消息失败: %v", err)
	}

	if messageType != MessageTypeTransferResponse {
		return nil, fmt.Errorf("无效的传输响应类型: %s", messageType)
	}

	return tm.protocolHandler.DecodeTransferResponse(data)
}

// sendChunk 发送分片
func (tm *TransmissionManager) sendChunk(conn net.Conn, chunk *Chunk) error {
	message, err := tm.protocolHandler.EncodeChunk(chunk)
	if err != nil {
		return fmt.Errorf("编码分片失败: %v", err)
	}

	_, err = conn.Write([]byte(message + "\n"))
	return err
}

// 使用统一的性能优化模块管理内存池

// receiveChunk 接收分片（已优化）
func (tm *TransmissionManager) receiveChunk(conn net.Conn) (*Chunk, error) {
	memPool := performance.GetMemoryPool()
	buffer := memPool.GetLargeBuffer() // 256KB缓冲区
	defer memPool.PutLargeBuffer(buffer)
	
	n, err := conn.Read(buffer)
	if err != nil {
		return nil, err
	}

	message := string(buffer[:n])
	messageType, data, err := tm.protocolHandler.DecodeMessage(message)
	if err != nil {
		return nil, fmt.Errorf("解码消息失败: %v", err)
	}

	if messageType != MessageTypeChunk {
		return nil, fmt.Errorf("无效的分片消息类型: %s", messageType)
	}

	return tm.protocolHandler.DecodeChunk(data)
}

// sendTransferComplete 发送传输完成信号
func (tm *TransmissionManager) sendTransferComplete(conn net.Conn, fileID string) error {
	complete := &TransferComplete{
		FileID: fileID,
		Time:   time.Now().Unix(),
	}

	message, err := tm.protocolHandler.EncodeComplete(complete)
	if err != nil {
		return fmt.Errorf("编码完成消息失败: %v", err)
	}

	_, err = conn.Write([]byte(message + "\n"))
	return err
}

// GetSession 获取传输会话
func (tm *TransmissionManager) GetSession(fileID string) *TransferSession {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.sessions[fileID]
}

// CancelSession 取消传输会话
func (tm *TransmissionManager) CancelSession(fileID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if session, exists := tm.sessions[fileID]; exists {
		session.SetCanceled()
		return nil
	}
	return fmt.Errorf("会话不存在: %s", fileID)
}

// ListSessions 列出所有会话
func (tm *TransmissionManager) ListSessions() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var fileIDs []string
	for fileID := range tm.sessions {
		fileIDs = append(fileIDs, fileID)
	}
	return fileIDs
}
