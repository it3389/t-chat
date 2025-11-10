package network



// NackMessageHandler 否定确认消息处理器
type NackMessageHandler struct {
	pineconeService PineconeServiceLike
	logger         Logger
	nackCallbacks  map[string]func(nack MessageNack) // 消息ID -> 回调函数
}

// NewNackMessageHandler 创建否定确认消息处理器
func NewNackMessageHandler(p PineconeServiceLike, logger Logger) *NackMessageHandler {
	return &NackMessageHandler{
		pineconeService: p,
		logger:         logger,
		nackCallbacks:  make(map[string]func(nack MessageNack)),
	}
}

// HandleMessage 处理否定确认消息
func (h *NackMessageHandler) HandleMessage(msg *Message) error {
	// 收到否定确认消息
	
	// 解析否定确认数据
	nackData, ok := msg.Metadata["nack_data"]
	if !ok {
		// 否定确认消息缺少nack_data
		return nil
	}
	
	// 转换为MessageNack结构
	var nack MessageNack
	if nackBytes, err := MarshalJSONPooled(nackData); err != nil {
		// 序列化nack_data失败
		return nil
	} else if err := UnmarshalJSONPooled(nackBytes, &nack); err != nil {
		// 反序列化nack_data失败
		return nil
	}
	
	// 消息失败处理
	
	if nack.RetryAfter > 0 {
		// 建议重试
	}
	
	// 调用回调函数
	if callback, exists := h.nackCallbacks[nack.OriginalMsgID]; exists {
		callback(nack)
		delete(h.nackCallbacks, nack.OriginalMsgID) // 清理回调
	}
	
	return nil
}

// RegisterNackCallback 注册否定确认回调
func (h *NackMessageHandler) RegisterNackCallback(msgID string, callback func(nack MessageNack)) {
	h.nackCallbacks[msgID] = callback
}

// GetMessageType 获取消息类型
func (h *NackMessageHandler) GetMessageType() string {
	return MessageTypeNack
}

// GetPriority 获取优先级
func (h *NackMessageHandler) GetPriority() int {
	return MessagePriorityHigh
}

// CanHandle 检查是否能处理消息
func (h *NackMessageHandler) CanHandle(msg *Message) bool {
	return msg != nil && msg.Type == MessageTypeNack
}

// Initialize 初始化
func (h *NackMessageHandler) Initialize() error {
	return nil
}

// Cleanup 清理
func (h *NackMessageHandler) Cleanup() error {
	h.nackCallbacks = make(map[string]func(nack MessageNack))
	return nil
}