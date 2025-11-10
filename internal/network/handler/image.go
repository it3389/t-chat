package handler

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"t-chat/internal/network"
)

// ImageMessageHandler 图片消息处理器
// 负责处理图片消息的接收、保存和显示
type ImageMessageHandler struct {
	pineconeService network.PineconeServiceLike
	logger         network.Logger
	saveDir        string // 图片保存目录
}

// NewImageMessageHandler 创建图片消息处理器
func NewImageMessageHandler(p network.PineconeServiceLike, logger network.Logger) *ImageMessageHandler {
	handler := &ImageMessageHandler{
		pineconeService: p,
		logger:         logger,
		saveDir:        "images", // 默认保存目录
	}
	
	// 确保保存目录存在
	os.MkdirAll(handler.saveDir, 0755)
	
	return handler
}

// HandleMessage 处理图片消息
func (h *ImageMessageHandler) HandleMessage(msg *network.Message) error {

	
	// 解析图片元数据
	imageInfo := h.parseImageMetadata(msg)
	
	// 保存图片文件
	if len(msg.Data) > 0 {
		// 从Data字段中提取图片数据
		var imageData []byte
		if dataInterface, exists := msg.Data["image_data"]; exists {
			if dataStr, ok := dataInterface.(string); ok {
				// 假设数据是base64编码的
				var err error
				imageData, err = base64.StdEncoding.DecodeString(dataStr)
				if err != nil {
					return fmt.Errorf("解码base64图片数据失败: %v", err)
				}
			} else if dataBytes, ok := dataInterface.([]byte); ok {
				imageData = dataBytes
			} else {
				return fmt.Errorf("无效的图片数据格式")
			}
		} else {
			return fmt.Errorf("消息中缺少图片数据")
		}
		
		_, err := h.saveImage(imageData, msg.Content, imageInfo)
			if err != nil {
				// 保存图片失败
				return err
			}

	}
	
	// 显示图片信息
	h.displayImageInfo(imageInfo)
	
	return nil
}

// GetMessageType 获取消息类型
func (h *ImageMessageHandler) GetMessageType() string {
	return network.MessageTypeImage
}

// GetPriority 获取处理优先级
func (h *ImageMessageHandler) GetPriority() int {
	return network.MessagePriorityNormal
}

// parseImageMetadata 解析图片元数据
func (h *ImageMessageHandler) parseImageMetadata(msg *network.Message) *ImageInfo {
	info := &ImageInfo{
		FileName: msg.Content,
		Size:     len(msg.Data),
		From:     msg.From,
		Time:     msg.Timestamp,
	}
	
	// 从元数据中提取信息
	if msg.Metadata != nil {
		if width, ok := msg.Metadata["width"].(float64); ok {
			info.Width = int(width)
		}
		if height, ok := msg.Metadata["height"].(float64); ok {
			info.Height = int(height)
		}
		if format, ok := msg.Metadata["format"].(string); ok {
			info.Format = format
		}
		if description, ok := msg.Metadata["description"].(string); ok {
			info.Description = description
		}
	}
	
	// 如果没有格式信息，从文件名推断
	if info.Format == "" {
		info.Format = h.inferImageFormat(msg.Content)
	}
	
	// 如果没有尺寸信息，从图片数据解析
	if info.Width == 0 || info.Height == 0 {
		// 从Data字段中提取图片数据
		var imageData []byte
		if dataInterface, exists := msg.Data["image_data"]; exists {
			if dataStr, ok := dataInterface.(string); ok {
				// 假设数据是base64编码的
				if decoded, err := base64.StdEncoding.DecodeString(dataStr); err == nil {
					imageData = decoded
				}
			} else if dataBytes, ok := dataInterface.([]byte); ok {
				imageData = dataBytes
			}
		}
		
		if len(imageData) > 0 {
			if img, err := h.decodeImage(imageData); err == nil {
				bounds := img.Bounds()
				info.Width = bounds.Dx()
				info.Height = bounds.Dy()
			}
		}
	}
	
	return info
}

// saveImage 保存图片文件
func (h *ImageMessageHandler) saveImage(data []byte, fileName string, info *ImageInfo) (string, error) {
	// 生成唯一文件名
	timestamp := time.Now().Format("20060102_150405")
	ext := h.getFileExtension(info.Format)
	uniqueName := fmt.Sprintf("%s_%s_%s%s", 
		strings.TrimSuffix(fileName, filepath.Ext(fileName)),
		info.From,
		timestamp,
		ext,
	)
	
	// 构建完整路径
	filePath := filepath.Join(h.saveDir, uniqueName)
	
	// 写入文件
	err := os.WriteFile(filePath, data, 0644)
	if err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	
	// 验证图片完整性
	if err := h.validateImage(filePath, info.Format); err != nil {
		// 删除损坏的文件
		os.Remove(filePath)
		return "", fmt.Errorf("图片验证失败: %w", err)
	}
	
	return filePath, nil
}

// displayImageInfo 显示图片信息
func (h *ImageMessageHandler) displayImageInfo(info *ImageInfo) {
	// 图片信息显示 - 仅在需要时输出
}

// decodeImage 解码图片数据
func (h *ImageMessageHandler) decodeImage(data []byte) (image.Image, error) {
	// 尝试不同的图片格式
	formats := []struct {
		name string
		decode func([]byte) (image.Image, error)
	}{
		{"JPEG", func(data []byte) (image.Image, error) {
			return jpeg.Decode(strings.NewReader(string(data)))
		}},
		{"PNG", func(data []byte) (image.Image, error) {
			return png.Decode(strings.NewReader(string(data)))
		}},
		{"GIF", func(data []byte) (image.Image, error) {
			return gif.Decode(strings.NewReader(string(data)))
		}},
	}
	
	for _, format := range formats {
		if img, err := format.decode(data); err == nil {
			return img, nil
		}
	}
	
	return nil, fmt.Errorf("无法解码图片数据")
}

// validateImage 验证图片完整性
func (h *ImageMessageHandler) validateImage(filePath, format string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// 根据格式验证
	switch strings.ToUpper(format) {
	case "JPEG", "JPG":
		_, err = jpeg.Decode(file)
	case "PNG":
		_, err = png.Decode(file)
	case "GIF":
		_, err = gif.Decode(file)
	default:
		// 尝试自动检测格式
		_, err = h.decodeImageFromFile(file)
	}
	
	return err
}

// decodeImageFromFile 从文件解码图片
func (h *ImageMessageHandler) decodeImageFromFile(file *os.File) (image.Image, error) {
	// 重置文件指针
	file.Seek(0, 0)
	
	// 尝试不同的解码器
	if img, err := jpeg.Decode(file); err == nil {
		return img, nil
	}
	
	file.Seek(0, 0)
	if img, err := png.Decode(file); err == nil {
		return img, nil
	}
	
	file.Seek(0, 0)
	if img, err := gif.Decode(file); err == nil {
		return img, nil
	}
	
	return nil, fmt.Errorf("无法识别图片格式")
}

// inferImageFormat 从文件名推断图片格式
func (h *ImageMessageHandler) inferImageFormat(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".jpg", ".jpeg":
		return "JPEG"
	case ".png":
		return "PNG"
	case ".gif":
		return "GIF"
	case ".bmp":
		return "BMP"
	case ".webp":
		return "WEBP"
	default:
		return "UNKNOWN"
	}
}

// getFileExtension 获取文件扩展名
func (h *ImageMessageHandler) getFileExtension(format string) string {
	switch strings.ToUpper(format) {
	case "JPEG", "JPG":
		return ".jpg"
	case "PNG":
		return ".png"
	case "GIF":
		return ".gif"
	case "BMP":
		return ".bmp"
	case "WEBP":
		return ".webp"
	default:
		return ".bin"
	}
}

// ImageInfo 图片信息结构
type ImageInfo struct {
	FileName    string    `json:"file_name"`
	Format      string    `json:"format"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Size        int       `json:"size"`
	From        string    `json:"from"`
	Time        time.Time `json:"time"`
	Description string    `json:"description"`
}

// 工具方法

// EncodeImageToBase64 将图片编码为Base64
func (h *ImageMessageHandler) EncodeImageToBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeImageFromBase64 从Base64解码图片
func (h *ImageMessageHandler) DecodeImageFromBase64(base64Data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(base64Data)
}

// GetImageStats 获取图片统计信息
func (h *ImageMessageHandler) GetImageStats() map[string]interface{} {
	// 统计保存目录中的图片
	files, err := os.ReadDir(h.saveDir)
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}
	
	stats := map[string]interface{}{
		"total_images": len(files),
		"save_directory": h.saveDir,
		"formats": make(map[string]int),
	}
	
	for _, file := range files {
		if !file.IsDir() {
			format := h.inferImageFormat(file.Name())
			stats["formats"].(map[string]int)[format]++
		}
	}
	
	return stats
}