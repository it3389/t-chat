package network



// AckMessageHandler 消息确认处理器
type AckMessageHandler struct {
	pineconeService PineconeServiceLike
	logger         Logger
	ackCallbacks   map[string]func(ack MessageAck) // 消息ID -> 回调函数
}

// NewAckMessageHandler 创建确认消息处理器
func NewAckMessageHandler(p PineconeServiceLike, logger Logger) *AckMessageHandler {
	return &AckMessageHandler{
		pineconeService: p,
		logger:         logger,
		ackCallbacks:   make(map[string]func(ack MessageAck)),
	}
}

// HandleMessage 处理确认消息
func (h *AckMessageHandler) HandleMessage(msg *Message) error {
	// 收到确认消息
	h.logger.Debugf("[AckHandler] 🔔 收到ACK消息: From=%s, ID=%s", msg.From, msg.ID)
	
	// 解析确认数据
	ackData, ok := msg.Metadata["ack_data"]
	if !ok {
		h.logger.Warnf("[AckHandler] ⚠️ 消息中没有确认数据: %+v", msg.Metadata)
		return nil
	}
	
	// 转换为MessageAck结构
	var ack MessageAck
	ackBytes, err := MarshalJSONPooled(ackData)
	if err != nil {
		h.logger.Errorf("[AckHandler] 序列化确认数据失败: %v", err)
		return nil
	}
	if err := UnmarshalJSONPooled(ackBytes, &ack); err != nil {
		h.logger.Errorf("[AckHandler] 解析确认数据失败: %v", err)
		return nil
	}
	
	h.logger.Debugf("[AckHandler] ✅ 解析ACK成功: OriginalMsgID=%s, Status=%s", ack.OriginalMsgID, ack.Status)
	
	// 通知等待确认的消息
	if ackNotifier, ok := h.pineconeService.(interface {
		NotifyAckReceived(string, bool)
	}); ok {
		ackNotifier.NotifyAckReceived(ack.OriginalMsgID, ack.Status == "delivered" || ack.Status == "read")
		h.logger.Debugf("[AckHandler] 已通知ACK接收: %s", ack.OriginalMsgID)
	}
	
	// 调用回调函数
	if callback, exists := h.ackCallbacks[ack.OriginalMsgID]; exists {
		h.logger.Debugf("[AckHandler] 🎯 执行ACK回调: %s", ack.OriginalMsgID)
		callback(ack)
		delete(h.ackCallbacks, ack.OriginalMsgID) // 清理回调
	} else {
		// 未注册回调在多数非交互场景属于正常情况（例如仅持久化状态或不需终端提示）
		// 为减少终端噪声，将告警降级为信息日志，同时保留诊断信息
		h.logger.Infof("[AckHandler] ℹ️ 未找到ACK回调（非致命）: %s, 当前回调数: %d", ack.OriginalMsgID, len(h.ackCallbacks))
	}
	
	return nil
}

// RegisterAckCallback 注册确认回调
func (h *AckMessageHandler) RegisterAckCallback(msgID string, callback func(ack MessageAck)) {
	h.ackCallbacks[msgID] = callback
}

// GetMessageType 获取消息类型
func (h *AckMessageHandler) GetMessageType() string {
	return MessageTypeAck
}

// GetPriority 获取优先级
func (h *AckMessageHandler) GetPriority() int {
	return MessagePriorityHigh
}

// CanHandle 检查是否能处理消息
func (h *AckMessageHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == MessageTypeAck
}

// Initialize 初始化
func (h *AckMessageHandler) Initialize() error {
	return nil
}

// Cleanup 清理
func (h *AckMessageHandler) Cleanup() error {
	h.ackCallbacks = make(map[string]func(ack MessageAck))
	return nil
}