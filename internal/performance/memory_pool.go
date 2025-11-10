package performance

import (
	"bytes"
	"encoding/json"
	"sync"
)

// 内存池管理器，提供统一的内存分配和回收
type MemoryPoolManager struct {
	// 不同大小的字节缓冲池
	smallBufferPool  *sync.Pool // 1KB
	mediumBufferPool *sync.Pool // 16KB
	largeBufferPool  *sync.Pool // 256KB
	
	// JSON编解码器池
	jsonEncoderPool *sync.Pool
	jsonDecoderPool *sync.Pool
	
	// 字符串构建器池
	stringBuilderPool *sync.Pool
	
	// 统计信息
	stats PoolStats
	mutex sync.RWMutex
}

// PoolStats 内存池统计信息
type PoolStats struct {
	SmallBufferGets    int64 // 小缓冲区获取次数
	SmallBufferPuts    int64 // 小缓冲区归还次数
	MediumBufferGets   int64 // 中等缓冲区获取次数
	MediumBufferPuts   int64 // 中等缓冲区归还次数
	LargeBufferGets    int64 // 大缓冲区获取次数
	LargeBufferPuts    int64 // 大缓冲区归还次数
	JSONEncoderGets    int64 // JSON编码器获取次数
	JSONEncoderPuts    int64 // JSON编码器归还次数
	JSONDecoderGets    int64 // JSON解码器获取次数
	JSONDecoderPuts    int64 // JSON解码器归还次数
	StringBuilderGets  int64 // 字符串构建器获取次数
	StringBuilderPuts  int64 // 字符串构建器归还次数
}

// 全局内存池管理器实例
var globalMemoryPool *MemoryPoolManager
var once sync.Once

// GetMemoryPool 获取全局内存池管理器实例
func GetMemoryPool() *MemoryPoolManager {
	once.Do(func() {
		globalMemoryPool = NewMemoryPoolManager()
	})
	return globalMemoryPool
}

// NewMemoryPoolManager 创建新的内存池管理器
func NewMemoryPoolManager() *MemoryPoolManager {
	return &MemoryPoolManager{
		smallBufferPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 1024) // 1KB初始容量
			},
		},
		mediumBufferPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 16*1024) // 16KB初始容量
			},
		},
		largeBufferPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 256*1024) // 256KB初始容量
			},
		},
		jsonEncoderPool: &sync.Pool{
			New: func() interface{} {
				return &bytes.Buffer{}
			},
		},
		jsonDecoderPool: &sync.Pool{
			New: func() interface{} {
				return &bytes.Buffer{}
			},
		},
		stringBuilderPool: &sync.Pool{
			New: func() interface{} {
				return &bytes.Buffer{}
			},
		},
	}
}

// GetSmallBuffer 获取小缓冲区 (1KB)
func (mpm *MemoryPoolManager) GetSmallBuffer() []byte {
	mpm.mutex.Lock()
	mpm.stats.SmallBufferGets++
	mpm.mutex.Unlock()
	
	buf := mpm.smallBufferPool.Get().([]byte)
	return buf[:0] // 重置长度但保留容量
}

// PutSmallBuffer 归还小缓冲区
func (mpm *MemoryPoolManager) PutSmallBuffer(buf []byte) {
	if cap(buf) <= 2*1024 { // 限制最大容量为2KB
		mpm.mutex.Lock()
		mpm.stats.SmallBufferPuts++
		mpm.mutex.Unlock()
		
		mpm.smallBufferPool.Put(buf)
	}
}

// GetMediumBuffer 获取中等缓冲区 (16KB)
func (mpm *MemoryPoolManager) GetMediumBuffer() []byte {
	mpm.mutex.Lock()
	mpm.stats.MediumBufferGets++
	mpm.mutex.Unlock()
	
	buf := mpm.mediumBufferPool.Get().([]byte)
	return buf[:0] // 重置长度但保留容量
}

// PutMediumBuffer 归还中等缓冲区
func (mpm *MemoryPoolManager) PutMediumBuffer(buf []byte) {
	if cap(buf) <= 32*1024 { // 限制最大容量为32KB
		mpm.mutex.Lock()
		mpm.stats.MediumBufferPuts++
		mpm.mutex.Unlock()
		
		mpm.mediumBufferPool.Put(buf)
	}
}

// GetLargeBuffer 获取大缓冲区 (256KB)
func (mpm *MemoryPoolManager) GetLargeBuffer() []byte {
	mpm.mutex.Lock()
	mpm.stats.LargeBufferGets++
	mpm.mutex.Unlock()
	
	buf := mpm.largeBufferPool.Get().([]byte)
	return buf[:0] // 重置长度但保留容量
}

// PutLargeBuffer 归还大缓冲区
func (mpm *MemoryPoolManager) PutLargeBuffer(buf []byte) {
	if cap(buf) <= 512*1024 { // 限制最大容量为512KB
		mpm.mutex.Lock()
		mpm.stats.LargeBufferPuts++
		mpm.mutex.Unlock()
		
		mpm.largeBufferPool.Put(buf)
	}
}

// GetJSONEncoder 获取JSON编码器
func (mpm *MemoryPoolManager) GetJSONEncoder() *bytes.Buffer {
	mpm.mutex.Lock()
	mpm.stats.JSONEncoderGets++
	mpm.mutex.Unlock()
	
	buf := mpm.jsonEncoderPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutJSONEncoder 归还JSON编码器
func (mpm *MemoryPoolManager) PutJSONEncoder(buf *bytes.Buffer) {
	if buf.Cap() <= 64*1024 { // 限制最大容量为64KB
		mpm.mutex.Lock()
		mpm.stats.JSONEncoderPuts++
		mpm.mutex.Unlock()
		
		mpm.jsonEncoderPool.Put(buf)
	}
}

// GetJSONDecoder 获取JSON解码器
func (mpm *MemoryPoolManager) GetJSONDecoder() *bytes.Buffer {
	mpm.mutex.Lock()
	mpm.stats.JSONDecoderGets++
	mpm.mutex.Unlock()
	
	buf := mpm.jsonDecoderPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutJSONDecoder 归还JSON解码器
func (mpm *MemoryPoolManager) PutJSONDecoder(buf *bytes.Buffer) {
	if buf.Cap() <= 64*1024 { // 限制最大容量为64KB
		mpm.mutex.Lock()
		mpm.stats.JSONDecoderPuts++
		mpm.mutex.Unlock()
		
		mpm.jsonDecoderPool.Put(buf)
	}
}

// GetStringBuilder 获取字符串构建器
func (mpm *MemoryPoolManager) GetStringBuilder() *bytes.Buffer {
	mpm.mutex.Lock()
	mpm.stats.StringBuilderGets++
	mpm.mutex.Unlock()
	
	buf := mpm.stringBuilderPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutStringBuilder 归还字符串构建器
func (mpm *MemoryPoolManager) PutStringBuilder(buf *bytes.Buffer) {
	if buf.Cap() <= 16*1024 { // 限制最大容量为16KB
		mpm.mutex.Lock()
		mpm.stats.StringBuilderPuts++
		mpm.mutex.Unlock()
		
		mpm.stringBuilderPool.Put(buf)
	}
}

// MarshalJSONOptimized 优化的JSON序列化
func (mpm *MemoryPoolManager) MarshalJSONOptimized(v interface{}) ([]byte, error) {
	buf := mpm.GetJSONEncoder()
	defer mpm.PutJSONEncoder(buf)
	
	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	
	// 移除末尾的换行符
	data := buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	
	// 复制数据，因为buf会被重用
	result := make([]byte, len(data))
	copy(result, data)
	
	return result, nil
}

// UnmarshalJSONOptimized 优化的JSON反序列化
func (mpm *MemoryPoolManager) UnmarshalJSONOptimized(data []byte, v interface{}) error {
	buf := mpm.GetJSONDecoder()
	defer mpm.PutJSONDecoder(buf)
	
	buf.Write(data)
	decoder := json.NewDecoder(buf)
	return decoder.Decode(v)
}

// GetStats 获取内存池统计信息
func (mpm *MemoryPoolManager) GetStats() PoolStats {
	mpm.mutex.RLock()
	defer mpm.mutex.RUnlock()
	return mpm.stats
}

// ResetStats 重置统计信息
func (mpm *MemoryPoolManager) ResetStats() {
	mpm.mutex.Lock()
	defer mpm.mutex.Unlock()
	mpm.stats = PoolStats{}
}

// GetBufferBySize 根据大小获取合适的缓冲区
func (mpm *MemoryPoolManager) GetBufferBySize(size int) []byte {
	switch {
	case size <= 1024:
		return mpm.GetSmallBuffer()
	case size <= 16*1024:
		return mpm.GetMediumBuffer()
	default:
		return mpm.GetLargeBuffer()
	}
}

// PutBufferBySize 根据大小归还合适的缓冲区
func (mpm *MemoryPoolManager) PutBufferBySize(buf []byte) {
	capacity := cap(buf)
	switch {
	case capacity <= 2*1024:
		mpm.PutSmallBuffer(buf)
	case capacity <= 32*1024:
		mpm.PutMediumBuffer(buf)
	default:
		mpm.PutLargeBuffer(buf)
	}
}