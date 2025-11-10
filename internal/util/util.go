package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"t-chat/internal/performance"
)

// SendSession 发送会话信息
type SendSession struct {
	FileID      string
	FileName    string
	TotalChunks int
	CurrentIdx  int
	Checksum    string
}

// FileReceiveSession 接收会话信息
type FileReceiveSession struct {
	FileName   string
	Total      int
	Chunks     [][]byte
	Received   int
	Checksum   string
	LastUpdate int64
}

// CryptoUtils 加密工具类
type CryptoUtils struct{}

// NewCryptoUtils 创建加密工具实例
func NewCryptoUtils() *CryptoUtils {
	return &CryptoUtils{}
}

// CalculateChecksum 计算数据的SHA256校验和
func (cu *CryptoUtils) CalculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// CalculateFileChecksum 计算文件的SHA256校验和
func (cu *CryptoUtils) CalculateFileChecksum(filePath string) (string, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return cu.CalculateChecksum(data), nil
}

// StringUtils 字符串工具类
type StringUtils struct{}

// 字符串构建器池，减少内存分配
var stringBuilderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// NewStringUtils 创建字符串工具实例
func NewStringUtils() *StringUtils {
	return &StringUtils{}
}

// IsEmpty 检查字符串是否为空
func (su *StringUtils) IsEmpty(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

// Truncate 截断字符串到指定长度
func (su *StringUtils) Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// IsHexString 检查字符串是否为有效的十六进制字符串
func (su *StringUtils) IsHexString(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// IsHex 检查字符串是否为十六进制
func (su *StringUtils) IsHex(str string) bool {
	for _, c := range str {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// JoinStrings 高效拼接字符串
func (su *StringUtils) JoinStrings(parts ...string) string {
	builder := stringBuilderPool.Get().(*strings.Builder)
	builder.Reset()
	defer stringBuilderPool.Put(builder)
	
	for _, part := range parts {
		builder.WriteString(part)
	}
	return builder.String()
}

// FormatWithBuilder 使用构建器格式化字符串
func (su *StringUtils) FormatWithBuilder(format string, args ...interface{}) string {
	builder := stringBuilderPool.Get().(*strings.Builder)
	builder.Reset()
	defer stringBuilderPool.Put(builder)
	
	fmt.Fprintf(builder, format, args...)
	return builder.String()
}

// FileUtils 文件工具类
type FileUtils struct{}

// NewFileUtils 创建文件工具实例
func NewFileUtils() *FileUtils {
	return &FileUtils{}
}

// EnsureDir 确保目录存在
func (fu *FileUtils) EnsureDir(dirPath string) error {
	return os.MkdirAll(dirPath, 0755)
}

// FileExists 检查文件是否存在
func (fu *FileUtils) FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// GetFileSize 获取文件大小
func (fu *FileUtils) GetFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// 全局工具实例
var (
	Crypto = NewCryptoUtils()
	String = NewStringUtils()
	File   = NewFileUtils()
)

// CalculateChecksum 保持向后兼容的全局函数
func CalculateChecksum(data []byte) string {
	return Crypto.CalculateChecksum(data)
}

func LoadSendSession(fileID string) *SendSession {
	filePath := filepath.Join("files", ".send_"+fileID+".json")
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil
	}
	sess := &SendSession{}
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, sess); err != nil {
		return nil
	}
	return sess
}

func SaveSendSession(fileID string, sess *SendSession) {
	filePath := filepath.Join("files", ".send_"+fileID+".json")
	jsonOptimizer := performance.GetJSONOptimizer()
	data, _ := jsonOptimizer.MarshalIndent(sess, "", "  ")
	_ = ioutil.WriteFile(filePath, data, 0644)
}

func LoadReceiveSession(fileID string) *FileReceiveSession {
	filePath := filepath.Join("files", ".recv_"+fileID+".json")
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil
	}
	sess := &FileReceiveSession{}
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, sess); err != nil {
		return nil
	}
	return sess
}

func SaveReceiveSession(fileID string, sess *FileReceiveSession) {
	filePath := filepath.Join("files", ".recv_"+fileID+".json")
	jsonOptimizer := performance.GetJSONOptimizer()
	data, _ := jsonOptimizer.MarshalIndent(sess, "", "  ")
	_ = ioutil.WriteFile(filePath, data, 0644)
}