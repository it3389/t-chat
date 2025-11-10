package file

import (
	"fmt"
	"strings"
	"time"
	"t-chat/internal/performance"
)

// 协议常量 - 与网络层保持一致
const (
	// 文件传输消息类型
	MessageTypeTransferRequest  = "file_request"
	MessageTypeTransferResponse = "file_ack"
	MessageTypeChunk            = "file_chunk"
	MessageTypeAck              = "file_ack"
	MessageTypeComplete         = "file_complete"
	MessageTypeNack             = "file_nack"
	MessageTypeData             = "file_data"

	// 默认配置
	DefaultChunkSize  = 17 * 1024 // 17KB - 最大安全分片大小，确保Base64编码后不超过32KB
	DefaultWindowSize = 10
	DefaultTimeout    = 30 * time.Second
	DefaultRetryCount = 3

	// 传输状态常量
	TransferStatusPending   = "pending"
	TransferStatusRunning   = "running"
	TransferStatusPaused    = "paused"
	TransferStatusCompleted = "completed"
	TransferStatusFailed    = "failed"
	TransferStatusCanceled  = "canceled"

	// 错误码
	FileTransferErrorNone           = 0
	FileTransferErrorInvalidRequest = 1001
	FileTransferErrorFileNotFound   = 1002
	FileTransferErrorPermissionDenied = 1003
	FileTransferErrorInsufficientSpace = 1004
	FileTransferErrorChecksumMismatch = 1005
	FileTransferErrorTimeout        = 1006
	FileTransferErrorNetworkError   = 1007
	FileTransferErrorCanceled       = 1008
	FileTransferErrorUnknown        = 1999
)

// TransferRequest 传输请求
type TransferRequest struct {
	FileID      string `json:"file_id"`
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	ChunkSize   int    `json:"chunk_size"`
	ChunkCount  int    `json:"chunk_count"`
	Checksum    string `json:"checksum"`
	Encrypted   bool   `json:"encrypted"`
	PublicKey   string `json:"public_key"`
	RequestTime int64  `json:"request_time"`
	ResumePoint int    `json:"resume_point"` // 断点续传位置
}

// TransferResponse 传输响应
type TransferResponse struct {
	FileID       string `json:"file_id"`
	Accepted     bool   `json:"accepted"`
	Message      string `json:"message"`
	ChunkSize    int    `json:"chunk_size"`
	SavePath     string `json:"save_path"`
	ResponseTime int64  `json:"response_time"`
	ResumePoint  int    `json:"resume_point"` // 断点续传位置
}

// ChunkAck 分片确认
type ChunkAck struct {
	FileID     string `json:"file_id"`
	ChunkIndex int    `json:"chunk_index"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Timestamp  int64  `json:"timestamp"`
}

// TransferComplete 传输完成消息
type TransferComplete struct {
	FileID string `json:"file_id"`
	Time   int64  `json:"time"`
}

// ProtocolHandler 协议处理器，实现 ProtocolHandlerInterface 接口
// 负责文件传输协议的编解码
type ProtocolHandler struct{}

// 确保 ProtocolHandler 实现了 ProtocolHandlerInterface 接口
var _ ProtocolHandlerInterface = (*ProtocolHandler)(nil)

// NewProtocolHandler 创建协议处理器
func NewProtocolHandler() *ProtocolHandler {
	return &ProtocolHandler{}
}

// EncodeMessage 编码消息（已优化）
func (ph *ProtocolHandler) EncodeMessage(messageType string, data interface{}) (string, error) {
	jsonOptimizer := performance.GetJSONOptimizer()
	jsonData, err := jsonOptimizer.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("序列化消息失败: %v", err)
	}

	// 使用优化的字符串构建器
	stringOptimizer := performance.GetStringOptimizer()
	return stringOptimizer.ConcatStrings(messageType, ":", string(jsonData)), nil
}

// DecodeMessage 解码消息
func (ph *ProtocolHandler) DecodeMessage(message string) (string, []byte, error) {
	parts := strings.SplitN(message, ":", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("无效的消息格式")
	}

	return parts[0], []byte(parts[1]), nil
}

// EncodeTransferRequest 编码传输请求
func (ph *ProtocolHandler) EncodeTransferRequest(req *TransferRequest) (string, error) {
	return ph.EncodeMessage(MessageTypeTransferRequest, req)
}

// DecodeTransferRequest 解码传输请求（已优化）
func (ph *ProtocolHandler) DecodeTransferRequest(data []byte) (*TransferRequest, error) {
	var req TransferRequest
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("解析传输请求失败: %v", err)
	}
	return &req, nil
}

// EncodeTransferResponse 编码传输响应
func (ph *ProtocolHandler) EncodeTransferResponse(resp *TransferResponse) (string, error) {
	return ph.EncodeMessage(MessageTypeTransferResponse, resp)
}

// DecodeTransferResponse 解码传输响应（已优化）
func (ph *ProtocolHandler) DecodeTransferResponse(data []byte) (*TransferResponse, error) {
	var resp TransferResponse
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析传输响应失败: %v", err)
	}
	return &resp, nil
}

// EncodeChunk 编码分片
func (ph *ProtocolHandler) EncodeChunk(chunk *Chunk) (string, error) {
	return ph.EncodeMessage(MessageTypeChunk, chunk)
}

// DecodeChunk 解码分片（已优化）
func (ph *ProtocolHandler) DecodeChunk(data []byte) (*Chunk, error) {
	var chunk Chunk
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, &chunk); err != nil {
		return nil, fmt.Errorf("解析分片失败: %v", err)
	}
	return &chunk, nil
}

// EncodeAck 编码ACK
func (ph *ProtocolHandler) EncodeAck(ack *ChunkAck) (string, error) {
	return ph.EncodeMessage(MessageTypeAck, ack)
}

// DecodeAck 解码ACK（已优化）
func (ph *ProtocolHandler) DecodeAck(data []byte) (*ChunkAck, error) {
	var ack ChunkAck
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, &ack); err != nil {
		return nil, fmt.Errorf("解析ACK失败: %v", err)
	}
	return &ack, nil
}

// EncodeComplete 编码完成消息
func (ph *ProtocolHandler) EncodeComplete(complete *TransferComplete) (string, error) {
	return ph.EncodeMessage(MessageTypeComplete, complete)
}

// DecodeComplete 解码完成消息（已优化）
func (ph *ProtocolHandler) DecodeComplete(data []byte) (*TransferComplete, error) {
	var complete TransferComplete
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, &complete); err != nil {
		return nil, fmt.Errorf("解析完成消息失败: %v", err)
	}
	return &complete, nil
}
