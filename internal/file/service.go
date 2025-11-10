package file

import (
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// FileService 文件服务实现
type FileService struct {
	config              *Config
	fileManager         FileManagerInterface
	transmissionManager TransmissionManagerInterface
	protocolHandler     ProtocolHandlerInterface
	progressTracker     ProgressTrackerInterface
	logger              *zap.Logger
	running             bool
	mutex               sync.RWMutex
}

// zapLoggerAdapter 适配器，将*zap.Logger适配为Logger接口
type zapLoggerAdapter struct {
	logger *zap.Logger
}

func (z *zapLoggerAdapter) Infof(format string, args ...interface{}) {
	z.logger.Sugar().Infof(format, args...)
}

func (z *zapLoggerAdapter) Debugf(format string, args ...interface{}) {
	z.logger.Sugar().Debugf(format, args...)
}

func (z *zapLoggerAdapter) Errorf(format string, args ...interface{}) {
	z.logger.Sugar().Errorf(format, args...)
}

// NewFileService 创建文件服务
func NewFileService(logger *zap.Logger, config *Config) *FileService {
	if config == nil {
		config = DefaultConfig()
	}

	service := &FileService{
		config:  config,
		logger:  logger,
		running: false,
	}

	// 初始化各个组件
	service.fileManager = NewManager()
	loggerAdapter := &zapLoggerAdapter{logger: logger}
	service.transmissionManager = NewTransmissionManager(loggerAdapter)
	service.protocolHandler = NewProtocolHandler()
	service.progressTracker = NewProgressTracker()

	return service
}

// Start 启动文件服务
func (fs *FileService) Start() error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	if fs.running {
		return fmt.Errorf("文件服务已经在运行")
	}

	fs.logger.Info("启动文件服务", zap.Any("config", fs.config))
	fs.running = true
	return nil
}

// Stop 停止文件服务
func (fs *FileService) Stop() error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	if !fs.running {
		return fmt.Errorf("文件服务未运行")
	}

	fs.logger.Info("停止文件服务")
	fs.running = false
	return nil
}

// IsRunning 检查服务是否运行
func (fs *FileService) IsRunning() bool {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()
	return fs.running
}

// GetConfig 获取配置
func (fs *FileService) GetConfig() *Config {
	return fs.config
}

// SetConfig 设置配置
func (fs *FileService) SetConfig(config *Config) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	fs.config = config
}

// 实现 FileManagerInterface 接口
func (fs *FileService) SaveFile(fileName string, data []byte, from string) error {
	return fs.fileManager.SaveFile(fileName, data, from)
}

func (fs *FileService) GetFile(fileName string) ([]byte, error) {
	return fs.fileManager.GetFile(fileName)
}

func (fs *FileService) DeleteFile(fileName string) error {
	return fs.fileManager.DeleteFile(fileName)
}

func (fs *FileService) GetFileInfo(fileName string) *FileInfo {
	return fs.fileManager.GetFileInfo(fileName)
}

func (fs *FileService) GetAllFiles() []*FileInfo {
	return fs.fileManager.GetAllFiles()
}

func (fs *FileService) SaveFileChunk(sessionID string, chunkIndex int, data []byte, checksum string) error {
	return fs.fileManager.SaveFileChunk(sessionID, chunkIndex, data, checksum)
}

func (fs *FileService) CreateSession(sessionID, fileName string, fileSize int64, checksum string, from, to string, totalChunks int) *FileTransferSession {
	return fs.fileManager.CreateSession(sessionID, fileName, fileSize, checksum, from, to, totalChunks)
}

func (fs *FileService) GetSession(sessionID string) *FileTransferSession {
	return fs.fileManager.GetSession(sessionID)
}

func (fs *FileService) GetAllSessions() []*FileTransferSession {
	return fs.fileManager.GetAllSessions()
}

func (fs *FileService) GetStats() *TransferStats {
	return fs.fileManager.GetStats()
}

// 实现 TransmissionManagerInterface 接口
func (fs *FileService) SendFile(conn net.Conn, filePath, toUser string, chunkSize int) error {
	if !fs.IsRunning() {
		return fmt.Errorf("文件服务未运行")
	}
	return fs.transmissionManager.SendFile(conn, filePath, toUser, chunkSize)
}

func (fs *FileService) ReceiveFile(conn net.Conn, savePath string) error {
	if !fs.IsRunning() {
		return fmt.Errorf("文件服务未运行")
	}
	return fs.transmissionManager.ReceiveFile(conn, savePath)
}

func (fs *FileService) CancelSession(fileID string) error {
	return fs.transmissionManager.CancelSession(fileID)
}

func (fs *FileService) ListSessions() []string {
	return fs.transmissionManager.ListSessions()
}

// SendFileWithProgress 发送文件并跟踪进度
func (fs *FileService) SendFileWithProgress(conn net.Conn, filePath, toUser string, progressCallback func(float64)) error {
	if !fs.IsRunning() {
		return fmt.Errorf("文件服务未运行")
	}

	// 设置进度回调
	sessionID := fmt.Sprintf("%s_%d", filePath, time.Now().Unix())
	fs.progressTracker.SetCallback(func(sid string, progress float64) {
		if sid == sessionID && progressCallback != nil {
			progressCallback(progress)
		}
	})

	return fs.transmissionManager.SendFile(conn, filePath, toUser, fs.config.ChunkSize)
}

// ReceiveFileWithProgress 接收文件并跟踪进度
func (fs *FileService) ReceiveFileWithProgress(conn net.Conn, savePath string, progressCallback func(float64)) error {
	if !fs.IsRunning() {
		return fmt.Errorf("文件服务未运行")
	}

	// 设置进度回调
	sessionID := fmt.Sprintf("%s_%d", savePath, time.Now().Unix())
	fs.progressTracker.SetCallback(func(sid string, progress float64) {
		if sid == sessionID && progressCallback != nil {
			progressCallback(progress)
		}
	})

	return fs.transmissionManager.ReceiveFile(conn, savePath)
}

// GetTransferProgress 获取传输进度
func (fs *FileService) GetTransferProgress(sessionID string) float64 {
	return fs.progressTracker.GetProgress(sessionID)
}

// ValidateConfig 验证配置
func (fs *FileService) ValidateConfig() error {
	if fs.config.ChunkSize <= 0 {
		return fmt.Errorf("分片大小必须大于0")
	}
	if fs.config.WindowSize <= 0 {
		return fmt.Errorf("窗口大小必须大于0")
	}
	if fs.config.Timeout <= 0 {
		return fmt.Errorf("超时时间必须大于0")
	}
	if fs.config.MaxFileSize <= 0 {
		return fmt.Errorf("最大文件大小必须大于0")
	}
	return nil
}