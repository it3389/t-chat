package performance

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// JSONOptimizer JSON优化器，提供高效的JSON操作
type JSONOptimizer struct {
	pool *MemoryPoolManager
	
	// 类型缓存，避免重复反射
	typeCache sync.Map
	
	// 编码器配置缓存
	encoderConfigs sync.Map
	
	// 统计信息
	stats JSONStats
	mutex sync.RWMutex
}

// JSONStats JSON操作统计信息
type JSONStats struct {
	MarshalCount     int64         // 序列化次数
	UnmarshalCount   int64         // 反序列化次数
	MarshalTime      time.Duration // 序列化总时间
	UnmarshalTime    time.Duration // 反序列化总时间
	MarshalErrors    int64         // 序列化错误次数
	UnmarshalErrors  int64         // 反序列化错误次数
	CacheHits        int64         // 缓存命中次数
	CacheMisses      int64         // 缓存未命中次数
}

// TypeInfo 类型信息缓存
type TypeInfo struct {
	Type        reflect.Type
	IsPointer   bool
	IsSlice     bool
	IsMap       bool
	IsStruct    bool
	FieldCount  int
	CacheKey    string
}

// EncoderConfig 编码器配置
type EncoderConfig struct {
	EscapeHTML   bool
	IndentPrefix string
	IndentValue  string
}

// 全局JSON优化器实例
var globalJSONOptimizer *JSONOptimizer
var jsonOptimizerOnce sync.Once

// GetJSONOptimizer 获取全局JSON优化器实例
func GetJSONOptimizer() *JSONOptimizer {
	jsonOptimizerOnce.Do(func() {
		globalJSONOptimizer = NewJSONOptimizer()
	})
	return globalJSONOptimizer
}

// NewJSONOptimizer 创建新的JSON优化器
func NewJSONOptimizer() *JSONOptimizer {
	return &JSONOptimizer{
		pool: GetMemoryPool(),
	}
}

// Marshal 优化的JSON序列化
func (jo *JSONOptimizer) Marshal(v interface{}) ([]byte, error) {
	start := time.Now()
	
	jo.mutex.Lock()
	jo.stats.MarshalCount++
	jo.mutex.Unlock()
	
	// 获取类型信息
	typeInfo := jo.getTypeInfo(v)
	
	// 根据类型选择最优的序列化策略
	var result []byte
	var err error
	
	switch {
	case typeInfo.IsStruct && typeInfo.FieldCount <= 5:
		// 小结构体使用快速序列化
		result, err = jo.marshalSmallStruct(v)
	case typeInfo.IsSlice:
		// 切片使用流式序列化
		result, err = jo.marshalSlice(v)
	case typeInfo.IsMap:
		// 映射使用优化序列化
		result, err = jo.marshalMap(v)
	default:
		// 默认序列化
		result, err = jo.marshalDefault(v)
	}
	
	// 更新统计信息
	jo.mutex.Lock()
	jo.stats.MarshalTime += time.Since(start)
	if err != nil {
		jo.stats.MarshalErrors++
	}
	jo.mutex.Unlock()
	
	return result, err
}

// Unmarshal 优化的JSON反序列化
func (jo *JSONOptimizer) Unmarshal(data []byte, v interface{}) error {
	start := time.Now()
	
	jo.mutex.Lock()
	jo.stats.UnmarshalCount++
	jo.mutex.Unlock()
	
	// 获取类型信息
	typeInfo := jo.getTypeInfo(v)
	
	// 根据类型选择最优的反序列化策略
	var err error
	
	switch {
	case typeInfo.IsPointer && typeInfo.Type.Elem().Kind() == reflect.Struct:
		// 结构体指针使用快速反序列化
		err = jo.unmarshalStruct(data, v)
	case typeInfo.IsSlice:
		// 切片使用流式反序列化
		err = jo.unmarshalSlice(data, v)
	case typeInfo.IsMap:
		// 映射使用优化反序列化
		err = jo.unmarshalMap(data, v)
	default:
		// 默认反序列化
		err = jo.unmarshalDefault(data, v)
	}
	
	// 更新统计信息
	jo.mutex.Lock()
	jo.stats.UnmarshalTime += time.Since(start)
	if err != nil {
		jo.stats.UnmarshalErrors++
	}
	jo.mutex.Unlock()
	
	return err
}

// MarshalIndent 优化的格式化JSON序列化
func (jo *JSONOptimizer) MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	// 使用缓存的编码器配置
	configKey := fmt.Sprintf("%s:%s", prefix, indent)
	config, exists := jo.encoderConfigs.Load(configKey)
	if !exists {
		config = &EncoderConfig{
			EscapeHTML:   false,
			IndentPrefix: prefix,
			IndentValue:  indent,
		}
		jo.encoderConfigs.Store(configKey, config)
		
		jo.mutex.Lock()
		jo.stats.CacheMisses++
		jo.mutex.Unlock()
	} else {
		jo.mutex.Lock()
		jo.stats.CacheHits++
		jo.mutex.Unlock()
	}
	
	buf := jo.pool.GetJSONEncoder()
	defer jo.pool.PutJSONEncoder(buf)
	
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(config.(*EncoderConfig).EscapeHTML)
	encoder.SetIndent(prefix, indent)
	
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	
	// 移除末尾的换行符
	data := buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	
	result := make([]byte, len(data))
	copy(result, data)
	
	return result, nil
}

// getTypeInfo 获取类型信息（带缓存）
func (jo *JSONOptimizer) getTypeInfo(v interface{}) *TypeInfo {
	t := reflect.TypeOf(v)
	cacheKey := t.String()
	
	if cached, exists := jo.typeCache.Load(cacheKey); exists {
		jo.mutex.Lock()
		jo.stats.CacheHits++
		jo.mutex.Unlock()
		return cached.(*TypeInfo)
	}
	
	info := &TypeInfo{
		Type:      t,
		IsPointer: t.Kind() == reflect.Ptr,
		IsSlice:   t.Kind() == reflect.Slice,
		IsMap:     t.Kind() == reflect.Map,
		IsStruct:  t.Kind() == reflect.Struct,
		CacheKey:  cacheKey,
	}
	
	if info.IsPointer && t.Elem().Kind() == reflect.Struct {
		info.IsStruct = true
		info.FieldCount = t.Elem().NumField()
	} else if info.IsStruct {
		info.FieldCount = t.NumField()
	}
	
	jo.typeCache.Store(cacheKey, info)
	
	jo.mutex.Lock()
	jo.stats.CacheMisses++
	jo.mutex.Unlock()
	
	return info
}

// marshalSmallStruct 小结构体快速序列化
func (jo *JSONOptimizer) marshalSmallStruct(v interface{}) ([]byte, error) {
	buf := jo.pool.GetJSONEncoder()
	defer jo.pool.PutJSONEncoder(buf)
	
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false) // 禁用HTML转义以提高性能
	
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	
	data := buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	
	result := make([]byte, len(data))
	copy(result, data)
	
	return result, nil
}

// marshalSlice 切片序列化
func (jo *JSONOptimizer) marshalSlice(v interface{}) ([]byte, error) {
	return jo.marshalDefault(v)
}

// marshalMap 映射序列化
func (jo *JSONOptimizer) marshalMap(v interface{}) ([]byte, error) {
	return jo.marshalDefault(v)
}

// marshalDefault 默认序列化
func (jo *JSONOptimizer) marshalDefault(v interface{}) ([]byte, error) {
	return jo.pool.MarshalJSONOptimized(v)
}

// unmarshalStruct 结构体反序列化
func (jo *JSONOptimizer) unmarshalStruct(data []byte, v interface{}) error {
	return jo.pool.UnmarshalJSONOptimized(data, v)
}

// unmarshalSlice 切片反序列化
func (jo *JSONOptimizer) unmarshalSlice(data []byte, v interface{}) error {
	return jo.pool.UnmarshalJSONOptimized(data, v)
}

// unmarshalMap 映射反序列化
func (jo *JSONOptimizer) unmarshalMap(data []byte, v interface{}) error {
	return jo.pool.UnmarshalJSONOptimized(data, v)
}

// unmarshalDefault 默认反序列化
func (jo *JSONOptimizer) unmarshalDefault(data []byte, v interface{}) error {
	return jo.pool.UnmarshalJSONOptimized(data, v)
}

// ValidateJSON 验证JSON格式
func (jo *JSONOptimizer) ValidateJSON(data []byte) bool {
	var temp interface{}
	return json.Unmarshal(data, &temp) == nil
}

// CompactJSON 压缩JSON（移除空白字符）
func (jo *JSONOptimizer) CompactJSON(data []byte) ([]byte, error) {
	buf := jo.pool.GetJSONEncoder()
	defer jo.pool.PutJSONEncoder(buf)
	
	if err := json.Compact(buf, data); err != nil {
		return nil, err
	}
	
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	
	return result, nil
}

// PrettyJSON 格式化JSON
func (jo *JSONOptimizer) PrettyJSON(data []byte) ([]byte, error) {
	var temp interface{}
	if err := json.Unmarshal(data, &temp); err != nil {
		return nil, err
	}
	
	return jo.MarshalIndent(temp, "", "  ")
}

// GetStats 获取JSON操作统计信息
func (jo *JSONOptimizer) GetStats() JSONStats {
	jo.mutex.RLock()
	defer jo.mutex.RUnlock()
	return jo.stats
}

// ResetStats 重置统计信息
func (jo *JSONOptimizer) ResetStats() {
	jo.mutex.Lock()
	defer jo.mutex.Unlock()
	jo.stats = JSONStats{}
}

// ClearCache 清空缓存
func (jo *JSONOptimizer) ClearCache() {
	jo.typeCache.Range(func(key, value interface{}) bool {
		jo.typeCache.Delete(key)
		return true
	})
	
	jo.encoderConfigs.Range(func(key, value interface{}) bool {
		jo.encoderConfigs.Delete(key)
		return true
	})
}

// GetCacheSize 获取缓存大小
func (jo *JSONOptimizer) GetCacheSize() (typeCache, encoderCache int) {
	jo.typeCache.Range(func(key, value interface{}) bool {
		typeCache++
		return true
	})
	
	jo.encoderConfigs.Range(func(key, value interface{}) bool {
		encoderCache++
		return true
	})
	
	return
}