package file

import (
	"fmt"
	"io"
	"net"
	"time"
)

// TransferHelper 传输辅助器
type TransferHelper struct {
	logger Logger
}

// NewTransferHelper 创建传输辅助器
func NewTransferHelper(logger Logger) *TransferHelper {
	return &TransferHelper{
		logger: logger,
	}
}

// SendChunksWithWindow 使用滑动窗口发送分片
func (th *TransferHelper) SendChunksWithWindow(
	conn net.Conn,
	chunkManager ChunkManagerInterface,
	session *TransferSession,
	tm *TransmissionManager,
) error {
	if err := th.initializeTransfer(session, chunkManager); err != nil {
		return err
	}

	for {
		// 检查是否需要等待窗口空间
		if err := th.waitForWindowSpace(conn, session, tm); err != nil {
			return th.handleTransferError(session, "等待窗口空间失败", err)
		}

		// 获取并发送下一个分片
		finished, err := th.sendNextChunk(conn, chunkManager, session, tm)
		if err != nil {
			return th.handleTransferError(session, "发送分片失败", err)
		}
		if finished {
			break
		}

		// 更新传输进度
		th.updateTransferProgress(chunkManager, session)
	}

	// 等待所有分片确认并发送完成信号
	return th.finalizeTransfer(conn, chunkManager, session, tm)
}

// initializeTransfer 初始化传输
func (th *TransferHelper) initializeTransfer(session *TransferSession, chunkManager ChunkManagerInterface) error {
	session.Start()
	th.logger.Infof("开始文件传输: fileID=%s, totalChunks=%d", 
		chunkManager.FileID(), chunkManager.TotalChunks())
	return nil
}

// waitForWindowSpace 等待窗口空间
func (th *TransferHelper) waitForWindowSpace(conn net.Conn, session *TransferSession, tm *TransmissionManager) error {
	for session.Window.IsWindowFull() {
		if err := tm.waitForAck(conn, session.WindowConfig.Timeout); err != nil {
			return fmt.Errorf("等待ACK超时: %v", err)
		}
	}
	return nil
}

// sendNextChunk 发送下一个分片
func (th *TransferHelper) sendNextChunk(
	conn net.Conn,
	chunkManager ChunkManagerInterface,
	session *TransferSession,
	tm *TransmissionManager,
) (bool, error) {
	// 获取下一个分片
	chunk, err := chunkManager.NextChunk()
	if err == io.EOF {
		return true, nil // 传输完成
	}
	if err != nil {
		return false, fmt.Errorf("读取分片失败: %v", err)
	}

	// 发送分片
	if err := tm.sendChunk(conn, chunk); err != nil {
		return false, fmt.Errorf("发送分片失败: %v", err)
	}

	// 添加到窗口
	session.Window.AddChunk(chunk)
	
	th.logger.Debugf("发送分片成功: chunkIndex=%d, chunkSize=%d", 
		chunk.Index, chunk.Size)

	return false, nil
}

// updateTransferProgress 更新传输进度
func (th *TransferHelper) updateTransferProgress(chunkManager ChunkManagerInterface, session *TransferSession) {
	currentChunk := chunkManager.CurrentChunk()
	totalChunks := chunkManager.TotalChunks()
	session.UpdateProgress(currentChunk, totalChunks)
	
	progress := float64(currentChunk) / float64(totalChunks) * 100
	th.logger.Debugf("传输进度更新: current=%d, total=%d, progress=%.2f%%", 
		currentChunk, totalChunks, progress)
}

// finalizeTransfer 完成传输
func (th *TransferHelper) finalizeTransfer(
	conn net.Conn,
	chunkManager ChunkManagerInterface,
	session *TransferSession,
	tm *TransmissionManager,
) error {
	// 等待所有分片确认
	for !session.Window.IsAllAcked() {
		if err := tm.waitForAck(conn, session.WindowConfig.Timeout); err != nil {
			return th.handleTransferError(session, "等待最终ACK超时", err)
		}
	}

	// 发送传输完成信号
	if err := tm.sendTransferComplete(conn, chunkManager.FileID()); err != nil {
		return th.handleTransferError(session, "发送完成信号失败", err)
	}

	session.SetCompleted()
	th.logger.Infof("文件传输完成: fileID=%s", chunkManager.FileID())
	return nil
}

// handleTransferError 处理传输错误
func (th *TransferHelper) handleTransferError(session *TransferSession, message string, err error) error {
	session.SetFailed()
	th.logger.Errorf("%s: %v", message, err)
	return fmt.Errorf("%s: %v", message, err)
}

// ReceiveChunksWithAck 接收分片并发送确认
func (th *TransferHelper) ReceiveChunksWithAck(
	conn net.Conn,
	receiver ReceiverInterface,
	totalChunks int,
	tm *TransmissionManager,
) error {
	receivedChunks := 0
	
	th.logger.Infof("开始接收文件分片: totalChunks=%d", totalChunks)

	for receivedChunks < totalChunks {
		// 接收分片
		chunk, err := tm.receiveChunk(conn)
		if err != nil {
			return th.handleReceiveError("接收分片失败", err)
		}

		// 处理分片
		if err := th.processReceivedChunk(conn, receiver, chunk, tm); err != nil {
			return th.handleReceiveError("处理分片失败", err)
		}

		receivedChunks++
		th.updateReceiveProgress(receivedChunks, totalChunks)
	}

	th.logger.Infof("文件接收完成: totalChunks=%d", totalChunks)
	return nil
}

// processReceivedChunk 处理接收到的分片
func (th *TransferHelper) processReceivedChunk(
	conn net.Conn,
	receiver ReceiverInterface,
	chunk *Chunk,
	tm *TransmissionManager,
) error {
	// 验证分片
	if !VerifyChunk(chunk) {
		ack := &ChunkAck{
			ChunkIndex: chunk.Index,
			Success:    false,
			Message:    "分片校验失败",
			Timestamp:  time.Now().Unix(),
		}
		return tm.sendAck(conn, ack)
	}

	// 保存分片
	if err := receiver.ReceiveChunk(chunk); err != nil {
		ack := &ChunkAck{
			ChunkIndex: chunk.Index,
			Success:    false,
			Message:    "保存分片失败",
			Timestamp:  time.Now().Unix(),
		}
		tm.sendAck(conn, ack)
		return err
	}

	// 发送成功确认
	ack := &ChunkAck{
		ChunkIndex: chunk.Index,
		Success:    true,
		Message:    "分片接收成功",
		Timestamp:  time.Now().Unix(),
	}
	
	th.logger.Debugf("分片接收成功: chunkIndex=%d", chunk.Index)
	return tm.sendAck(conn, ack)
}

// updateReceiveProgress 更新接收进度
func (th *TransferHelper) updateReceiveProgress(received, total int) {
	progress := float64(received) / float64(total) * 100
	th.logger.Debugf("接收进度更新: received=%d, total=%d, progress=%.2f%%", 
		received, total, progress)
}

// handleReceiveError 处理接收错误
func (th *TransferHelper) handleReceiveError(message string, err error) error {
	th.logger.Errorf("%s: %v", message, err)
	return fmt.Errorf("%s: %v", message, err)
}