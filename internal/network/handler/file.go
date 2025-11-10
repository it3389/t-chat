package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"t-chat/internal/network"
)

type FileMessageHandler struct {
	pineconeService network.PineconeServiceLike
	logger         network.Logger
}

func NewFileMessageHandler(p network.PineconeServiceLike, logger network.Logger) *FileMessageHandler {
	return &FileMessageHandler{pineconeService: p, logger: logger}
}

func (h *FileMessageHandler) HandleMessage(msg *network.Message) error {
	// 处理文件消息
	// 这里只做简单演示，详细分片/断点续传逻辑可参考原实现
	if len(msg.Data) > 0 {
		fileDir := "./received_files"
		if err := os.MkdirAll(fileDir, 0755); err != nil {
			return fmt.Errorf("创建文件目录失败: %v", err)
		}
		filePath := filepath.Join(fileDir, msg.Content)
		
		// 从Data字段中提取文件数据
		var fileData []byte
		if dataInterface, exists := msg.Data["file_data"]; exists {
			if dataStr, ok := dataInterface.(string); ok {
				// 假设数据是base64编码的
				fileData = []byte(dataStr)
			} else if dataBytes, ok := dataInterface.([]byte); ok {
				fileData = dataBytes
			} else {
				return fmt.Errorf("无效的文件数据格式")
			}
		} else {
			return fmt.Errorf("消息中缺少文件数据")
		}
		
		if err := os.WriteFile(filePath, fileData, 0644); err != nil {
			return fmt.Errorf("保存文件失败: %v", err)
		}
		// 文件已保存

	}
	return nil
}

func (h *FileMessageHandler) GetMessageType() string {
	return network.MessageTypeFile
}

func (h *FileMessageHandler) GetPriority() int {
	return network.MessagePriorityHigh
}