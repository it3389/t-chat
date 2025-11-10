package performance

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
)

// StringOptimizer 字符串优化器，提供高效的字符串操作
type StringOptimizer struct {
	pool *MemoryPoolManager
}

// 全局字符串优化器实例
var globalStringOptimizer *StringOptimizer
var stringOptimizerOnce sync.Once

// GetStringOptimizer 获取全局字符串优化器实例
func GetStringOptimizer() *StringOptimizer {
	stringOptimizerOnce.Do(func() {
		globalStringOptimizer = NewStringOptimizer()
	})
	return globalStringOptimizer
}

// NewStringOptimizer 创建新的字符串优化器
func NewStringOptimizer() *StringOptimizer {
	return &StringOptimizer{
		pool: GetMemoryPool(),
	}
}

// ConcatStrings 高效拼接字符串
func (so *StringOptimizer) ConcatStrings(strs ...string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}
	
	// 计算总长度
	totalLen := 0
	for _, s := range strs {
		totalLen += len(s)
	}
	
	// 获取合适大小的缓冲区
	buf := so.pool.GetStringBuilder()
	defer so.pool.PutStringBuilder(buf)
	
	// 预分配容量
	buf.Grow(totalLen)
	
	// 拼接字符串
	for _, s := range strs {
		buf.WriteString(s)
	}
	
	return buf.String()
}

// FormatString 高效格式化字符串
func (so *StringOptimizer) FormatString(format string, args ...interface{}) string {
	buf := so.pool.GetStringBuilder()
	defer so.pool.PutStringBuilder(buf)
	
	// 预估容量（格式字符串长度的2倍）
	buf.Grow(len(format) * 2)
	
	fmt.Fprintf(buf, format, args...)
	return buf.String()
}

// JoinStrings 高效连接字符串切片
func (so *StringOptimizer) JoinStrings(strs []string, separator string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}
	
	// 计算总长度
	totalLen := 0
	for _, s := range strs {
		totalLen += len(s)
	}
	totalLen += len(separator) * (len(strs) - 1)
	
	buf := so.pool.GetStringBuilder()
	defer so.pool.PutStringBuilder(buf)
	
	buf.Grow(totalLen)
	
	for i, s := range strs {
		if i > 0 {
			buf.WriteString(separator)
		}
		buf.WriteString(s)
	}
	
	return buf.String()
}

// BuildString 使用构建器模式创建字符串
func (so *StringOptimizer) BuildString(fn func(*bytes.Buffer)) string {
	buf := so.pool.GetStringBuilder()
	defer so.pool.PutStringBuilder(buf)
	
	fn(buf)
	return buf.String()
}

// TrimAndLower 高效的字符串修剪和转小写
func (so *StringOptimizer) TrimAndLower(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

// SplitAndTrim 分割字符串并修剪空白
func (so *StringOptimizer) SplitAndTrim(s, separator string) []string {
	if s == "" {
		return nil
	}
	
	parts := strings.Split(s, separator)
	result := make([]string, 0, len(parts))
	
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	
	return result
}

// ContainsAny 检查字符串是否包含任意一个子字符串
func (so *StringOptimizer) ContainsAny(s string, substrings ...string) bool {
	for _, substr := range substrings {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// ReplaceMultiple 高效替换多个字符串
func (so *StringOptimizer) ReplaceMultiple(s string, replacements map[string]string) string {
	if len(replacements) == 0 {
		return s
	}
	
	result := s
	for old, new := range replacements {
		result = strings.ReplaceAll(result, old, new)
	}
	
	return result
}

// PadString 填充字符串到指定长度
func (so *StringOptimizer) PadString(s string, length int, padChar rune, leftPad bool) string {
	if len(s) >= length {
		return s
	}
	
	padCount := length - len(s)
	padding := strings.Repeat(string(padChar), padCount)
	
	if leftPad {
		return so.ConcatStrings(padding, s)
	}
	return so.ConcatStrings(s, padding)
}

// TruncateString 截断字符串到指定长度
func (so *StringOptimizer) TruncateString(s string, maxLength int, suffix string) string {
	if len(s) <= maxLength {
		return s
	}
	
	if len(suffix) >= maxLength {
		return suffix[:maxLength]
	}
	
	truncateLength := maxLength - len(suffix)
	return so.ConcatStrings(s[:truncateLength], suffix)
}

// EscapeString 转义字符串中的特殊字符
func (so *StringOptimizer) EscapeString(s string, escapeChars map[rune]string) string {
	if len(escapeChars) == 0 {
		return s
	}
	
	buf := so.pool.GetStringBuilder()
	defer so.pool.PutStringBuilder(buf)
	
	buf.Grow(len(s) * 2) // 预估转义后的长度
	
	for _, r := range s {
		if escaped, exists := escapeChars[r]; exists {
			buf.WriteString(escaped)
		} else {
			buf.WriteRune(r)
		}
	}
	
	return buf.String()
}

// RemoveDuplicateStrings 去除字符串切片中的重复项
func (so *StringOptimizer) RemoveDuplicateStrings(strs []string) []string {
	if len(strs) <= 1 {
		return strs
	}
	
	seen := make(map[string]bool, len(strs))
	result := make([]string, 0, len(strs))
	
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	
	return result
}

// FilterStrings 过滤字符串切片
func (so *StringOptimizer) FilterStrings(strs []string, predicate func(string) bool) []string {
	result := make([]string, 0, len(strs))
	
	for _, s := range strs {
		if predicate(s) {
			result = append(result, s)
		}
	}
	
	return result
}

// MapStrings 映射字符串切片
func (so *StringOptimizer) MapStrings(strs []string, mapper func(string) string) []string {
	result := make([]string, len(strs))
	
	for i, s := range strs {
		result[i] = mapper(s)
	}
	
	return result
}

// StringStats 字符串统计信息
type StringStats struct {
	Length      int
	WordCount   int
	LineCount   int
	CharCounts  map[rune]int
	IsEmpty     bool
	IsBlank     bool
	HasUnicode  bool
}

// AnalyzeString 分析字符串统计信息
func (so *StringOptimizer) AnalyzeString(s string) StringStats {
	stats := StringStats{
		Length:     len(s),
		CharCounts: make(map[rune]int),
		IsEmpty:    len(s) == 0,
		IsBlank:    strings.TrimSpace(s) == "",
	}
	
	if stats.IsEmpty {
		return stats
	}
	
	words := strings.Fields(s)
	stats.WordCount = len(words)
	stats.LineCount = strings.Count(s, "\n") + 1
	
	for _, r := range s {
		stats.CharCounts[r]++
		if r > 127 {
			stats.HasUnicode = true
		}
	}
	
	return stats
}