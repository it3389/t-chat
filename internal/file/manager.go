package file

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"t-chat/internal/util"
)

// FileInfo 文件信息
type FileInfo struct {
	Name        string    `json:"name"`         // 原始文件名
	UniqueName  string    `json:"unique_name"`  // 唯一文件名
	Size        int64     `json:"size"`         // 文件大小
	Checksum    string    `json:"checksum"`     // 文件校验和
	ReceivedAt  time.Time `json:"received_at"`  // 接收时间
	From        string    `json:"from"`         // 发送方
	Status      string    `json:"status"`       // 状态：received, processing, completed, failed
	Chunks      int       `json:"chunks"`       // 总分片数
	ReceivedChunks int    `json:"received_chunks"` // 已接收分片数
	Progress    float64   `json:"progress"`     // 进度（0-100）
	Metadata    map[string]interface{} `json:"metadata"` // 元数据
}

// FileTransferSession 文件传输会话信息
type FileTransferSession struct {
	ID             string                 `json:"id"`              // 会话ID
	FileID         string                 `json:"file_id"`         // 文件ID
	Type           string                 `json:"type"`            // 传输类型：send, receive
	FileName       string                 `json:"file_name"`       // 文件名
	FileSize       int64                  `json:"file_size"`       // 文件大小
	Checksum       string                 `json:"checksum"`        // 文件校验和
	From           string                 `json:"from"`            // 发送方
	To             string                 `json:"to"`              // 接收方
	Status         string                 `json:"status"`          // 会话状态
	Progress       float64                `json:"progress"`        // 进度
	StartTime      time.Time              `json:"start_time"`      // 开始时间
	EndTime        time.Time              `json:"end_time"`        // 结束时间
	Chunks         map[int]*ChunkInfo     `json:"chunks"`          // 分片信息
	TotalChunks    int                    `json:"total_chunks"`    // 总分片数
	ReceivedChunks int                    `json:"received_chunks"` // 已接收分片数
	Metadata       map[string]interface{} `json:"metadata"`        // 元数据
}

// ChunkInfo 分片信息
type ChunkInfo struct {
	Index      int       `json:"index"`       // 分片索引
	Size       int       `json:"size"`        // 分片大小
	Checksum   string    `json:"checksum"`    // 分片校验和
	Status     string    `json:"status"`      // 分片状态
	SendTime   time.Time `json:"send_time"`   // 发送时间
	RecvTime   time.Time `json:"recv_time"`   // 接收时间
	RetryCount int       `json:"retry_count"` // 重试次数
}

// TransferStats 传输统计信息
type TransferStats struct {
	TotalFiles        int64         `json:"total_files"`
	TotalSize         int64         `json:"total_size"`
	CompletedFiles    int64         `json:"completed_files"`
	FailedFiles       int64         `json:"failed_files"`
	AverageSpeed      float64       `json:"average_speed"`      // 字节/秒
	LastTransferTime  time.Time     `json:"last_transfer_time"`
}

// Manager 文件管理器，实现 FileManagerInterface 接口
// 负责文件的存储、检索、会话管理和统计信息
type Manager struct {
	files           map[string]*FileInfo              // 文件信息映射
	sessions        map[string]*FileTransferSession   // 传输会话映射
	chunkCache      map[string]map[int][]byte         // 分片数据缓存: sessionID -> chunkIndex -> data
	mutex           sync.RWMutex                      // 文件操作锁
	filePath        string                            // 文件信息存储路径
	sessionPath     string                            // 会话信息存储路径
	dirPath         string                            // 文件存储目录
	stats           *TransferStats                    // 传输统计信息
	statsMutex      sync.RWMutex                      // 统计信息锁
}

// 确保 Manager 实现了 FileManagerInterface 接口
var _ FileManagerInterface = (*Manager)(nil)

// NewManager 创建新的文件管理器
func NewManager() *Manager {
	m := &Manager{
		files:       make(map[string]*FileInfo),
		sessions:    make(map[string]*FileTransferSession),
		chunkCache:  make(map[string]map[int][]byte),
		filePath:    "files/files.json",
		sessionPath: "files/sessions.json",
		dirPath:     "files",
		stats:       &TransferStats{},
	}

	// 确保目录存在
	os.MkdirAll(m.dirPath, 0755)

	// 加载现有文件信息和会话
	m.loadFileInfo()
	m.loadSessions()

	return m
}

// 文件写入缓冲池 - 优化：减少缓冲区大小
var fileBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 16*1024) // 从32KB减少到16KB缓冲区
	},
}

// SaveFile 保存文件
func (m *Manager) SaveFile(fileName string, data []byte, from string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 计算文件校验和
	checksum := m.calculateChecksum(data)

	// 生成唯一文件名
	uniqueName := m.generateUniqueName(fileName)

	// 保存文件
	filePath := filepath.Join(m.dirPath, uniqueName)
	
	// 对于大文件，使用流式写入减少内存占用
	if len(data) > 1024*1024 { // 1MB以上的文件
		if err := m.writeFileStreaming(filePath, data); err != nil {
			return fmt.Errorf("流式写入文件失败: %v", err)
		}
	} else {
		// 小文件直接写入
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return fmt.Errorf("保存文件失败: %v", err)
		}
	}

	// 记录文件信息
	fileInfo := &FileInfo{
		Name:           fileName,
		UniqueName:     uniqueName,
		Size:           int64(len(data)),
		Checksum:       checksum,
		ReceivedAt:     time.Now(),
		From:           from,
		Status:         "completed",
		Chunks:         1, // 单次传输
		ReceivedChunks: 1,
		Progress:       100,
		Metadata:       make(map[string]interface{}),
	}

	m.files[uniqueName] = fileInfo

	// 注意：统计信息已在SaveFileChunk的updateTransferStats中更新，这里不需要重复更新

	return m.saveFileInfo()
}

// writeFileStreaming 流式写入大文件
func (m *Manager) writeFileStreaming(filePath string, data []byte) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// 对于小文件，直接写入
	if len(data) <= 1024*1024 { // 1MB以下
		_, err := file.Write(data)
		return err
	}
	
	// 对于大文件，使用缓冲写入
	buffer := fileBufferPool.Get().([]byte)
	defer fileBufferPool.Put(buffer)
	
	for i := 0; i < len(data); i += len(buffer) {
		end := i + len(buffer)
		if end > len(data) {
			end = len(data)
		}
		
		_, err := file.Write(data[i:end])
		if err != nil {
			return err
		}
	}
	
	return nil
}

// writeFileStreamingFromReader 从Reader流式写入文件，避免将整个文件加载到内存
func (m *Manager) writeFileStreamingFromReader(filePath string, reader io.Reader, fileSize int64) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	
	buffer := fileBufferPool.Get().([]byte)
	defer fileBufferPool.Put(buffer)
	
	_, err = io.CopyBuffer(file, reader, buffer)
	return err
}

// SaveFileChunk 保存文件分片
func (m *Manager) SaveFileChunk(sessionID string, chunkIndex int, data []byte, checksum string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 获取或创建会话
	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("会话不存在: %s", sessionID)
	}

	// 验证分片校验和
	if m.calculateChecksum(data) != checksum {
		return fmt.Errorf("分片校验和不匹配")
	}

	// 初始化分片缓存
	if m.chunkCache[sessionID] == nil {
		m.chunkCache[sessionID] = make(map[int][]byte)
	}

	// 保存分片数据
	m.chunkCache[sessionID][chunkIndex] = data

	// 创建分片信息
	chunk := &ChunkInfo{
		Index:    chunkIndex,
		Size:     len(data),
		Checksum: checksum,
		Status:   "received",
		RecvTime: time.Now(),
	}

	session.Chunks[chunkIndex] = chunk
	session.ReceivedChunks++

	// 更新进度
	session.Progress = float64(session.ReceivedChunks) * 100 / float64(session.TotalChunks)

	// 检查是否完成
	if session.ReceivedChunks >= session.TotalChunks {
		session.Status = "completed"
		session.EndTime = time.Now()
		session.Progress = 100

		// 组装完整文件
		if err := m.assembleFile(session); err != nil {
			return fmt.Errorf("组装文件失败: %w", err)
		}

		// 更新传输统计信息
		m.updateTransferStats(session)
	}

	// 保存会话信息
	return m.saveSessions()
}

// assembleFile 组装完整文件
func (m *Manager) assembleFile(session *FileTransferSession) error {
	// 创建临时文件
	tempPath := filepath.Join(m.dirPath, session.ID+".tmp")
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return err
	}

	// 检查分片缓存是否存在
	chunks, exists := m.chunkCache[session.ID]
	if !exists {
		tempFile.Close()
		return fmt.Errorf("分片缓存不存在: %s", session.ID)
	}

	// 按顺序写入分片数据
	for i := 0; i < session.TotalChunks; i++ {
		_, exists := session.Chunks[i]
		if !exists {
			tempFile.Close()
			return fmt.Errorf("缺少分片: %d", i)
		}

		// 从分片缓存中读取数据
		data, exists := chunks[i]
		if !exists {
			tempFile.Close()
			return fmt.Errorf("分片数据不存在: %d", i)
		}

		// 写入分片数据到临时文件
		_, err := tempFile.Write(data)
		if err != nil {
			tempFile.Close()
			return fmt.Errorf("写入分片失败: Index=%d, Error=%v", i, err)
		}
	}

	// 验证文件校验和 - 使用流式计算避免加载整个文件到内存
	tempFile.Seek(0, 0) // 重置文件指针
	calculatedChecksum, err := m.calculateChecksumFromFile(tempFile)
	if err != nil {
		tempFile.Close()
		return fmt.Errorf("计算校验和失败: %v", err)
	}

	if calculatedChecksum != session.Checksum {
		tempFile.Close()
		return fmt.Errorf("文件校验和不匹配: 期望=%s, 实际=%s", session.Checksum, calculatedChecksum)
	}

	// 显式关闭文件句柄，确保Windows系统可以重命名文件
	tempFile.Close()

	// 移动到最终位置
	finalPath := filepath.Join(m.dirPath, session.FileName)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}

	// 清理分片缓存
	delete(m.chunkCache, session.ID)

	// 创建文件信息
	fileInfo := &FileInfo{
		Name:           session.FileName,
		UniqueName:     session.FileName,
		Size:           session.FileSize,
		Checksum:       session.Checksum,
		ReceivedAt:     session.EndTime,
		From:           session.From,
		Status:         "completed",
		Chunks:         session.TotalChunks,
		ReceivedChunks: session.ReceivedChunks,
		Progress:       100,
		Metadata:       session.Metadata,
	}

	m.files[session.FileName] = fileInfo

	return nil
}

// GetFile 获取文件
func (m *Manager) GetFile(fileName string) ([]byte, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	filePath := filepath.Join(m.dirPath, fileName)
	return os.ReadFile(filePath)
}

// GetFileInfo 获取文件信息
func (m *Manager) GetFileInfo(fileName string) *FileInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.files[fileName]
}

// GetAllFiles 获取所有文件信息
func (m *Manager) GetAllFiles() []*FileInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	files := make([]*FileInfo, 0, len(m.files))
	for _, f := range m.files {
		files = append(files, f)
	}

	return files
}

// GetSession 获取传输会话
func (m *Manager) GetSession(sessionID string) *FileTransferSession {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.sessions[sessionID]
}

// GetAllSessions 获取所有会话
func (m *Manager) GetAllSessions() []*FileTransferSession {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	sessions := make([]*FileTransferSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}

	return sessions
}

// CreateSession 创建传输会话
func (m *Manager) CreateSession(sessionID, fileName string, fileSize int64, checksum string, from, to string, totalChunks int) *FileTransferSession {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	session := &FileTransferSession{
		ID:             sessionID,
		FileID:         sessionID,
		Type:           "receive",
		FileName:       fileName,
		FileSize:       fileSize,
		Checksum:       checksum,
		From:           from,
		To:             to,
		Status:         "active",
		Progress:       0,
		StartTime:      time.Now(),
		Chunks:         make(map[int]*ChunkInfo),
		TotalChunks:    totalChunks,
		ReceivedChunks: 0,
		Metadata:       make(map[string]interface{}),
	}

	m.sessions[sessionID] = session
	m.saveSessions()

	return session
}

// DeleteFile 删除文件
func (m *Manager) DeleteFile(fileName string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	filePath := filepath.Join(m.dirPath, fileName)
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除文件失败: %v", err)
	}

	delete(m.files, fileName)

	return m.saveFileInfo()
}

// updateTransferStats 更新传输统计信息
func (m *Manager) updateTransferStats(session *FileTransferSession) {
	m.statsMutex.Lock()
	defer m.statsMutex.Unlock()

	// 更新传输时间
	m.stats.LastTransferTime = session.EndTime

	// 计算传输速度（字节/秒）
	transferDuration := session.EndTime.Sub(session.StartTime).Seconds()
	if transferDuration > 0.1 { // 避免传输时间过短导致的异常高速度
		currentSpeed := float64(session.FileSize) / transferDuration
		
		// 使用加权平均计算平均速度，避免异常高速度
		if m.stats.CompletedFiles == 1 {
			// 第一个文件，直接设置
			m.stats.AverageSpeed = currentSpeed
		} else {
			// 使用指数移动平均，权重为0.3，避免单次异常值影响过大
			alpha := 0.3
			m.stats.AverageSpeed = alpha*currentSpeed + (1-alpha)*m.stats.AverageSpeed
		}
	}
}

// UpdateTransferStatsForTest 测试用的公开方法
func (m *Manager) UpdateTransferStatsForTest(session *FileTransferSession) {
	m.updateTransferStats(session)
}

// GetStats 获取统计信息
func (m *Manager) GetStats() *TransferStats {
	m.statsMutex.RLock()
	defer m.statsMutex.RUnlock()

	// 返回副本
	stats := *m.stats
	return &stats
}

// calculateChecksum 计算校验和
func (m *Manager) calculateChecksum(data []byte) string {
	return util.Crypto.CalculateChecksum(data)
}

// calculateChecksumFromFile 从文件流式计算校验和
func (m *Manager) calculateChecksumFromFile(file *os.File) (string, error) {
	hash := sha256.New()
	
	// 使用缓冲池中的缓冲区
	buffer := fileBufferPool.Get().([]byte)
	defer fileBufferPool.Put(buffer)
	
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
	
	// 统一使用hex编码，与util.CalculateChecksum保持一致
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// generateUniqueName 生成唯一文件名
func (m *Manager) generateUniqueName(originalName string) string {
	ext := filepath.Ext(originalName)
	base := originalName[:len(originalName)-len(ext)]

	uniqueName := originalName
	counter := 1

	for {
		if _, exists := m.files[uniqueName]; !exists {
			break
		}
		uniqueName = fmt.Sprintf("%s_%d%s", base, counter, ext)
		counter++
	}

	return uniqueName
}

// loadFileInfo 加载文件信息
func (m *Manager) loadFileInfo() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在是正常
		}
		return err
	}

	var files map[string]*FileInfo
	if err := json.Unmarshal(data, &files); err != nil {
		return err
	}

	m.files = files
	return nil
}

// saveFileInfo 保存文件信息
func (m *Manager) saveFileInfo() error {
	data, err := json.MarshalIndent(m.files, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.filePath, data, 0644)
}

// loadSessions 加载会话信息
func (m *Manager) loadSessions() error {
	data, err := os.ReadFile(m.sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在是正常
		}
		return err
	}

	var sessions map[string]*FileTransferSession
	if err := json.Unmarshal(data, &sessions); err != nil {
		return err
	}

	m.sessions = sessions
	return nil
}

// saveSessions 保存会话信息
func (m *Manager) saveSessions() error {
	data, err := json.MarshalIndent(m.sessions, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.sessionPath, data, 0644)
}
