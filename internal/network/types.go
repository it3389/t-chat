package network

import (
	"bytes"
	"sync"
	"time"
	"t-chat/internal/friend"
	"t-chat/internal/performance"
)

// SetDebugLogger 设置调试日志记录器
func SetDebugLogger(logger Logger) {
	// 设置message_utils.go中的debugLogger
	setMessageUtilsDebugLogger(logger)
}

// getDebugLogger 获取调试日志记录器
func getDebugLogger() Logger {
	return getMessageUtilsDebugLogger()
}

// 文件分片传输相关消息类型
const (
	MessageTypeFileRequest  = "file_request"
	MessageTypeFileChunk    = "file_chunk"
	MessageTypeFileAck      = "file_ack"
	MessageTypeFileComplete = "file_complete"
	MessageTypeFileNack     = "file_nack"
	MessageTypeFileData     = "file_data"
	MessageTypeFileCancel   = "file_cancel"
	MessageTypeFriendSearchRequest  = "friend_search_request"
	MessageTypeFriendSearchResponse = "friend_search_response"
	MessageTypeUserInfoExchange     = "user_info_exchange"
)

// 消息优先级常量已在interfaces.go中定义

// 文件传输状态常量
const (
	TransferStatusPending   = "pending"
	TransferStatusRunning   = "running"
	TransferStatusPaused    = "paused"
	TransferStatusCompleted = "completed"
	TransferStatusFailed    = "failed"
	TransferStatusCanceled  = "canceled"
)

// 文件传输错误码
const (
	FileTransferErrorNone           = 0
	FileTransferErrorInvalidRequest = 1001
	FileTransferErrorFileNotFound   = 1002
	FileTransferErrorPermissionDenied = 1003
	FileTransferErrorInsufficientSpace = 1004
	FileTransferErrorChecksumMismatch = 1005
	FileTransferErrorTimeout        = 1006
	FileTransferErrorNetworkError   = 1007
	FileTransferErrorCanceled       = 1008
	FileTransferErrorUnknown        = 1999
)

// FileChunkMsg 文件分片消息结构
// 用于大文件分片传输
// FileID: 文件唯一标识
// Index: 分片序号（从0开始）
// Total: 总分片数
// Data: 分片数据（BASE64编码字符串，确保JSON序列化安全）
// Checksum: 分片校验和
// FileName: 文件名（仅首片带）
type FileChunkMsg struct {
	FileID   string `json:"file_id"`
	Index    int    `json:"index"`
	Total    int    `json:"total"`
	Data     string `json:"data"` // 存储BASE64编码的字符串
	Checksum string `json:"checksum"`
	FileName string `json:"file_name,omitempty"`
}

// FileRequestMsg 文件传输请求
// FileID: 文件唯一标识
// FileName: 文件名
// Size: 文件总大小
// Total: 总分片数
// Checksum: 文件整体校验和
type FileRequestMsg struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	Size     int64  `json:"size"`
	Total    int    `json:"total"`
	Checksum string `json:"checksum"`
}

// FileAckMsg 分片确认/断点续传
// FileID: 文件唯一标识
// ReceivedIndex: 已收到的最大分片号
type FileAckMsg struct {
	FileID        string `json:"file_id"`
	ReceivedIndex int    `json:"received_index"`
}

// FileCompleteMsg 文件传输完成通知
type FileCompleteMsg struct {
	FileID string `json:"file_id"`
}

// MessageAck 消息确认结构
// OriginalMsgID: 原始消息ID
// Status: 确认状态 (delivered, read, failed)
// Timestamp: 确认时间戳
// ErrorCode: 错误代码（失败时）
// ErrorMessage: 错误信息（失败时）
type MessageAck struct {
	OriginalMsgID string    `json:"original_msg_id"`
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
	ErrorCode     int       `json:"error_code,omitempty"`
	ErrorMessage  string    `json:"error_message,omitempty"`
}

// MessageNack 消息否定确认结构
// OriginalMsgID: 原始消息ID
// Reason: 失败原因
// ErrorCode: 错误代码
// Timestamp: 失败时间戳
// RetryAfter: 建议重试间隔（秒）
type MessageNack struct {
	OriginalMsgID string    `json:"original_msg_id"`
	Reason        string    `json:"reason"`
	ErrorCode     int       `json:"error_code"`
	Timestamp     time.Time `json:"timestamp"`
	RetryAfter    int       `json:"retry_after,omitempty"`
}

// FriendSearchRequest 好友搜索请求
// Keyword: 搜索关键字（名称或公钥）
// From: 来源节点公钥
// Path: 跳跃路径（节点公钥数组）
type FriendSearchRequest struct {
	Keyword string   `json:"keyword"`
	From    string   `json:"from"`
	Path    []string `json:"path"`
}

// FriendSearchResponse 好友搜索响应
// Name: 用户名
// PublicKey: 公钥
// Path: 跳跃路径（节点公钥数组）
type FriendSearchResponse struct {
	Name     string   `json:"name"`
	PublicKey string  `json:"public_key"`
	Path     []string `json:"path"`
}

// UserInfoExchange 用户信息交换消息
// Username: 用户名
// PublicKey: 公钥
// PineconeAddr: Pinecone地址
type UserInfoExchange struct {
	Username     string `json:"username"`
	PublicKey    string `json:"public_key"`
	PineconeAddr string `json:"pinecone_addr"`
}

// 消息对象池，减少GC压力
var messagePool = sync.Pool{
	New: func() interface{} {
		return &Message{
			Metadata: make(map[string]interface{}),
		}
	},
}

// MessagePacket对象池
var messagePacketPool = sync.Pool{
	New: func() interface{} {
		return &MessagePacket{
			Metadata: make(map[string]interface{}),
		}
	},
}

// 字节切片池，用于序列化缓冲 - 优化：减少初始容量
var bytesPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 512) // 从1KB减少到512B初始容量
	},
}

// JSON编码器池，减少编码器创建开销
var jsonEncoderPool = sync.Pool{
	New: func() interface{} {
		return &bytes.Buffer{}
	},
}

// JSON解码器缓冲池
var jsonDecoderPool = sync.Pool{
	New: func() interface{} {
		return &bytes.Buffer{}
	},
}

// NewMessage 从对象池获取消息对象
func NewMessage() *Message {
	msg := messagePool.Get().(*Message)
	// 重置消息字段
	msg.ID = ""
	msg.Type = ""
	msg.From = ""
	msg.To = ""
	msg.Content = ""
	msg.Timestamp = time.Time{}
	// 清空metadata但保留map
	for k := range msg.Metadata {
		delete(msg.Metadata, k)
	}
	// 添加调试日志记录对象池使用情况
	if logger := getDebugLogger(); logger != nil {
		logger.Debugf("[ObjectPool] 从消息池获取对象: ID=%p, Type=%s", msg, msg.Type)
	}
	return msg
}

// ReleaseMessage 将消息对象归还到池中
func ReleaseMessage(msg *Message) {
	if msg != nil {
		// 添加调试日志记录对象池归还情况
		if logger := getDebugLogger(); logger != nil {
			logger.Debugf("[ObjectPool] 归还消息对象到池: ID=%p, Type=%s, Content=%s", msg, msg.Type, msg.Content)
		}
		// 验证对象状态，确保没有残留数据
		if msg.Type != "" || msg.Content != "" || len(msg.Metadata) > 0 {
			// 屏蔽对象池警告日志，避免过多输出
			// if logger := getDebugLogger(); logger != nil {
			//	logger.Warnf("[ObjectPool] 警告：归还的消息对象未完全清理 - Type=%s, Content=%s, MetadataCount=%d", msg.Type, msg.Content, len(msg.Metadata))
			// }
			// 强制清理
			msg.ID = ""
			msg.Type = ""
			msg.From = ""
			msg.To = ""
			msg.Content = ""
			msg.Timestamp = time.Time{}
			for k := range msg.Metadata {
				delete(msg.Metadata, k)
			}
		}
		messagePool.Put(msg)
	}
}

// NewMessagePacket 从对象池获取MessagePacket对象
func NewMessagePacket() *MessagePacket {
	packet := messagePacketPool.Get().(*MessagePacket)
	// 重置字段
	packet.ID = ""
	packet.Type = ""
	packet.From = ""
	packet.To = ""
	packet.Content = ""
	packet.Data = nil
	packet.Timestamp = time.Time{}
	packet.Priority = 0
	packet.ReplyTo = ""
	// 确保Metadata不为nil，然后清空
	if packet.Metadata == nil {
		packet.Metadata = make(map[string]interface{})
	} else {
		// 清空metadata但保留map
		for k := range packet.Metadata {
			delete(packet.Metadata, k)
		}
	}
	// 添加调试日志记录对象池使用情况
	if logger := getDebugLogger(); logger != nil {
		logger.Debugf("[ObjectPool] 从消息包池获取对象: ID=%p, Type=%s", packet, packet.Type)
	}
	return packet
}

// ReleaseMessagePacket 将MessagePacket对象归还到池中
func ReleaseMessagePacket(packet *MessagePacket) {
	if packet != nil {
		// 添加调试日志记录对象池归还情况
		if logger := getDebugLogger(); logger != nil {
			logger.Debugf("[ObjectPool] 归还消息包对象到池: ID=%p, Type=%s, Content=%s", packet, packet.Type, packet.Content)
		}
		// 验证对象状态，确保没有残留数据
		if packet.Type != "" || packet.Content != "" || len(packet.Metadata) > 0 || packet.Data != nil {
			// 屏蔽对象池警告日志，避免过多输出
			// if logger := getDebugLogger(); logger != nil {
			//	logger.Warnf("[ObjectPool] 警告：归还的消息包对象未完全清理 - Type=%s, Content=%s, MetadataCount=%d, DataLen=%d", packet.Type, packet.Content, len(packet.Metadata), len(packet.Data))
			// }
			// 强制清理
			packet.ID = ""
			packet.Type = ""
			packet.From = ""
			packet.To = ""
			packet.Content = ""
			packet.Data = nil
			packet.Timestamp = time.Time{}
			packet.Priority = 0
			packet.ReplyTo = ""
			// 确保Metadata不为nil，然后清空
			if packet.Metadata == nil {
				packet.Metadata = make(map[string]interface{})
			} else {
				for k := range packet.Metadata {
					delete(packet.Metadata, k)
				}
			}
		}
		messagePacketPool.Put(packet)
	}
}

// GetBytesBuffer 从对象池获取字节缓冲区（已优化）
func GetBytesBuffer() *bytes.Buffer {
	memPool := performance.GetMemoryPool()
	return memPool.GetStringBuilder()
}

// PutBytesBuffer 将字节缓冲区返回到对象池（已优化）
func PutBytesBuffer(buf *bytes.Buffer) {
	memPool := performance.GetMemoryPool()
	memPool.PutStringBuilder(buf)
}

// MarshalJSONPooled 使用对象池进行JSON序列化（已优化）
func MarshalJSONPooled(v interface{}) ([]byte, error) {
	// 添加序列化前的调试日志
	if logger := getDebugLogger(); logger != nil {
		switch obj := v.(type) {
		case *MessagePacket:
			logger.Debugf("[Serialization] 序列化MessagePacket: ID=%s, Type=%s, Content=%s", obj.ID, obj.Type, obj.Content)
		case *Message:
			logger.Debugf("[Serialization] 序列化Message: ID=%s, Type=%s, Content=%s", obj.ID, obj.Type, obj.Content)
		default:
			logger.Debugf("[Serialization] 序列化对象: Type=%T", v)
		}
	}
	
	// 使用优化的JSON序列化器
	jsonOptimizer := performance.GetJSONOptimizer()
	result, err := jsonOptimizer.Marshal(v)
	
	if err != nil {
		if logger := getDebugLogger(); logger != nil {
			logger.Errorf("[Serialization] 序列化失败: %v", err)
		}
		return nil, err
	}
	
	// 添加序列化后的调试日志
	if logger := getDebugLogger(); logger != nil {
		logger.Debugf("[Serialization] 序列化完成: 数据长度=%d, 数据预览=%s", len(result), string(result[:minInt(len(result), 200)]))
	}
	
	return result, nil
}

// UnmarshalJSONPooled 使用对象池进行JSON反序列化（已优化）
func UnmarshalJSONPooled(data []byte, v interface{}) error {
	// 添加反序列化前的调试日志
	if logger := getDebugLogger(); logger != nil {
		logger.Debugf("[Deserialization] 开始反序列化: 目标类型=%T, 数据长度=%d, 数据预览=%s", v, len(data), string(data[:minInt(len(data), 200)]))
	}
	
	// 使用优化的JSON反序列化器
	jsonOptimizer := performance.GetJSONOptimizer()
	err := jsonOptimizer.Unmarshal(data, v)
	
	// 添加反序列化后的调试日志
	if err != nil {
		if logger := getDebugLogger(); logger != nil {
			logger.Errorf("[Deserialization] 反序列化失败: %v, 数据=%s", err, string(data))
		}
	} else {
		if logger := getDebugLogger(); logger != nil {
			switch obj := v.(type) {
			case *MessagePacket:
				logger.Debugf("[Deserialization] 反序列化MessagePacket成功: ID=%s, Type=%s, Content=%s", obj.ID, obj.Type, obj.Content)
			case *Message:
				logger.Debugf("[Deserialization] 反序列化Message成功: ID=%s, Type=%s, Content=%s", obj.ID, obj.Type, obj.Content)
			default:
				logger.Debugf("[Deserialization] 反序列化成功: 类型=%T", v)
			}
		}
	}
	
	return err
}

// minInt 返回两个整数中的较小值
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// PineconeServiceLike 供 handler 解耦依赖
// 只包含 handler 需要用到的方法

type PineconeServiceLike interface {
    GetPineconeAddr() string
    GetNetworkInfo() map[string]interface{}
    SendMessagePacket(toAddr string, packet *MessagePacket) error
    // 统一的ACK/NACK发送接口，供各处理器使用
    SendAckMessage(toAddr, originalMsgID, status string) error
    SendNackMessage(toAddr, originalMsgID, reason string, errorCode int, retryAfter int) error
    GetAllPeerAddrs() []string
    GetStartTime() time.Time
    FriendList() FriendListLike
    GetUsernameByPubKey(pubkey string) (string, bool)
    GetPubKeyByUsername(username string) (string, bool)
    GetPublicKeyHex() string
    GetMessageChannel() chan *Message
}

// FriendListLike 供 handler 查询好友
// 只包含 handler 需要用到的方法

type FriendListLike interface {
	GetAllFriends() []*friend.Friend
	GetCurrentAccount() *friend.Friend
}