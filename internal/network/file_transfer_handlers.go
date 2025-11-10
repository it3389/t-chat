package network

// 文件传输消息处理器实现

// FileDataHandler 处理file_data消息
type FileDataHandler struct {
	pineconeService     *PineconeService
	fileTransferService *FileTransferService
	logger             Logger
	initialized        bool
}

func (h *FileDataHandler) HandleMessage(msg *Message) error {
	h.logger.Debugf("[FileDataHandler] 处理file_data消息: From=%s, Content=%s", msg.From, msg.Content)
	// 这里可以添加具体的文件数据处理逻辑
	return nil
}

func (h *FileDataHandler) GetMessageType() string {
	return "file_data"
}

func (h *FileDataHandler) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FileDataHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == "file_data"
}

func (h *FileDataHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FileDataHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// FileRequestHandler 处理file_request消息
type FileRequestHandler struct {
	pineconeService     *PineconeService
	fileTransferService *FileTransferService
	logger             Logger
	initialized        bool
}

func (h *FileRequestHandler) HandleMessage(msg *Message) error {
	h.logger.Debugf("[FileRequestHandler] 处理file_request消息: From=%s, Content=%s", msg.From, msg.Content)
	
	// 使用FileTransferService处理文件请求
	if h.fileTransferService != nil {
		return h.fileTransferService.HandleFileRequest(msg)
	}
	
	// 兼容旧的处理逻辑
	if fileReq, ok := msg.Metadata["file_request"]; ok {
		if reqData, ok := fileReq.(map[string]interface{}); ok {
			fileID := reqData["file_id"].(string)
			fileName := reqData["file_name"].(string)
			size := int64(reqData["size"].(float64))
			total := int(reqData["total"].(float64))
			checksum := reqData["checksum"].(string)
			
			h.logger.Infof("[FileRequestHandler] 收到文件传输请求: FileID=%s, FileName=%s, Size=%d, Total=%d, Checksum=%s", fileID, fileName, size, total, checksum)
			
			// 发送ACK确认接收文件请求
			ackMsg := FileAckMsg{
				FileID:        fileID,
				ReceivedIndex: -1, // -1表示确认文件请求
			}
			
			packetAck := &MessagePacket{
				From:    h.pineconeService.GetPineconeAddr(),
				To:      msg.From,
				Type:    MessageTypeFileAck,
				Content: "file_request_ack",
				Metadata: map[string]interface{}{
					"file_ack": ackMsg,
				},
			}
			
			err := h.pineconeService.SendMessagePacket(msg.From, packetAck)
			if err != nil {
				h.logger.Errorf("[FileRequestHandler] 发送ACK失败: %v", err)
				return err
			}
			
			h.logger.Debugf("[FileRequestHandler] 已发送文件请求ACK: FileID=%s", fileID)
		}
	}
	
	return nil
}

func (h *FileRequestHandler) GetMessageType() string {
	return MessageTypeFileRequest
}

func (h *FileRequestHandler) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FileRequestHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == MessageTypeFileRequest
}

func (h *FileRequestHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FileRequestHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// FileChunkHandler 处理file_chunk消息
type FileChunkHandler struct {
	pineconeService     *PineconeService
	fileTransferService *FileTransferService
	logger             Logger
	initialized        bool
}

func (h *FileChunkHandler) HandleMessage(msg *Message) error {
	h.logger.Debugf("[FileChunkHandler] 处理file_chunk消息: From=%s, Content=%s", msg.From, msg.Content)
	
	// 使用FileTransferService处理文件分片
	if h.fileTransferService != nil {
		return h.fileTransferService.HandleFileChunk(msg)
	}
	
	// 兼容旧的处理逻辑
	if fileChunk, ok := msg.Metadata["file_chunk"]; ok {
		if chunkData, ok := fileChunk.(map[string]interface{}); ok {
			fileID := chunkData["file_id"].(string)
			index := int(chunkData["index"].(float64))
			total := int(chunkData["total"].(float64))
			checksum := chunkData["checksum"].(string)
			
			h.logger.Infof("[FileChunkHandler] 收到文件分片: FileID=%s, Index=%d/%d, Checksum=%s", fileID, index, total, checksum)
			
			// 发送ACK确认接收分片
			ackMsg := FileAckMsg{
				FileID:        fileID,
				ReceivedIndex: index,
			}
			
			packetAck := &MessagePacket{
				From:    h.pineconeService.GetPineconeAddr(),
				To:      msg.From,
				Type:    MessageTypeFileAck,
				Content: "file_chunk_ack",
				Metadata: map[string]interface{}{
					"file_ack": ackMsg,
				},
			}
			
			err := h.pineconeService.SendMessagePacket(msg.From, packetAck)
			if err != nil {
				h.logger.Errorf("[FileChunkHandler] 发送ACK失败: %v", err)
				return err
			}
			
			h.logger.Debugf("[FileChunkHandler] 已发送文件分片ACK: FileID=%s, Index=%d", fileID, index)
		}
	}
	
	return nil
}

func (h *FileChunkHandler) GetMessageType() string {
	return MessageTypeFileChunk
}

func (h *FileChunkHandler) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FileChunkHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == MessageTypeFileChunk
}

func (h *FileChunkHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FileChunkHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// FileAckHandler 处理file_ack消息
type FileAckHandler struct {
	pineconeService     *PineconeService
	fileTransferService *FileTransferService
	logger             Logger
	initialized        bool
}

func (h *FileAckHandler) HandleMessage(msg *Message) error {
	h.logger.Debugf("[FileAckHandler] 处理file_ack消息: From=%s, Content=%s", msg.From, msg.Content)
	
	// 使用FileTransferService处理文件确认
	if h.fileTransferService != nil {
		return h.fileTransferService.HandleFileAck(msg)
	}
	
	// 兼容旧的处理逻辑
	return nil
}

func (h *FileAckHandler) GetMessageType() string {
	return MessageTypeFileAck
}

func (h *FileAckHandler) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FileAckHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == MessageTypeFileAck
}

func (h *FileAckHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FileAckHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// FileCompleteHandler 处理file_complete消息
type FileCompleteHandler struct {
	pineconeService     *PineconeService
	fileTransferService *FileTransferService
	logger             Logger
	initialized        bool
}

func (h *FileCompleteHandler) HandleMessage(msg *Message) error {
	h.logger.Debugf("[FileCompleteHandler] 处理file_complete消息: From=%s, Content=%s", msg.From, msg.Content)
	
	// 使用FileTransferService处理文件完成
	if h.fileTransferService != nil {
		return h.fileTransferService.HandleFileComplete(msg)
	}
	
	// 兼容旧的处理逻辑
	return nil
}

func (h *FileCompleteHandler) GetMessageType() string {
	return MessageTypeFileComplete
}

func (h *FileCompleteHandler) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FileCompleteHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == MessageTypeFileComplete
}

func (h *FileCompleteHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FileCompleteHandler) Cleanup() error {
	h.initialized = false
	return nil
}

// FileNackHandler 处理file_nack消息
type FileNackHandler struct {
	pineconeService     *PineconeService
	fileTransferService *FileTransferService
	logger             Logger
	initialized        bool
}

func (h *FileNackHandler) HandleMessage(msg *Message) error {
	h.logger.Debugf("[FileNackHandler] 处理file_nack消息: From=%s, Content=%s", msg.From, msg.Content)
	
	// 使用FileTransferService处理文件否定确认
	if h.fileTransferService != nil {
		// 这里可以添加NACK处理逻辑，比如重传等
		h.logger.Warnf("[FileNackHandler] 收到文件传输NACK: From=%s", msg.From)
	}
	
	// 兼容旧的处理逻辑
	return nil
}

func (h *FileNackHandler) GetMessageType() string {
	return MessageTypeFileNack
}

func (h *FileNackHandler) GetPriority() int {
	return MessagePriorityHigh
}

func (h *FileNackHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == MessageTypeFileNack
}

func (h *FileNackHandler) Initialize() error {
	h.initialized = true
	return nil
}

func (h *FileNackHandler) Cleanup() error {
	h.initialized = false
	return nil
}