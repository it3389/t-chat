package file

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileReceiver 负责接收文件分片并重组为完整文件，支持断点续�?
type FileReceiver struct {
	OutPath   string         // 输出文件路径
	TotalSize int64          // 文件总大�?
	Chunks    map[int]*Chunk // 已接收分�?
	Mutex     sync.Mutex     // 并发保护
}

// NewFileReceiver 创建文件接收�?
func NewFileReceiver(outPath string, totalSize int64) *FileReceiver {
	return &FileReceiver{
		OutPath:   outPath,
		TotalSize: totalSize,
		Chunks:    make(map[int]*Chunk),
	}
}

// ReceiveChunk 接收单个分片，校验并保存
func (fr *FileReceiver) ReceiveChunk(chunk *Chunk) error {
	fr.Mutex.Lock()
	defer fr.Mutex.Unlock()
	if !VerifyChunk(chunk) {
		return ErrChunkCorrupted
	}
	fr.Chunks[chunk.Index] = chunk
	return nil
}

// SaveToFile 将所有分片重组成完整文件
func (fr *FileReceiver) SaveToFile() error {
	file, err := os.Create(fr.OutPath)
	if err != nil {
		return err
	}
	defer file.Close()
	for i := 0; ; i++ {
		chunk, ok := fr.Chunks[i]
		if !ok {
			break
		}
		if _, err := file.WriteAt(chunk.Data, chunk.Offset); err != nil {
			return err
		}
	}
	return nil
}

// GetProgress 获取当前接收进度（已接收分片�?总分片数�?
func (fr *FileReceiver) GetProgress(chunkSize int) (received, total int) {
	fr.Mutex.Lock()
	defer fr.Mutex.Unlock()
	total = int((fr.TotalSize + int64(chunkSize) - 1) / int64(chunkSize))
	received = len(fr.Chunks)
	return
}

// ErrChunkCorrupted 分片校验失败错误
var ErrChunkCorrupted = &ChunkError{"分片数据校验失败"}

type ChunkError struct {
	Msg string
}

func (e *ChunkError) Error() string { return e.Msg }

// Receiver 文件接收器
// 实现 ReceiverInterface 接口
type Receiver struct {
	fileID      string
	savePath    string
	fileSize    int64
	chunkSize   int
	resumePoint int
	file        *os.File
	logger      Logger
}

// 确保 Receiver 实现了 ReceiverInterface 接口
var _ ReceiverInterface = (*Receiver)(nil)

// NewReceiver 创建文件接收�?
func NewReceiver(fileID, savePath string, fileSize int64, chunkSize int, logger Logger) *Receiver {
	// 创建保存目录
	saveDir := filepath.Dir(savePath)
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		logger.Errorf("创建保存目录失败: save_dir=%s, error=%v", saveDir, err)
		return nil
	}

	// 创建或打开文件
	file, err := os.OpenFile(savePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Errorf("创建文件失败: save_path=%s, error=%v", savePath, err)
		return nil
	}

	return &Receiver{
		fileID:      fileID,
		savePath:    savePath,
		fileSize:    fileSize,
		chunkSize:   chunkSize,
		resumePoint: 0,
		file:        file,
		logger:      logger,
	}
}

// WriteChunk 写入分片
func (r *Receiver) WriteChunk(chunk *Chunk) error {
	// 计算写入位置
	writeOffset := int64(chunk.Index * r.chunkSize)

	// 写入数据
	if _, err := r.file.WriteAt(chunk.Data, writeOffset); err != nil {
		return fmt.Errorf("写入分片失败: %v", err)
	}

	r.logger.Debugf("写入分片成功: file_id=%s, chunk_index=%d, chunk_size=%d, write_offset=%d",
		r.fileID, chunk.Index, len(chunk.Data), writeOffset)

	return nil
}

// ReceiveChunk 接收分片（实现 ReceiverInterface 接口）
func (r *Receiver) ReceiveChunk(chunk *Chunk) error {
	// 验证分片
	if !VerifyChunk(chunk) {
		return fmt.Errorf("分片校验失败: index=%d", chunk.Index)
	}
	
	// 写入分片
	return r.WriteChunk(chunk)
}

// SetResumePoint 设置断点续传位置
func (r *Receiver) SetResumePoint(resumePoint int) {
	r.resumePoint = resumePoint
}

// Complete 完成文件接收
func (r *Receiver) Complete() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// FileID 获取文件ID
func (r *Receiver) FileID() string {
	return r.fileID
}

// SavePath 获取保存路径
func (r *Receiver) SavePath() string {
	return r.savePath
}

// FileSize 获取文件大小
func (r *Receiver) FileSize() int64 {
	return r.fileSize
}

// Close 关闭接收器（实现 ReceiverInterface 接口）
func (r *Receiver) Close() error {
	return r.Complete()
}

// GetProgress 获取进度（实现 ReceiverInterface 接口）
func (r *Receiver) GetProgress() float64 {
	if r.fileSize == 0 {
		return 0
	}
	
	// 获取当前文件大小
	if r.file != nil {
		if stat, err := r.file.Stat(); err == nil {
			return float64(stat.Size()) / float64(r.fileSize) * 100
		}
	}
	return 0
}

// IsComplete 检查是否完成（实现 ReceiverInterface 接口）
func (r *Receiver) IsComplete() bool {
	if r.file != nil {
		if stat, err := r.file.Stat(); err == nil {
			return stat.Size() >= r.fileSize
		}
	}
	return false
}

// GetFilePath 获取文件路径（实现 ReceiverInterface 接口）
func (r *Receiver) GetFilePath() string {
	return r.savePath
}

// 检查并修复所有非法字符，确保为合法 UTF-8 编码
