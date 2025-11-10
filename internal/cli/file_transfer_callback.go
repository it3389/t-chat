package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"t-chat/internal/network"
)

// CLIFileTransferCallback CLI文件传输确认回调实现
// 自动接受所有文件传输请求，并保存到默认下载目录
type CLIFileTransferCallback struct {
	downloadDir string
}

// NewCLIFileTransferCallback 创建CLI文件传输回调
func NewCLIFileTransferCallback(downloadDir string) *CLIFileTransferCallback {
	// 确保下载目录存在
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		fmt.Printf("创建下载目录失败: %v\n", err)
		// 使用当前目录作为备选
		downloadDir = "."
	}
	
	return &CLIFileTransferCallback{
		downloadDir: downloadDir,
	}
}

// OnFileTransferRequest 处理文件传输请求
// 自动接受所有文件传输请求
func (c *CLIFileTransferCallback) OnFileTransferRequest(request *network.FileTransferRequest) (bool, string, error) {
	fmt.Printf("\n=== 收到文件传输请求 ===\n")
	fmt.Printf("文件名: %s\n", request.FileName)
	fmt.Printf("文件大小: %d 字节\n", request.FileSize)
	fmt.Printf("发送者: %s\n", request.SenderID)
	fmt.Printf("文件ID: %s\n", request.FileID)
	fmt.Printf("保存路径: %s\n", c.GetSavePath(request.FileName))
	fmt.Printf("=== 自动接受文件传输 ===\n")
	
	// 自动接受所有文件传输请求
	return true, "自动接受", nil
}

// OnTransferProgress 传输进度回调
func (c *CLIFileTransferCallback) OnTransferProgress(fileID string, progress float64) {
	fmt.Printf("\r📊 文件传输进度 [%s]: %.1f%%", fileID[:8], progress*100)
	if progress >= 1.0 {
		fmt.Println() // 换行
	}
}

// OnTransferComplete 传输完成回调
func (c *CLIFileTransferCallback) OnTransferComplete(fileID string, success bool, err error) {
	if success {
		fmt.Printf("\n✅ 文件传输完成: %s\n", fileID[:8])
	} else {
		fmt.Printf("\n❌ 文件传输失败: %s, 错误: %v\n", fileID[:8], err)
	}
}

// OnTransferCanceled 传输取消回调
func (c *CLIFileTransferCallback) OnTransferCanceled(fileID string, reason string) {
	fmt.Printf("\n⚠️ 文件传输已取消: %s, 原因: %s\n", fileID[:8], reason)
}

// GetDownloadDir 获取下载目录
func (c *CLIFileTransferCallback) GetDownloadDir() string {
	return c.downloadDir
}

// SetDownloadDir 设置下载目录
func (c *CLIFileTransferCallback) SetDownloadDir(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建下载目录失败: %v", err)
	}
	c.downloadDir = dir
	return nil
}

// GetSavePath 获取文件保存路径
func (c *CLIFileTransferCallback) GetSavePath(fileName string) string {
	return filepath.Join(c.downloadDir, fileName)
}