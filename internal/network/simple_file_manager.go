package network

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"t-chat/internal/file"
)

// SimpleFileManager 简单文件管理器实现
type SimpleFileManager struct {
	dataDir string
	tempDir string
	logger  Logger
	mu      sync.RWMutex
	
	// 文件分片缓存
	chunkCache map[string]map[int][]byte // fileID -> chunkIndex -> data
	fileMeta   map[string]*FileMetadata  // fileID -> metadata
}

// FileMetadata 文件元数据
type FileMetadata struct {
	FileID       string    `json:"file_id"`
	FileName     string    `json:"file_name"`
	FileSize     int64     `json:"file_size"`
	ChunkCount   int       `json:"chunk_count"`
	Checksum     string    `json:"checksum"`     // 文件校验和
	ReceivedChunks map[int]bool `json:"received_chunks"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Completed    bool      `json:"completed"`
}

// NewSimpleFileManager 创建简单文件管理器
func NewSimpleFileManager(dataDir, tempDir string, logger Logger) *SimpleFileManager {
	// 确保目录存在
	os.MkdirAll(dataDir, 0755)
	os.MkdirAll(tempDir, 0755)
	
	return &SimpleFileManager{
		dataDir:    dataDir,
		tempDir:    tempDir,
		logger:     logger,
		chunkCache: make(map[string]map[int][]byte),
		fileMeta:   make(map[string]*FileMetadata),
	}
}

// SaveFile 保存文件
func (sfm *SimpleFileManager) saveFileInternal(fileID, fileName string, data []byte) error {
	sfm.mu.Lock()
	defer sfm.mu.Unlock()
	
	filePath := filepath.Join(sfm.dataDir, fileName)
	err := os.WriteFile(filePath, data, 0644)
	if err != nil {
		sfm.logger.Errorf("[SimpleFileManager] 保存文件失败: %s, Error: %v", filePath, err)
		return err
	}
	
	sfm.logger.Infof("[SimpleFileManager] 文件保存成功: %s", filePath)
	return nil
}

// LoadFile 加载文件
func (sfm *SimpleFileManager) LoadFile(fileID, fileName string) ([]byte, error) {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	filePath := filepath.Join(sfm.dataDir, fileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		sfm.logger.Errorf("[SimpleFileManager] 加载文件失败: %s, Error: %v", filePath, err)
		return nil, err
	}
	
	sfm.logger.Debugf("[SimpleFileManager] 文件加载成功: %s, Size: %d", filePath, len(data))
	return data, nil
}

// DeleteFile 删除文件
func (sfm *SimpleFileManager) deleteFileInternal(fileID, fileName string) error {
	sfm.mu.Lock()
	defer sfm.mu.Unlock()
	
	filePath := filepath.Join(sfm.dataDir, fileName)
	err := os.Remove(filePath)
	if err != nil {
		sfm.logger.Errorf("[SimpleFileManager] 删除文件失败: %s, Error: %v", filePath, err)
		return err
	}
	
	sfm.logger.Infof("[SimpleFileManager] 文件删除成功: %s", filePath)
	return nil
}

// FileExists 检查文件是否存在
func (sfm *SimpleFileManager) FileExists(fileID, fileName string) bool {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	filePath := filepath.Join(sfm.dataDir, fileName)
	_, err := os.Stat(filePath)
	return err == nil
}

// GetFileInfo 获取文件信息
func (sfm *SimpleFileManager) getFileInfoInternal(fileID, fileName string) (*file.FileInfo, error) {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	filePath := filepath.Join(sfm.dataDir, fileName)
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	
	return &file.FileInfo{
		Name:        fileName,
		UniqueName:  fileID,
		Size:        stat.Size(),
		ReceivedAt:  stat.ModTime(),
		Status:      "completed",
		Progress:    100.0,
		Metadata:    make(map[string]interface{}),
	}, nil
}

// SaveFileChunk 保存文件分片
func (sfm *SimpleFileManager) saveFileChunkInternal(fileID string, chunkIndex int, data []byte, hash string) error {
	sfm.mu.Lock()
	defer sfm.mu.Unlock()
	
	// 验证分片哈希
	hashBytes := sha256.Sum256(data)
	calculatedHash := hex.EncodeToString(hashBytes[:])
	if hash != "" && calculatedHash != hash {
		sfm.logger.Errorf("[SimpleFileManager] 分片哈希校验失败: FileID=%s, Index=%d, Expected=%s, Got=%s",
			fileID, chunkIndex, hash, calculatedHash)
		return fmt.Errorf("分片哈希校验失败")
	}
	
	// 初始化分片缓存
	if sfm.chunkCache[fileID] == nil {
		sfm.chunkCache[fileID] = make(map[int][]byte)
	}
	
	// 保存分片数据
	sfm.chunkCache[fileID][chunkIndex] = data
	
	// 更新元数据
	if sfm.fileMeta[fileID] == nil {
		sfm.fileMeta[fileID] = &FileMetadata{
			FileID:         fileID,
			ReceivedChunks: make(map[int]bool),
			CreatedAt:      time.Now(),
		}
	}
	
	meta := sfm.fileMeta[fileID]
	meta.ReceivedChunks[chunkIndex] = true
	meta.UpdatedAt = time.Now()
	
	sfm.logger.Debugf("[SimpleFileManager] 分片保存成功: FileID=%s, Index=%d, Size=%d",
		fileID, chunkIndex, len(data))
	
	return nil
}

// LoadFileChunk 加载文件分片
func (sfm *SimpleFileManager) LoadFileChunk(fileID string, chunkIndex int) ([]byte, error) {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	if sfm.chunkCache[fileID] == nil {
		return nil, fmt.Errorf("文件不存在: %s", fileID)
	}
	
	data, exists := sfm.chunkCache[fileID][chunkIndex]
	if !exists {
		return nil, fmt.Errorf("分片不存在: FileID=%s, Index=%d", fileID, chunkIndex)
	}
	
	sfm.logger.Debugf("[SimpleFileManager] 分片加载成功: FileID=%s, Index=%d, Size=%d",
		fileID, chunkIndex, len(data))
	
	return data, nil
}

// AssembleFile 组装文件
func (sfm *SimpleFileManager) AssembleFile(fileID, fileName string) error {
	sfm.mu.Lock()
	defer sfm.mu.Unlock()
	
	meta := sfm.fileMeta[fileID]
	if meta == nil {
		return fmt.Errorf("文件元数据不存在: %s", fileID)
	}
	
	chunks := sfm.chunkCache[fileID]
	if chunks == nil {
		return fmt.Errorf("文件分片不存在: %s", fileID)
	}
	
	// 检查所有分片是否都已接收
	for i := 0; i < meta.ChunkCount; i++ {
		if !meta.ReceivedChunks[i] {
			return fmt.Errorf("分片缺失: FileID=%s, Index=%d", fileID, i)
		}
	}
	
	// 创建输出文件
	filePath := filepath.Join(sfm.dataDir, fileName)
	outFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %v", err)
	}
	defer outFile.Close()
	
	// 按顺序写入分片并计算校验和
	hasher := sha256.New()
	for i := 0; i < meta.ChunkCount; i++ {
		data := chunks[i]
		_, err := outFile.Write(data)
		if err != nil {
			return fmt.Errorf("写入分片失败: Index=%d, Error=%v", i, err)
		}
		// 同时计算校验和
		hasher.Write(data)
	}
	
	// 验证文件校验和
	calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
	if meta.Checksum != "" && calculatedChecksum != meta.Checksum {
		// 删除损坏的文件
		os.Remove(filePath)
		return fmt.Errorf("文件校验和验证失败: FileID=%s, Expected=%s, Calculated=%s", 
			fileID, meta.Checksum, calculatedChecksum)
	}
	
	// 标记完成
	meta.Completed = true
	meta.UpdatedAt = time.Now()
	
	// 清理分片缓存
	delete(sfm.chunkCache, fileID)
	
	sfm.logger.Infof("[SimpleFileManager] 文件组装完成并校验通过: %s (校验和: %s)", filePath, calculatedChecksum)
	return nil
}

// GetFileMetadata 获取文件元数据
func (sfm *SimpleFileManager) GetFileMetadata(fileID string) *FileMetadata {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	return sfm.fileMeta[fileID]
}

// SetFileMetadata 设置文件元数据
func (sfm *SimpleFileManager) SetFileMetadata(fileID, fileName string, fileSize int64, chunkCount int, checksum string) {
	sfm.mu.Lock()
	defer sfm.mu.Unlock()
	
	sfm.fileMeta[fileID] = &FileMetadata{
		FileID:         fileID,
		FileName:       fileName,
		FileSize:       fileSize,
		ChunkCount:     chunkCount,
		Checksum:       checksum,
		ReceivedChunks: make(map[int]bool),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		Completed:      false,
	}
	
	sfm.logger.Debugf("[SimpleFileManager] 文件元数据已设置: FileID=%s, FileName=%s, Size=%d, ChunkCount=%d, Checksum=%s",
		fileID, fileName, fileSize, chunkCount, checksum)
}

// CleanupFile 清理文件相关数据
func (sfm *SimpleFileManager) CleanupFile(fileID string) {
	sfm.mu.Lock()
	defer sfm.mu.Unlock()
	
	delete(sfm.chunkCache, fileID)
	delete(sfm.fileMeta, fileID)
	
	sfm.logger.Debugf("[SimpleFileManager] 文件数据已清理: FileID=%s", fileID)
}

// GetTransferProgress 获取传输进度
func (sfm *SimpleFileManager) GetTransferProgress(fileID string) (received, total int) {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	meta := sfm.fileMeta[fileID]
	if meta == nil {
		return 0, 0
	}
	
	received = len(meta.ReceivedChunks)
	total = meta.ChunkCount
	return
}

// ListFiles 列出所有文件
func (sfm *SimpleFileManager) ListFiles() ([]string, error) {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	files, err := os.ReadDir(sfm.dataDir)
	if err != nil {
		return nil, err
	}
	
	var fileNames []string
	for _, file := range files {
		if !file.IsDir() {
			fileNames = append(fileNames, file.Name())
		}
	}
	
	return fileNames, nil
}

// GetDataDir 获取数据目录
func (sfm *SimpleFileManager) GetDataDir() string {
	return sfm.dataDir
}

// GetTempDir 获取临时目录
func (sfm *SimpleFileManager) GetTempDir() string {
	return sfm.tempDir
}

// CopyFile 复制文件
func (sfm *SimpleFileManager) CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	
	_, err = io.Copy(dstFile, srcFile)
	return err
}

// MoveFile 移动文件
func (sfm *SimpleFileManager) MoveFile(src, dst string) error {
	err := sfm.CopyFile(src, dst)
	if err != nil {
		return err
	}
	return os.Remove(src)
}

// 实现 file.FileManagerInterface 接口的方法

// SaveFile 保存文件（接口方法）
func (sfm *SimpleFileManager) SaveFile(fileName string, data []byte, from string) error {
	return sfm.saveFileInternal(fileName, fileName, data)
}

// GetFile 获取文件（接口方法）
func (sfm *SimpleFileManager) GetFile(fileName string) ([]byte, error) {
	return sfm.LoadFile(fileName, fileName)
}

// DeleteFile 删除文件（接口方法）
func (sfm *SimpleFileManager) DeleteFile(fileName string) error {
	return sfm.deleteFileInternal(fileName, fileName)
}

// GetFileInfo 获取文件信息（接口方法）
func (sfm *SimpleFileManager) GetFileInfo(fileName string) *file.FileInfo {
	info, err := sfm.getFileInfoInternal(fileName, fileName)
	if err != nil {
		return nil
	}
	return info
}

// GetAllFiles 获取所有文件
func (sfm *SimpleFileManager) GetAllFiles() []*file.FileInfo {
	files, err := sfm.ListFiles()
	if err != nil {
		return nil
	}
	
	var result []*file.FileInfo
	for _, fileName := range files {
		if info := sfm.GetFileInfo(fileName); info != nil {
			result = append(result, info)
		}
	}
	return result
}

// SaveFileChunk 保存文件分片（接口方法）
func (sfm *SimpleFileManager) SaveFileChunk(sessionID string, chunkIndex int, data []byte, checksum string) error {
	return sfm.saveFileChunkInternal(sessionID, chunkIndex, data, checksum)
}

// CreateSession 创建传输会话
func (sfm *SimpleFileManager) CreateSession(sessionID, fileName string, fileSize int64, checksum string, from, to string, totalChunks int) *file.FileTransferSession {
	sfm.mu.Lock()
	defer sfm.mu.Unlock()
	
	// 创建文件元数据
	sfm.SetFileMetadata(sessionID, fileName, fileSize, totalChunks, checksum)
	
	// 创建并返回会话
	session := &file.FileTransferSession{
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
		Chunks:         make(map[int]*file.ChunkInfo),
		TotalChunks:    totalChunks,
		ReceivedChunks: 0,
		Metadata:       make(map[string]interface{}),
	}
	
	return session
}

// GetSession 获取传输会话
func (sfm *SimpleFileManager) GetSession(sessionID string) *file.FileTransferSession {
	// 简单实现，返回基于元数据的会话信息
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	meta := sfm.fileMeta[sessionID]
	if meta == nil {
		return nil
	}
	
	received, total := sfm.GetTransferProgress(sessionID)
	progress := float64(received) * 100 / float64(total)
	
	status := "active"
	if meta.Completed {
		status = "completed"
	}
	
	session := &file.FileTransferSession{
		ID:             sessionID,
		FileID:         sessionID,
		Type:           "receive",
		FileName:       meta.FileName,
		FileSize:       meta.FileSize,
		Status:         status,
		Progress:       progress,
		StartTime:      meta.CreatedAt,
		TotalChunks:    meta.ChunkCount,
		ReceivedChunks: received,
		Chunks:         make(map[int]*file.ChunkInfo),
		Metadata:       make(map[string]interface{}),
	}
	
	return session
}

// GetAllSessions 获取所有会话
func (sfm *SimpleFileManager) GetAllSessions() []*file.FileTransferSession {
	sfm.mu.RLock()
	defer sfm.mu.RUnlock()
	
	var sessions []*file.FileTransferSession
	for sessionID := range sfm.fileMeta {
		if session := sfm.GetSession(sessionID); session != nil {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

// GetStats 获取传输统计信息
func (sfm *SimpleFileManager) GetStats() *file.TransferStats {
	return &file.TransferStats{
		TotalFiles:     int64(len(sfm.fileMeta)),
		CompletedFiles: 0, // 需要计算
		TotalSize:      0, // 需要计算
		FailedFiles:    0, // 需要计算
	}
}