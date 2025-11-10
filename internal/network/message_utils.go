package network

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// debugLogger 调试日志记录器（由types.go中的SetDebugLogger设置）
var debugLogger Logger

// setMessageUtilsDebugLogger 设置调试日志记录器（由types.go调用）
func setMessageUtilsDebugLogger(logger Logger) {
	debugLogger = logger
}

// getMessageUtilsDebugLogger 获取调试日志记录器（由types.go调用）
func getMessageUtilsDebugLogger() Logger {
	return debugLogger
}

// MessageBuilder 消息构建器
type MessageBuilder struct {
	packet *MessagePacket
}

// MessageBuilder对象池
var messageBuilderPool = sync.Pool{
	New: func() interface{} {
		return &MessageBuilder{}
	},
}

// NewMessageBuilder 创建新的消息构建器（使用对象池）
func NewMessageBuilder() *MessageBuilder {
	builder := messageBuilderPool.Get().(*MessageBuilder)
	builder.packet = NewMessagePacket()
	builder.packet.Priority = MessagePriorityNormal
	return builder
}

// SetTo 设置接收方
func (mb *MessageBuilder) SetTo(to string) *MessageBuilder {
	mb.packet.To = to
	return mb
}

// SetFrom 设置发送方
func (mb *MessageBuilder) SetFrom(from string) *MessageBuilder {
	mb.packet.From = from
	return mb
}

// SetID 设置消息ID
func (mb *MessageBuilder) SetID(id string) *MessageBuilder {
	mb.packet.ID = id
	return mb
}

// SetType 设置消息类型
func (mb *MessageBuilder) SetType(msgType string) *MessageBuilder {
	mb.packet.Type = msgType
	return mb
}

// SetContent 设置消息内容
func (mb *MessageBuilder) SetContent(content string) *MessageBuilder {
	mb.packet.Content = content
	return mb
}

// SetData 设置消息数据
func (mb *MessageBuilder) SetData(data []byte) *MessageBuilder {
	mb.packet.Data = data
	return mb
}

// SetPriority 设置消息优先级
func (mb *MessageBuilder) SetPriority(priority int) *MessageBuilder {
	mb.packet.Priority = priority
	return mb
}

// SetReplyTo 设置回复消息ID
func (mb *MessageBuilder) SetReplyTo(replyTo string) *MessageBuilder {
	mb.packet.ReplyTo = replyTo
	return mb
}

// AddMetadata 添加元数据
func (mb *MessageBuilder) AddMetadata(key string, value interface{}) *MessageBuilder {
	mb.packet.Metadata[key] = value
	return mb
}

// Build 构建消息包并释放构建器
func (mb *MessageBuilder) Build() *MessagePacket {
	// 如果时间戳为零值，设置为当前时间
	if mb.packet.Timestamp.IsZero() {
		mb.packet.Timestamp = time.Now()
	}
	
	// 添加消息构建日志（如果有调试日志记录器）
	if debugLogger != nil {
		debugLogger.Debugf("[MessageBuild] 构建消息包: ID=%s, Type=%s, To=%s, Content=%s, Timestamp=%s", mb.packet.ID, mb.packet.Type, mb.packet.To, mb.packet.Content, mb.packet.Timestamp.Format("2006-01-02 15:04:05"))
	}
	
	// 返回原始packet指针，调用者负责释放
	result := mb.packet
	mb.packet = nil
	messageBuilderPool.Put(mb)
	return result
}

// BuildAndKeep 构建消息包但保持构建器可用
func (mb *MessageBuilder) BuildAndKeep() MessagePacket {
	return *mb.packet // 返回副本
}

// Release 手动释放构建器资源
func (mb *MessageBuilder) Release() {
	if mb.packet != nil {
		ReleaseMessagePacket(mb.packet)
		mb.packet = nil
	}
	messageBuilderPool.Put(mb)
}

// SendTextMessage 发送文本消息
func (ps *PineconeService) SendTextMessage(toAddr, content string, messageID string) error {
	builder := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeText).
		SetContent(content).
		SetPriority(MessagePriorityNormal)
	
	// 设置消息ID（用于ACK确认）
	if messageID != "" {
		builder = builder.SetID(messageID)
	}
	
	packet := builder.Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendFileMessage 发送文件消息
func (ps *PineconeService) SendFileMessage(toAddr, filePath string) error {
	// 读取文件
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %v", err)
	}

	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %v", err)
	}

	// 计算文件校验和
	hash := md5.New()
	hash.Write(fileData)
	checksum := hex.EncodeToString(hash.Sum(nil))

	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeFile).
		SetContent(filepath.Base(filePath)).
		SetData(fileData).
		SetPriority(MessagePriorityHigh).
		AddMetadata("size", fileInfo.Size()).
		AddMetadata("checksum", checksum).
		AddMetadata("type", getFileType(filePath)).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendSystemMessage 发送系统消息
func (ps *PineconeService) SendSystemMessage(toAddr, content, level string) error {
	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeSystem).
		SetContent(content).
		SetPriority(MessagePriorityHigh).
		AddMetadata("level", level).
		AddMetadata("timestamp", time.Now().Unix()).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendCommandMessage 发送命令消息
func (ps *PineconeService) SendCommandMessage(toAddr, command string, args ...string) error {
	fullCommand := command
	if len(args) > 0 {
		fullCommand = command + " " + strings.Join(args, " ")
	}

	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeCommand).
		SetContent(fullCommand).
		SetPriority(MessagePriorityHigh).
		AddMetadata("command", command).
		AddMetadata("args", args).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendHeartbeatMessage removed - heartbeat mechanism has been disabled

// SendImageMessage 发送图片消息
func (ps *PineconeService) SendImageMessage(toAddr, imagePath string, width, height int) error {
	// 读取图片文件
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("读取图片文件失败: %v", err)
	}

	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeImage).
		SetContent(filepath.Base(imagePath)).
		SetData(imageData).
		SetPriority(MessagePriorityNormal).
		AddMetadata("width", width).
		AddMetadata("height", height).
		AddMetadata("format", getFileType(imagePath)).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendVoiceMessage 发送语音消息
func (ps *PineconeService) SendVoiceMessage(toAddr, voicePath string, duration float64) error {
	// 读取语音文件
	voiceData, err := os.ReadFile(voicePath)
	if err != nil {
		return fmt.Errorf("读取语音文件失败: %v", err)
	}

	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeVoice).
		SetContent(filepath.Base(voicePath)).
		SetData(voiceData).
		SetPriority(MessagePriorityNormal).
		AddMetadata("duration", duration).
		AddMetadata("format", getFileType(voicePath)).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendLocationMessage 发送位置消息
func (ps *PineconeService) SendLocationMessage(toAddr, address string, latitude, longitude float64) error {
	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeLocation).
		SetContent(address).
		SetPriority(MessagePriorityNormal).
		AddMetadata("latitude", latitude).
		AddMetadata("longitude", longitude).
		AddMetadata("address", address).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// getFileType 根据文件扩展名获取文件类型
func getFileType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".mp4":
		return "video/mp4"
	case ".avi":
		return "video/avi"
	case ".mov":
		return "video/quicktime"
	case ".txt":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/msword"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	default:
		return "application/octet-stream"
	}
}

// BroadcastMessage 广播消息到所有已知节点
func (ps *PineconeService) BroadcastMessage(msgType, content string) error {
	ps.mu.RLock()
	peers := make([]string, 0, len(ps.peers))
	for _, peer := range ps.peers {
		peers = append(peers, peer.Address)
	}
	ps.mu.RUnlock()

	var lastError error
	for _, peerAddr := range peers {
		if err := ps.SendTextMessage(peerAddr, content, ""); err != nil {
			ps.logger.Errorf("广播消息到 %s 失败: %v", peerAddr, err)
			lastError = err
		}
	}

	return lastError
}

// SendReplyMessage 发送回复消息
func (ps *PineconeService) SendReplyMessage(toAddr, content, originalMsgID string) error {
	return ps.SendTextMessage(toAddr, content, originalMsgID)
}

// SendUrgentMessage 发送紧急消息（高优先级）
func (ps *PineconeService) SendUrgentMessage(toAddr, content string) error {
	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeText).
		SetContent(content).
		SetPriority(MessagePriorityHigh).
		AddMetadata("urgent", true).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendAckMessage 发送消息确认
func (ps *PineconeService) SendAckMessage(toAddr, originalMsgID, status string) error {
	ack := MessageAck{
		OriginalMsgID: originalMsgID,
		Status:        status,
		Timestamp:     time.Now(),
	}

	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeAck).
		SetContent("ack").
		SetPriority(MessagePriorityHigh).
		AddMetadata("ack_data", ack).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}

// SendNackMessage 发送消息否定确认
func (ps *PineconeService) SendNackMessage(toAddr, originalMsgID, reason string, errorCode int, retryAfter int) error {
	nack := MessageNack{
		OriginalMsgID: originalMsgID,
		Reason:        reason,
		ErrorCode:     errorCode,
		Timestamp:     time.Now(),
		RetryAfter:    retryAfter,
	}

	packet := NewMessageBuilder().
		SetFrom(ps.GetPublicKeyHex()).
		SetTo(toAddr).
		SetType(MessageTypeNack).
		SetContent("nack").
		SetPriority(MessagePriorityHigh).
		AddMetadata("nack_data", nack).
		Build()

	return ps.SendMessagePacket(toAddr, packet)
}
