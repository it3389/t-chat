package file

import (
	"sync"
	"time"
)

// WindowConfig 滑动窗口配置
type WindowConfig struct {
	WindowSize   int           // 窗口大小
	MinChunkSize int           // 最小分片大小
	MaxChunkSize int           // 最大分片大小
	Timeout      time.Duration // 超时时间
	RetryCount   int           // 重试次数
	AdaptiveRate float64       // 自适应速率
}

// NewWindowConfig 创建窗口配置
func NewWindowConfig() *WindowConfig {
	return &WindowConfig{
		WindowSize:   DefaultWindowSize,
		MinChunkSize: 1024,        // 1KB
		MaxChunkSize:    17 * 1024, // 17KB - 最大安全分片大小，确保Base64编码后不超过32KB
		Timeout:      DefaultTimeout,
		RetryCount:   DefaultRetryCount,
		AdaptiveRate: 0.1, // 10%自适应速率
	}
}

// SlidingWindow 滑动窗口
type SlidingWindow struct {
	windowSize  int
	sentChunks  map[int]*Chunk
	ackedChunks map[int]bool
	baseIndex   int
	nextIndex   int
	mu          sync.RWMutex
}

// NewSlidingWindow 创建滑动窗口
func NewSlidingWindow(windowSize int) *SlidingWindow {
	return &SlidingWindow{
		windowSize:  windowSize,
		sentChunks:  make(map[int]*Chunk),
		ackedChunks: make(map[int]bool),
		baseIndex:   0,
		nextIndex:   0,
	}
}

// IsWindowFull 检查窗口是否已满
func (sw *SlidingWindow) IsWindowFull() bool {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	return len(sw.sentChunks) >= sw.windowSize
}

// IsAllAcked 检查是否所有分片都已确认
func (sw *SlidingWindow) IsAllAcked() bool {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	return len(sw.sentChunks) == 0
}

// AddChunk 添加分片到窗口
func (sw *SlidingWindow) AddChunk(chunk *Chunk) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.sentChunks[chunk.Index] = chunk
}

// MarkAcked 标记分片为已确认
func (sw *SlidingWindow) MarkAcked(chunkIndex int) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	delete(sw.sentChunks, chunkIndex)
	sw.ackedChunks[chunkIndex] = true

	// 滑动窗口
	sw.slideWindow()
}

// GetUnackedChunks 获取未确认的分片
func (sw *SlidingWindow) GetUnackedChunks() []*Chunk {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	var chunks []*Chunk
	for _, chunk := range sw.sentChunks {
		chunks = append(chunks, chunk)
	}
	return chunks
}

// GetWindowSize 获取窗口大小
func (sw *SlidingWindow) GetWindowSize() int {
	return sw.windowSize
}

// SetWindowSize 设置窗口大小
func (sw *SlidingWindow) SetWindowSize(size int) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.windowSize = size
}

// GetBaseIndex 获取基础索引
func (sw *SlidingWindow) GetBaseIndex() int {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	return sw.baseIndex
}

// GetNextIndex 获取下一个索引
func (sw *SlidingWindow) GetNextIndex() int {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	return sw.nextIndex
}

// slideWindow 滑动窗口
func (sw *SlidingWindow) slideWindow() {
	// 找到最小的未确认分片索引
	minIndex := -1
	for index := range sw.sentChunks {
		if minIndex == -1 || index < minIndex {
			minIndex = index
		}
	}

	if minIndex != -1 {
		sw.baseIndex = minIndex
	}
}

// Reset 重置窗口
func (sw *SlidingWindow) Reset() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.sentChunks = make(map[int]*Chunk)
	sw.ackedChunks = make(map[int]bool)
	sw.baseIndex = 0
	sw.nextIndex = 0
}

// GetStats 获取窗口统计信息
func (sw *SlidingWindow) GetStats() map[string]interface{} {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	return map[string]interface{}{
		"window_size":  sw.windowSize,
		"sent_chunks":  len(sw.sentChunks),
		"acked_chunks": len(sw.ackedChunks),
		"base_index":   sw.baseIndex,
		"next_index":   sw.nextIndex,
		"window_full":  len(sw.sentChunks) >= sw.windowSize,
		"all_acked":    len(sw.sentChunks) == 0,
	}
}
