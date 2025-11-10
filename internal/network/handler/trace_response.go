package handler

import (
	"fmt"
	"strconv"
	"time"
	"t-chat/internal/network"
)

// TraceResponseHandler 追踪响应处理器
type TraceResponseHandler struct {
	pineconeService network.PineconeServiceLike
	logger          network.Logger
	responseChan    chan *TraceResponseData
}

// TraceResponseData 追踪响应数据
type TraceResponseData struct {
	TraceID     string                 `json:"trace_id"`
	HopNumber   int                    `json:"hop_number"`
	NodePubKey  string                 `json:"node_pubkey"`
	Username    string                 `json:"username"`
	Timestamp   int64                  `json:"timestamp"`
	Path        []string               `json:"path"`
	MaxHops     int                    `json:"max_hops"`
	Source      string                 `json:"source"`
	NodeInfo    map[string]interface{} `json:"node_info"`
	PeerInfo    []map[string]interface{} `json:"peer_info"`
}

// NewTraceResponseHandler 创建新的追踪响应处理器
func NewTraceResponseHandler(p network.PineconeServiceLike, logger network.Logger) *TraceResponseHandler {
	return &TraceResponseHandler{
		pineconeService: p,
		logger:          logger,
		responseChan:    make(chan *TraceResponseData, 100),
	}
}

// HandleMessage 处理追踪响应消息
func (h *TraceResponseHandler) HandleMessage(msg *network.Message) error {

	
	if msg.Type != network.MessageTypeCommand {
		return nil
	}
	
	// 检查是否为追踪响应
	if msg.Content != "trace_response" {
		return nil
	}
	

	
	// 解析响应数据
	responseData := h.parseTraceResponse(msg)
	if responseData == nil {
		return fmt.Errorf("无法解析追踪响应")
	}
	
	// 发送到响应通道
	select {
	case h.responseChan <- responseData:
		// 响应已发送到通道
	default:
		// 追踪响应通道已满，丢弃响应
	}
	
	return nil
}

// parseTraceResponse 解析追踪响应
func (h *TraceResponseHandler) parseTraceResponse(msg *network.Message) *TraceResponseData {
	data := &TraceResponseData{
		NodePubKey: msg.From,
		Timestamp:  time.Now().UnixNano(),
	}
	
	if msg.Metadata == nil {
		return nil
	}
	
	// 解析基本字段
	if traceID, ok := msg.Metadata["trace_id"].(string); ok {
		data.TraceID = traceID
	}
	if hopNumber, ok := msg.Metadata["hop_number"].(int); ok {
		data.HopNumber = hopNumber
	} else if hopNumberStr, ok := msg.Metadata["hop_number"].(string); ok {
		if hopNumber, err := strconv.Atoi(hopNumberStr); err == nil {
			data.HopNumber = hopNumber
		}
	}
	if nodePubKey, ok := msg.Metadata["node_pubkey"].(string); ok {
		data.NodePubKey = nodePubKey
	}
	if username, ok := msg.Metadata["username"].(string); ok {
		data.Username = username
	}
	if timestamp, ok := msg.Metadata["timestamp"].(int64); ok {
		data.Timestamp = timestamp
	}
	if maxHops, ok := msg.Metadata["max_hops"].(int); ok {
		data.MaxHops = maxHops
	} else if maxHopsStr, ok := msg.Metadata["max_hops"].(string); ok {
		if maxHops, err := strconv.Atoi(maxHopsStr); err == nil {
			data.MaxHops = maxHops
		}
	}
	if source, ok := msg.Metadata["source"].(string); ok {
		data.Source = source
	}
	
	// 解析路径
	if path, ok := msg.Metadata["path"].([]interface{}); ok {
		for _, hop := range path {
			if hopStr, ok := hop.(string); ok {
				data.Path = append(data.Path, hopStr)
			}
		}
	}
	
	// 解析节点信息
	if nodeInfo, ok := msg.Metadata["node_info"].(map[string]interface{}); ok {
		data.NodeInfo = nodeInfo
	} else {
		// 从其他字段构建节点信息
		data.NodeInfo = make(map[string]interface{})
		if nodeID, ok := msg.Metadata["node_id"].(string); ok {
			data.NodeInfo["node_id"] = nodeID
		}
		if listenAddr, ok := msg.Metadata["listen_addr"].(string); ok {
			data.NodeInfo["listen_addr"] = listenAddr
		}
		if peerCount, ok := msg.Metadata["peer_count"].(int); ok {
			data.NodeInfo["peer_count"] = peerCount
		}
	}
	
	// 解析对等节点信息
	if peers, ok := msg.Metadata["peers"].([]interface{}); ok {
		for _, peer := range peers {
			if peerMap, ok := peer.(map[string]interface{}); ok {
				data.PeerInfo = append(data.PeerInfo, peerMap)
			}
		}
	}
	
	return data
}

// GetResponseChannel 获取响应通道
func (h *TraceResponseHandler) GetResponseChannel() <-chan *TraceResponseData {
	return h.responseChan
}

// GetMessageType 获取消息类型
func (h *TraceResponseHandler) GetMessageType() string {
	return network.MessageTypeCommand
}

// GetPriority 获取优先级
func (h *TraceResponseHandler) GetPriority() int {
	return network.MessagePriorityHigh
}
