package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"t-chat/internal/network"
)

// VoiceMessageHandler 语音消息处理器
// 负责处理语音消息的接收、保存和播放
type VoiceMessageHandler struct {
	pineconeService network.PineconeServiceLike
	logger         network.Logger
	saveDir        string // 语音文件保存目录
}

// NewVoiceMessageHandler 创建语音消息处理器
func NewVoiceMessageHandler(p network.PineconeServiceLike, logger network.Logger) *VoiceMessageHandler {
	handler := &VoiceMessageHandler{
		pineconeService: p,
		logger:         logger,
		saveDir:        "voice", // 默认保存目录
	}
	
	// 确保保存目录存在
	os.MkdirAll(handler.saveDir, 0755)
	
	return handler
}

// HandleMessage 处理语音消息
func (h *VoiceMessageHandler) HandleMessage(msg *network.Message) error {

	
	// 解析语音元数据
	voiceInfo := h.parseVoiceMetadata(msg)
	
	// 保存语音文件
	if len(msg.Data) > 0 {
		// 从Data字段中提取语音数据
		var voiceData []byte
		if dataInterface, exists := msg.Data["voice_data"]; exists {
			if dataStr, ok := dataInterface.(string); ok {
				// 假设数据是base64编码的
				voiceData = []byte(dataStr)
			} else if dataBytes, ok := dataInterface.([]byte); ok {
				voiceData = dataBytes
			} else {
				return fmt.Errorf("无效的语音数据格式")
			}
		} else {
			return fmt.Errorf("消息中缺少语音数据")
		}
		
		_, err := h.saveVoice(voiceData, msg.Content, voiceInfo)
		if err != nil {
			// 保存语音失败
			return err
		}

	}
	
	// 显示语音信息
	h.displayVoiceInfo(voiceInfo)
	
	return nil
}

// GetMessageType 获取消息类型
func (h *VoiceMessageHandler) GetMessageType() string {
	return network.MessageTypeVoice
}

// GetPriority 获取处理优先级
func (h *VoiceMessageHandler) GetPriority() int {
	return network.MessagePriorityNormal
}

// parseVoiceMetadata 解析语音元数据
func (h *VoiceMessageHandler) parseVoiceMetadata(msg *network.Message) *VoiceInfo {
	info := &VoiceInfo{
		FileName: msg.Content,
		Size:     0, // 将在后面从实际数据计算
		From:     msg.From,
		Time:     msg.Timestamp,
	}
	
	// 从元数据中提取信息
	if msg.Metadata != nil {
		if duration, ok := msg.Metadata["duration"].(float64); ok {
			info.Duration = duration
		}
		if format, ok := msg.Metadata["format"].(string); ok {
			info.Format = format
		}
		if sampleRate, ok := msg.Metadata["sample_rate"].(float64); ok {
			info.SampleRate = int(sampleRate)
		}
		if channels, ok := msg.Metadata["channels"].(float64); ok {
			info.Channels = int(channels)
		}
		if bitDepth, ok := msg.Metadata["bit_depth"].(float64); ok {
			info.BitDepth = int(bitDepth)
		}
		if quality, ok := msg.Metadata["quality"].(string); ok {
			info.Quality = quality
		}
	}
	
	// 如果没有格式信息，从文件名推断
	if info.Format == "" {
		info.Format = h.inferVoiceFormat(msg.Content)
	}
	
	// 如果没有时长信息，从音频数据计算
	if info.Duration == 0 {
		// 从Data字段中提取语音数据
		var voiceData []byte
		if dataInterface, exists := msg.Data["voice_data"]; exists {
			if dataStr, ok := dataInterface.(string); ok {
				// 假设数据是base64编码的
				voiceData = []byte(dataStr)
			} else if dataBytes, ok := dataInterface.([]byte); ok {
				voiceData = dataBytes
			}
		}
		
		if len(voiceData) > 0 {
			info.Duration = h.calculateDuration(voiceData, info)
			info.Size = len(voiceData) // 设置实际数据大小
		}
	}
	
	return info
}

// saveVoice 保存语音文件
func (h *VoiceMessageHandler) saveVoice(data []byte, fileName string, info *VoiceInfo) (string, error) {
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
	
	// 验证语音文件完整性
	if err := h.validateVoice(filePath, info.Format); err != nil {
		// 删除损坏的文件
		os.Remove(filePath)
		return "", fmt.Errorf("语音文件验证失败: %w", err)
	}
	
	return filePath, nil
}

// displayVoiceInfo 显示语音信息
func (h *VoiceMessageHandler) displayVoiceInfo(info *VoiceInfo) {
	// 语音信息显示已简化，仅在需要时输出
}

// calculateDuration 计算语音时长
func (h *VoiceMessageHandler) calculateDuration(data []byte, info *VoiceInfo) float64 {
	if info.SampleRate == 0 {
		// 默认采样率
		info.SampleRate = 16000
	}
	if info.Channels == 0 {
		// 默认单声道
		info.Channels = 1
	}
	if info.BitDepth == 0 {
		// 默认16位
		info.BitDepth = 16
	}
	
	// 计算字节数
	bytesPerSample := info.BitDepth / 8
	totalSamples := len(data) / bytesPerSample / info.Channels
	
	// 计算时长（秒）
	duration := float64(totalSamples) / float64(info.SampleRate)
	
	return duration
}

// validateVoice 验证语音文件完整性
func (h *VoiceMessageHandler) validateVoice(filePath, format string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// 根据格式验证文件头
	switch strings.ToUpper(format) {
	case "WAV":
		return h.validateWAVHeader(file)
	case "MP3":
		return h.validateMP3Header(file)
	case "OGG":
		return h.validateOGGHeader(file)
	case "AAC":
		return h.validateAACHeader(file)
	default:
		// 尝试自动检测格式
		return h.validateVoiceAutoDetect(file)
	}
}

// validateWAVHeader 验证WAV文件头
func (h *VoiceMessageHandler) validateWAVHeader(file *os.File) error {
	header := make([]byte, 12)
	_, err := file.Read(header)
	if err != nil {
		return err
	}
	
	// 检查WAV文件标识
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return fmt.Errorf("无效的WAV文件头")
	}
	
	return nil
}

// validateMP3Header 验证MP3文件头
func (h *VoiceMessageHandler) validateMP3Header(file *os.File) error {
	header := make([]byte, 3)
	_, err := file.Read(header)
	if err != nil {
		return err
	}
	
	// 检查MP3文件标识（简化验证）
	if header[0] != 0xFF || (header[1]&0xE0) != 0xE0 {
		return fmt.Errorf("无效的MP3文件头")
	}
	
	return nil
}

// validateOGGHeader 验证OGG文件头
func (h *VoiceMessageHandler) validateOGGHeader(file *os.File) error {
	header := make([]byte, 4)
	_, err := file.Read(header)
	if err != nil {
		return err
	}
	
	// 检查OGG文件标识
	if string(header) != "OggS" {
		return fmt.Errorf("无效的OGG文件头")
	}
	
	return nil
}

// validateAACHeader 验证AAC文件头
func (h *VoiceMessageHandler) validateAACHeader(file *os.File) error {
	header := make([]byte, 2)
	_, err := file.Read(header)
	if err != nil {
		return err
	}
	
	// 检查AAC文件标识（简化验证）
	if (header[0]&0xFF) != 0xFF || (header[1]&0xF0) != 0xF0 {
		return fmt.Errorf("无效的AAC文件头")
	}
	
	return nil
}

// validateVoiceAutoDetect 自动检测语音格式
func (h *VoiceMessageHandler) validateVoiceAutoDetect(file *os.File) error {
	// 重置文件指针
	file.Seek(0, 0)
	
	// 尝试不同的格式验证
	if err := h.validateWAVHeader(file); err == nil {
		return nil
	}
	
	file.Seek(0, 0)
	if err := h.validateMP3Header(file); err == nil {
		return nil
	}
	
	file.Seek(0, 0)
	if err := h.validateOGGHeader(file); err == nil {
		return nil
	}
	
	file.Seek(0, 0)
	if err := h.validateAACHeader(file); err == nil {
		return nil
	}
	
	return fmt.Errorf("无法识别语音格式")
}

// inferVoiceFormat 从文件名推断语音格式
func (h *VoiceMessageHandler) inferVoiceFormat(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".wav":
		return "WAV"
	case ".mp3":
		return "MP3"
	case ".ogg":
		return "OGG"
	case ".aac":
		return "AAC"
	case ".m4a":
		return "AAC"
	case ".flac":
		return "FLAC"
	default:
		return "UNKNOWN"
	}
}

// getFileExtension 获取文件扩展名
func (h *VoiceMessageHandler) getFileExtension(format string) string {
	switch strings.ToUpper(format) {
	case "WAV":
		return ".wav"
	case "MP3":
		return ".mp3"
	case "OGG":
		return ".ogg"
	case "AAC":
		return ".aac"
	case "FLAC":
		return ".flac"
	default:
		return ".bin"
	}
}

// VoiceInfo 语音信息结构
type VoiceInfo struct {
	FileName   string    `json:"file_name"`
	Format     string    `json:"format"`
	Duration   float64   `json:"duration"`    // 时长（秒）
	Size       int       `json:"size"`        // 文件大小（字节）
	From       string    `json:"from"`        // 发送者
	Time       time.Time `json:"time"`        // 发送时间
	SampleRate int       `json:"sample_rate"` // 采样率（Hz）
	Channels   int       `json:"channels"`    // 声道数
	BitDepth   int       `json:"bit_depth"`   // 位深度（bit）
	Quality    string    `json:"quality"`     // 质量描述
}

// 工具方法

// GetVoiceStats 获取语音统计信息
func (h *VoiceMessageHandler) GetVoiceStats() map[string]interface{} {
	// 统计保存目录中的语音文件
	files, err := os.ReadDir(h.saveDir)
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}
	
	stats := map[string]interface{}{
		"total_voice_files": len(files),
		"save_directory":    h.saveDir,
		"formats":          make(map[string]int),
		"total_duration":   0.0,
		"total_size":       0,
	}
	
	for _, file := range files {
		if !file.IsDir() {
			format := h.inferVoiceFormat(file.Name())
			stats["formats"].(map[string]int)[format]++
			
			// 获取文件信息
			if fileInfo, err := file.Info(); err == nil {
				stats["total_size"] = stats["total_size"].(int) + int(fileInfo.Size())
			}
		}
	}
	
	return stats
}

// ConvertVoiceFormat 转换语音格式
func (h *VoiceMessageHandler) ConvertVoiceFormat(inputPath, outputFormat string) (string, error) {
	// 这里可以实现语音格式转换功能
	// 需要集成外部工具如 ffmpeg
	return "", fmt.Errorf("语音格式转换功能暂未实现")
}

// ExtractVoiceMetadata 提取语音元数据
func (h *VoiceMessageHandler) ExtractVoiceMetadata(filePath string) (*VoiceInfo, error) {
	// 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	info := &VoiceInfo{
		FileName: filepath.Base(filePath),
		Size:     len(data),
		Time:     time.Now(),
	}
	
	// 推断格式
	info.Format = h.inferVoiceFormat(filePath)
	
	// 计算时长
	info.Duration = h.calculateDuration(data, info)
	
	return info, nil
}