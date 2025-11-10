package file

import (
	"crypto/sha256"
	"io"
	"os"
)

// Chunk 分片结构体，包含序号、偏移、大小、数据、校验和�?
type Chunk struct {
	Index  int    // 分片序号
	Offset int64  // 分片起始字节
	Size   int    // 分片大小
	Data   []byte // 分片数据
	Hash   []byte // 分片校验和（SHA256�?
	Done   bool   // 是否已完�?
}

// SplitFile 将文件切分为若干分片
func SplitFile(filePath string, chunkSize int) ([]*Chunk, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	totalSize := info.Size()
	var chunks []*Chunk
	for offset := int64(0); offset < totalSize; offset += int64(chunkSize) {
		end := offset + int64(chunkSize)
		if end > totalSize {
			end = totalSize
		}
		sz := int(end - offset)
		buf := make([]byte, sz)
		_, err := file.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return nil, err
		}
		hash := sha256.Sum256(buf)
		chunks = append(chunks, &Chunk{
			Index:  int(offset / int64(chunkSize)),
			Offset: offset,
			Size:   sz,
			Data:   buf,
			Hash:   hash[:],
			Done:   false,
		})
	}
	return chunks, nil
}

// MergeChunks 将分片重组成完整文件
func MergeChunks(chunks []*Chunk, outPath string) error {
	file, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, c := range chunks {
		if _, err := file.WriteAt(c.Data, c.Offset); err != nil {
			return err
		}
	}
	return nil
}

// VerifyChunk 校验分片数据完整�?
func VerifyChunk(c *Chunk) bool {
	hash := sha256.Sum256(c.Data)
	return string(hash[:]) == string(c.Hash)
}

// ChunkManager 分片管理器，实现 ChunkManagerInterface 接口
// 负责文件分片的读取和管理
type ChunkManager struct {
	fileID       string // 文件ID
	filePath     string // 文件路径
	chunkSize    int    // 分片大小
	totalChunks  int    // 总分片数
	currentChunk int    // 当前分片索引
	file         *os.File // 文件句柄
	logger       Logger // 日志记录器
}

// 确保 ChunkManager 实现了 ChunkManagerInterface 接口
var _ ChunkManagerInterface = (*ChunkManager)(nil)

// NewChunkManager 创建分片管理器
func NewChunkManager(fileID, filePath string, chunkSize, totalChunks int, logger Logger) *ChunkManager {
	file, err := os.Open(filePath)
	if err != nil {
		logger.Errorf("打开文件失败: file_path=%s, error=%v", filePath, err)
		return nil
	}

	return &ChunkManager{
		fileID:       fileID,
		filePath:     filePath,
		chunkSize:    chunkSize,
		totalChunks:  totalChunks,
		currentChunk: 0,
		file:         file,
		logger:       logger,
	}
}

// NextChunk 读取下一个分�?
func (cm *ChunkManager) NextChunk() (*Chunk, error) {
	if cm.currentChunk >= cm.totalChunks {
		return nil, io.EOF
	}

	offset := int64(cm.currentChunk * cm.chunkSize)
	buffer := make([]byte, cm.chunkSize)

	n, err := cm.file.ReadAt(buffer, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}

	// 调整缓冲区大小为实际读取的字节数
	buffer = buffer[:n]

	// 计算原始数据的哈希值（不进行任何数据清理）
	hash := sha256.Sum256(buffer)
	chunk := &Chunk{
		Index:  cm.currentChunk,
		Offset: offset,
		Size:   n,
		Data:   buffer, // 保持原始二进制数据不变
		Hash:   hash[:],
		Done:   false,
	}

	cm.currentChunk++
	return chunk, nil
}

// CurrentChunk 获取当前分片索引
func (cm *ChunkManager) CurrentChunk() int {
	return cm.currentChunk
}

// TotalChunks 获取总分片数
func (cm *ChunkManager) TotalChunks() int {
	return cm.totalChunks
}

// FileID 获取文件ID
func (cm *ChunkManager) FileID() string {
	return cm.fileID
}

// SetResumePoint 设置断点续传位置
func (cm *ChunkManager) SetResumePoint(resumePoint int) {
	cm.currentChunk = resumePoint
}

// Close 关闭文件
func (cm *ChunkManager) Close() error {
	if cm.file != nil {
		return cm.file.Close()
	}
	return nil
}
