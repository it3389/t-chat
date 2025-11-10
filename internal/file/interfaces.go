package file

import (
	"net"
	"time"
)

// FileManagerInterface 文件管理器接口
type FileManagerInterface interface {
	// 文件操作
	SaveFile(fileName string, data []byte, from string) error
	GetFile(fileName string) ([]byte, error)
	DeleteFile(fileName string) error
	GetFileInfo(fileName string) *FileInfo
	GetAllFiles() []*FileInfo

	// 分片操作
	SaveFileChunk(sessionID string, chunkIndex int, data []byte, checksum string) error
	
	// 会话管理
	CreateSession(sessionID, fileName string, fileSize int64, checksum string, from, to string, totalChunks int) *FileTransferSession
	GetSession(sessionID string) *FileTransferSession
	GetAllSessions() []*FileTransferSession
	
	// 统计信息
	GetStats() *TransferStats
}

// TransmissionManagerInterface 传输管理器接口
type TransmissionManagerInterface interface {
	// 文件传输
	SendFile(conn net.Conn, filePath, toUser string, chunkSize int) error
	ReceiveFile(conn net.Conn, savePath string) error
	
	// 会话管理
	GetSession(fileID string) *TransferSession
	CancelSession(fileID string) error
	ListSessions() []string
}

// ChunkManagerInterface 分片管理器接口
type ChunkManagerInterface interface {
	NextChunk() (*Chunk, error)
	CurrentChunk() int
	TotalChunks() int
	FileID() string
	SetResumePoint(resumePoint int)
	Close() error
}

// ProtocolHandlerInterface 协议处理器接口
type ProtocolHandlerInterface interface {
	// 消息编解码
	EncodeMessage(messageType string, data interface{}) (string, error)
	DecodeMessage(message string) (string, []byte, error)
	
	// 传输请求
	EncodeTransferRequest(req *TransferRequest) (string, error)
	DecodeTransferRequest(data []byte) (*TransferRequest, error)
	
	// 传输响应
	EncodeTransferResponse(resp *TransferResponse) (string, error)
	DecodeTransferResponse(data []byte) (*TransferResponse, error)
	
	// 分片处理
	EncodeChunk(chunk *Chunk) (string, error)
	DecodeChunk(data []byte) (*Chunk, error)
	
	// 确认消息
	EncodeAck(ack *ChunkAck) (string, error)
	DecodeAck(data []byte) (*ChunkAck, error)
	
	// 完成消息
	EncodeComplete(complete *TransferComplete) (string, error)
	DecodeComplete(data []byte) (*TransferComplete, error)
}

// ReceiverInterface 接收器接口
type ReceiverInterface interface {
	ReceiveChunk(chunk *Chunk) error
	GetProgress() float64
	IsComplete() bool
	GetFilePath() string
	Close() error
}

// ProgressTrackerInterface 进度跟踪器接口
type ProgressTrackerInterface interface {
	UpdateProgress(sessionID string, progress float64)
	GetProgress(sessionID string) float64
	SetCallback(callback func(sessionID string, progress float64))
}

// FileServiceInterface 文件服务接口（统一入口）
type FileServiceInterface interface {
	// 文件管理
	FileManagerInterface
	
	// 文件传输
	SendFile(conn net.Conn, filePath, toUser string, chunkSize int) error
	ReceiveFile(conn net.Conn, savePath string) error
	
	// 传输会话管理
	GetTransferSession(fileID string) *TransferSession
	CancelTransferSession(fileID string) error
	ListTransferSessions() []string
	
	// 配置管理
	GetConfig() *Config
	SetConfig(config *Config)
	
	// 生命周期管理
	Start() error
	Stop() error
	IsRunning() bool
}

// Config 文件服务配置
type Config struct {
	// 基础配置
	ChunkSize     int           `json:"chunk_size"`     // 分片大小
	WindowSize    int           `json:"window_size"`    // 滑动窗口大小
	Timeout       time.Duration `json:"timeout"`        // 超时时间
	RetryCount    int           `json:"retry_count"`    // 重试次数
	
	// 路径配置
	DataDir       string `json:"data_dir"`       // 数据目录
	TempDir       string `json:"temp_dir"`       // 临时目录
	
	// 性能配置
	MaxConcurrent int `json:"max_concurrent"` // 最大并发传输数
	BufferSize    int `json:"buffer_size"`    // 缓冲区大小
	
	// 安全配置
	EnableEncryption bool `json:"enable_encryption"` // 是否启用加密
	MaxFileSize      int64 `json:"max_file_size"`     // 最大文件大小
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:        DefaultChunkSize,
		WindowSize:       DefaultWindowSize,
		Timeout:          DefaultTimeout,
		RetryCount:       DefaultRetryCount,
		DataDir:          "data/files",
		TempDir:          "data/temp",
		MaxConcurrent:    5,
		BufferSize:       8192,
		EnableEncryption: true,
		MaxFileSize:      100 * 1024 * 1024, // 100MB
	}
}