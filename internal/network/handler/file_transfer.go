package handler

import (
	"fmt"
	"t-chat/internal/network"
)

// FileTransferHandler 文件传输消息处理器
type FileTransferHandler struct {
	*BaseMessageHandler
	fileTransferService FileTransferServiceInterface
}

// FileTransferServiceInterface 文件传输服务接口
type FileTransferServiceInterface interface {
	HandleFileRequest(msg *network.Message) error
	HandleFileChunk(msg *network.Message) error
	HandleFileAck(msg *network.Message) error
	HandleFileComplete(msg *network.Message) error
	HandleFileCancel(msg *network.Message) error
}

// NewFileTransferHandler 创建文件传输消息处理器
func NewFileTransferHandler(pineconeService network.PineconeServiceLike, logger network.Logger, fileService FileTransferServiceInterface) *FileTransferHandler {
	base := NewBaseMessageHandler("file_transfer", network.MessagePriorityHigh, pineconeService, logger)
	return &FileTransferHandler{
		BaseMessageHandler:  base,
		fileTransferService: fileService,
	}
}

// HandleMessage 处理文件传输消息
func (h *FileTransferHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleFileTransferMessage)
}

// handleFileTransferMessage 实际的文件传输消息处理逻辑
func (h *FileTransferHandler) handleFileTransferMessage(msg *network.Message) error {
	if h.fileTransferService == nil {
		return fmt.Errorf("文件传输服务未初始化")
	}
	
	switch msg.Type {
	case network.MessageTypeFileRequest:
		return h.fileTransferService.HandleFileRequest(msg)
	case network.MessageTypeFileChunk:
		return h.fileTransferService.HandleFileChunk(msg)
	case network.MessageTypeFileAck:
		return h.fileTransferService.HandleFileAck(msg)
	case network.MessageTypeFileComplete:
		return h.fileTransferService.HandleFileComplete(msg)
	case network.MessageTypeFileCancel:
		return h.fileTransferService.HandleFileCancel(msg)
	default:
		return fmt.Errorf("未知的文件传输消息类型: %s", msg.Type)
	}
}

// CanHandle 检查是否能处理消息
func (h *FileTransferHandler) CanHandle(msg *network.Message) bool {
	if msg == nil {
		return false
	}
	
	// 检查是否为文件传输相关的消息类型
	fileTransferTypes := []string{
		network.MessageTypeFileRequest,
		network.MessageTypeFileChunk,
		network.MessageTypeFileAck,
		network.MessageTypeFileComplete,
		network.MessageTypeFileCancel,
		network.MessageTypeFileData,
		network.MessageTypeFileNack,
	}
	
	for _, fileType := range fileTransferTypes {
		if msg.Type == fileType {
			return true
		}
	}
	
	return false
}

// Initialize 初始化文件传输处理器
func (h *FileTransferHandler) Initialize() error {
	if err := h.BaseMessageHandler.Initialize(); err != nil {
		return err
	}
	
	if h.fileTransferService == nil {
		return fmt.Errorf("文件传输服务不能为空")
	}
	
	h.logger.Debugf("[FileTransferHandler] 文件传输处理器初始化完成")
	return nil
}

// FileDataHandler 处理file_data消息的具体实现
type FileDataHandler struct {
	*BaseMessageHandler
	fileTransferService FileTransferServiceInterface
}

// NewFileDataHandler 创建文件数据处理器
func NewFileDataHandler(pineconeService network.PineconeServiceLike, logger network.Logger, fileService FileTransferServiceInterface) *FileDataHandler {
	base := NewBaseMessageHandler(network.MessageTypeFileData, network.MessagePriorityHigh, pineconeService, logger)
	return &FileDataHandler{
		BaseMessageHandler:  base,
		fileTransferService: fileService,
	}
}

// HandleMessage 处理文件数据消息
func (h *FileDataHandler) HandleMessage(msg *network.Message) error {
	return h.ProcessMessageWithStats(msg, h.handleFileDataMessage)
}

// handleFileDataMessage 实际的文件数据消息处理逻辑
func (h *FileDataHandler) handleFileDataMessage(msg *network.Message) error {
	h.logger.Debugf("[FileDataHandler] 处理file_data消息: From=%s, Content=%s", msg.From, msg.Content)
	
	// 这里可以添加具体的文件数据处理逻辑
	// 例如：解析文件数据、验证完整性、保存到本地等
	
	// 发送确认消息
	if msg.ID != "" {
		if err := h.SendAckMessage(msg.From, msg.ID, "received"); err != nil {
			h.logger.Errorf("[FileDataHandler] 发送确认消息失败: %v", err)
			return err
		}
	}
	
	return nil
}