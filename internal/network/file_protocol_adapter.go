package network

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"t-chat/internal/file"
)

// FileProtocolAdapter 文件传输协议适配器
// 负责在文件层协议和网络层协议之间进行转换
type FileProtocolAdapter struct {
	logger Logger
}

// isHexString 检查字符串是否为有效的十六进制字符串
func isHexString(s string) bool {
	matched, _ := regexp.MatchString("^[0-9a-fA-F]+$", s)
	return matched
}

// NewFileProtocolAdapter 创建新的文件协议适配器
func NewFileProtocolAdapter(logger Logger) *FileProtocolAdapter {
	return &FileProtocolAdapter{
		logger: logger,
	}
}

// ConvertToNetworkFileRequest 将文件层的TransferRequest转换为网络层的FileRequestMsg
func (adapter *FileProtocolAdapter) ConvertToNetworkFileRequest(req *file.TransferRequest) *FileRequestMsg {
	return &FileRequestMsg{
		FileID:   req.FileID,
		FileName: req.FileName,
		Size:     req.FileSize,
		Total:    req.ChunkCount,
		Checksum: req.Checksum,
	}
}

// ConvertFromNetworkFileRequest 将网络层的FileRequestMsg转换为文件层的TransferRequest
func (adapter *FileProtocolAdapter) ConvertFromNetworkFileRequest(msg *FileRequestMsg) *file.TransferRequest {
	return &file.TransferRequest{
		FileID:      msg.FileID,
		FileName:    msg.FileName,
		FileSize:    msg.Size,
		ChunkSize:   file.DefaultChunkSize,
		ChunkCount:  msg.Total,
		Checksum:    msg.Checksum,
		Encrypted:   false,
		RequestTime: time.Now().Unix(),
		ResumePoint: 0,
	}
}

// ConvertToNetworkFileChunk 将文件层的Chunk转换为网络层的FileChunkMsg
// 使用BASE64编码文件数据以确保JSON序列化安全
func (adapter *FileProtocolAdapter) ConvertToNetworkFileChunk(chunk *file.Chunk, fileID, fileName string, total int) *FileChunkMsg {
	// 将二进制数据编码为BASE64字符串
	encodedData := base64.StdEncoding.EncodeToString(chunk.Data)
	
	// 将哈希字节数组转换为十六进制字符串
	// 如果chunk.Hash已经是十六进制字符串的字节表示，直接转换为字符串
	// 否则，将字节数组转换为十六进制字符串
	var hashHex string
	if len(chunk.Hash) > 0 {
		// 尝试将字节数组作为字符串处理（如果它已经是十六进制字符串）
		hashStr := string(chunk.Hash)
		// 检查是否为有效的十六进制字符串
		if len(hashStr) > 0 && isHexString(hashStr) {
			hashHex = hashStr
		} else {
			// 如果不是十六进制字符串，则将字节数组转换为十六进制
			hashHex = fmt.Sprintf("%x", chunk.Hash)
		}
	}
	
	return &FileChunkMsg{
		FileID:   fileID,
		Index:    chunk.Index,
		Total:    total,
		Data:     encodedData, // 直接存储BASE64编码的字符串
		Checksum: hashHex, // 使用十六进制字符串形式的校验和
		FileName: fileName,
	}
}

// ConvertFromNetworkFileChunk 将网络层的FileChunkMsg转换为文件层的Chunk
// 解码BASE64编码的文件数据
func (adapter *FileProtocolAdapter) ConvertFromNetworkFileChunk(msg *FileChunkMsg) *file.Chunk {
	if adapter.logger != nil {
		adapter.logger.Debugf("[ConvertFromNetworkFileChunk] 开始转换网络分片")
		adapter.logger.Debugf("[ConvertFromNetworkFileChunk] 输入数据类型: %T", msg.Data)
		adapter.logger.Debugf("[ConvertFromNetworkFileChunk] 输入数据长度: %d", len(msg.Data))
		adapter.logger.Debugf("[ConvertFromNetworkFileChunk] 输入数据内容: %s", msg.Data)
	}
	
	// 解码BASE64字符串为原始二进制数据
	decodedData, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		// 如果解码失败，记录错误并使用原始数据（向后兼容）
		if adapter.logger != nil {
			adapter.logger.Errorf("[ConvertFromNetworkFileChunk] BASE64解码失败，使用原始数据: %v", err)
			adapter.logger.Debugf("[ConvertFromNetworkFileChunk] 使用原始数据作为fallback")
		}
		decodedData = []byte(msg.Data)
	} else {
		if adapter.logger != nil {
			adapter.logger.Debugf("[ConvertFromNetworkFileChunk] BASE64解码成功")
			adapter.logger.Debugf("[ConvertFromNetworkFileChunk] 解码后数据长度: %d", len(decodedData))
			// 不输出二进制数据内容，避免数据损坏
		}
	}
	
	// 将十六进制字符串转换回字节数组
	var hashBytes []byte
	if msg.Checksum != "" {
		// 尝试将十六进制字符串解码为字节数组
		if decoded, err := hex.DecodeString(msg.Checksum); err == nil {
			hashBytes = decoded
		} else {
			// 如果解码失败，直接使用字符串的字节表示（向后兼容）
			hashBytes = []byte(msg.Checksum)
		}
	}

	return &file.Chunk{
		Index:  msg.Index,
		Offset: 0, // Offset应该由接收端根据实际情况计算，这里设为0
		Size:   len(decodedData),
		Data:   decodedData,
		Hash:   hashBytes,
		Done:   false,
	}
}

// ConvertToNetworkFileAck 将文件层的ChunkAck转换为网络层的FileAckMsg
func (adapter *FileProtocolAdapter) ConvertToNetworkFileAck(ack *file.ChunkAck) *FileAckMsg {
	return &FileAckMsg{
		FileID:        ack.FileID,
		ReceivedIndex: ack.ChunkIndex,
	}
}

// ConvertFromNetworkFileAck 将网络层的FileAckMsg转换为文件层的ChunkAck
func (adapter *FileProtocolAdapter) ConvertFromNetworkFileAck(msg *FileAckMsg) *file.ChunkAck {
	return &file.ChunkAck{
		FileID:     msg.FileID,
		ChunkIndex: msg.ReceivedIndex,
		Success:    true,
		Message:    "chunk received",
		Timestamp:  time.Now().Unix(),
	}
}

// ConvertToNetworkFileComplete 将文件层的TransferComplete转换为网络层的FileCompleteMsg
func (adapter *FileProtocolAdapter) ConvertToNetworkFileComplete(complete *file.TransferComplete) *FileCompleteMsg {
	return &FileCompleteMsg{
		FileID: complete.FileID,
	}
}

// ConvertFromNetworkFileComplete 将网络层的FileCompleteMsg转换为文件层的TransferComplete
func (adapter *FileProtocolAdapter) ConvertFromNetworkFileComplete(msg *FileCompleteMsg) *file.TransferComplete {
	return &file.TransferComplete{
		FileID: msg.FileID,
		Time:   time.Now().Unix(),
	}
}

// CreateMessagePacketFromFileRequest 从文件请求创建消息包
func (adapter *FileProtocolAdapter) CreateMessagePacketFromFileRequest(req *file.TransferRequest, from, to string) *MessagePacket {
	networkReq := adapter.ConvertToNetworkFileRequest(req)
	
	return &MessagePacket{
		ID:        fmt.Sprintf("file-req-%s-%d", req.FileID, time.Now().UnixNano()),
		Type:      MessageTypeFileRequest,
		From:      from,
		To:        to,
		Content:   fmt.Sprintf("文件传输请求: %s", req.FileName),
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"file_request": networkReq,
		},
	}
}

// CreateMessagePacketFromFileChunk 从文件分片创建消息包
func (adapter *FileProtocolAdapter) CreateMessagePacketFromFileChunk(chunk *file.Chunk, fileID, fileName, from, to string, total int) *MessagePacket {
	networkChunk := adapter.ConvertToNetworkFileChunk(chunk, fileID, fileName, total)
	
	return &MessagePacket{
		ID:        fmt.Sprintf("file-chunk-%s-%d-%d", fileID, chunk.Index, time.Now().UnixNano()),
		Type:      MessageTypeFileChunk,
		From:      from,
		To:        to,
		Content:   fmt.Sprintf("文件分片 %d/%d", chunk.Index+1, total),
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"file_chunk": networkChunk,
		},
	}
}

// CreateMessagePacketFromFileAck 从文件确认创建消息包
func (adapter *FileProtocolAdapter) CreateMessagePacketFromFileAck(ack *file.ChunkAck, from, to string) *MessagePacket {
	networkAck := adapter.ConvertToNetworkFileAck(ack)
	
	return &MessagePacket{
		ID:        fmt.Sprintf("file-ack-%s-%d-%d", ack.FileID, ack.ChunkIndex, time.Now().UnixNano()),
		Type:      MessageTypeFileAck,
		From:      from,
		To:        to,
		Content:   fmt.Sprintf("分片确认: %d", ack.ChunkIndex),
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"file_ack": networkAck,
		},
	}
}

// CreateMessagePacketFromFileComplete 从文件完成创建消息包
func (adapter *FileProtocolAdapter) CreateMessagePacketFromFileComplete(complete *file.TransferComplete, from, to string) *MessagePacket {
	networkComplete := adapter.ConvertToNetworkFileComplete(complete)
	
	return &MessagePacket{
		ID:        fmt.Sprintf("file-complete-%s-%d", complete.FileID, time.Now().UnixNano()),
		Type:      MessageTypeFileComplete,
		From:      from,
		To:        to,
		Content:   "文件传输完成",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"file_complete": networkComplete,
		},
	}
}

// ExtractFileRequestFromMessage 从消息中提取文件请求
func (adapter *FileProtocolAdapter) ExtractFileRequestFromMessage(msg *Message) (*file.TransferRequest, error) {
	if msg.Type != MessageTypeFileRequest {
		return nil, fmt.Errorf("消息类型不是文件请求: %s", msg.Type)
	}
	
	if fileReq, ok := msg.Metadata["file_request"]; ok {
		if reqData, ok := fileReq.(map[string]interface{}); ok {
			// 安全地解析map到FileRequestMsg
			fileID, ok := reqData["file_id"].(string)
			if !ok {
				return nil, fmt.Errorf("无效的file_id字段")
			}
			
			fileName, ok := reqData["file_name"].(string)
			if !ok {
				return nil, fmt.Errorf("无效的file_name字段")
			}
			
			sizeFloat, ok := reqData["size"].(float64)
			if !ok {
				return nil, fmt.Errorf("无效的size字段")
			}
			
			totalFloat, ok := reqData["total"].(float64)
			if !ok {
				return nil, fmt.Errorf("无效的total字段")
			}
			
			checksum, ok := reqData["checksum"].(string)
			if !ok {
				return nil, fmt.Errorf("无效的checksum字段")
			}
			
			networkReq := &FileRequestMsg{
				FileID:   fileID,
				FileName: fileName,
				Size:     int64(sizeFloat),
				Total:    int(totalFloat),
				Checksum: checksum,
			}
			return adapter.ConvertFromNetworkFileRequest(networkReq), nil
		}
	}
	
	return nil, fmt.Errorf("无法解析文件请求元数据")
}

// ExtractFileChunkFromMessage 从消息中提取文件分片
// 支持BASE64编码的文件数据处理
func (adapter *FileProtocolAdapter) ExtractFileChunkFromMessage(msg *Message) (*file.Chunk, string, error) {
	if msg.Type != MessageTypeFileChunk {
		return nil, "", fmt.Errorf("消息类型不是文件分片: %s", msg.Type)
	}
	
	if fileChunk, ok := msg.Metadata["file_chunk"]; ok {
		if chunkData, ok := fileChunk.(map[string]interface{}); ok {
			// 安全地解析各个字段
			fileID, ok := chunkData["file_id"].(string)
			if !ok {
				return nil, "", fmt.Errorf("无法解析file_id字段")
			}
			
			indexFloat, ok := chunkData["index"].(float64)
			if !ok {
				return nil, "", fmt.Errorf("无法解析index字段")
			}
			
			totalFloat, ok := chunkData["total"].(float64)
			if !ok {
				return nil, "", fmt.Errorf("无法解析total字段")
			}
			
			// 处理data字段 - 应该是BASE64编码的字符串
			var dataStr string
			if dataBytes, ok := chunkData["data"].([]byte); ok {
				// 如果是[]byte，转换为字符串（应该是BASE64编码）
				dataStr = string(dataBytes)
			} else if str, ok := chunkData["data"].(string); ok {
				// 如果是字符串，直接使用（应该是BASE64编码）
				dataStr = str
			} else {
				return nil, "", fmt.Errorf("无法解析data字段")
			}
			
			checksum, ok := chunkData["checksum"].(string)
			if !ok {
				return nil, "", fmt.Errorf("无法解析checksum字段")
			}
			
			fileName, ok := chunkData["file_name"].(string)
			if !ok {
				return nil, "", fmt.Errorf("无法解析file_name字段")
			}
			
			// 手动解析map到FileChunkMsg
			networkChunk := &FileChunkMsg{
				FileID:   fileID,
				Index:    int(indexFloat),
				Total:    int(totalFloat),
				Data:     dataStr, // 这里的data是BASE64编码的字符串
				Checksum: checksum,
				FileName: fileName,
			}
			// ConvertFromNetworkFileChunk会自动处理BASE64解码
			return adapter.ConvertFromNetworkFileChunk(networkChunk), networkChunk.FileID, nil
		}
	}
	
	return nil, "", fmt.Errorf("无法解析文件分片元数据")
}

// ExtractFileAckFromMessage 从消息中提取文件确认
func (adapter *FileProtocolAdapter) ExtractFileAckFromMessage(msg *Message) (*file.ChunkAck, error) {
	if msg.Type != MessageTypeFileAck {
		return nil, fmt.Errorf("消息类型不是文件确认: %s", msg.Type)
	}
	
	if fileAck, ok := msg.Metadata["file_ack"]; ok {
		if ackData, ok := fileAck.(map[string]interface{}); ok {
			// 手动解析map到FileAckMsg
			networkAck := &FileAckMsg{
				FileID:        ackData["file_id"].(string),
				ReceivedIndex: int(ackData["received_index"].(float64)),
			}
			return adapter.ConvertFromNetworkFileAck(networkAck), nil
		}
	}
	
	return nil, fmt.Errorf("无法解析文件确认元数据")
}

// ExtractFileCompleteFromMessage 从消息中提取文件完成
func (adapter *FileProtocolAdapter) ExtractFileCompleteFromMessage(msg *Message) (*file.TransferComplete, error) {
	if msg.Type != MessageTypeFileComplete {
		return nil, fmt.Errorf("消息类型不是文件完成: %s", msg.Type)
	}
	
	if fileComplete, ok := msg.Metadata["file_complete"]; ok {
		if completeData, ok := fileComplete.(map[string]interface{}); ok {
			// 手动解析map到FileCompleteMsg
			networkComplete := &FileCompleteMsg{
				FileID: completeData["file_id"].(string),
			}
			return adapter.ConvertFromNetworkFileComplete(networkComplete), nil
		}
	}
	
	return nil, fmt.Errorf("无法解析文件完成元数据")
}