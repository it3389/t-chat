package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"t-chat/internal/network"
)

// TraceMessageHandler 路由追踪消息处理器
type TraceMessageHandler struct {
	pineconeService network.PineconeServiceLike
	logger          network.Logger
}

// NewTraceMessageHandler 创建新的路由追踪消息处理器
func NewTraceMessageHandler(p network.PineconeServiceLike, logger network.Logger) *TraceMessageHandler {
	return &TraceMessageHandler{pineconeService: p, logger: logger}
}

// HandleMessage 处理路由追踪消息
func (h *TraceMessageHandler) HandleMessage(msg *network.Message) error {
	
	
	if msg.Type != network.MessageTypeCommand {
		return nil
	}
	
	// 检查是否为追踪命令
	if !strings.HasPrefix(strings.ToLower(msg.Content), "trace") {
		return nil
	}
	

	
	// 解析追踪参数
	traceInfo := h.parseTraceMessage(msg)
	if traceInfo == nil {
		return fmt.Errorf("无法解析追踪消息")
	}
	
	// 生成追踪响应
	response := h.generateTraceResponse(msg, traceInfo)
	
	// 发送响应
	if err := h.pineconeService.SendMessagePacket(msg.From, &response); err != nil {
		// 发送追踪响应失败
		return err
	}
	
	
	return nil
}

// TraceInfo 追踪信息
type TraceInfo struct {
	TraceID  string
	MaxHops  int
	Source   string
	Path     []string
	HopCount int
}

// parseTraceMessage 解析追踪消息
func (h *TraceMessageHandler) parseTraceMessage(msg *network.Message) *TraceInfo {
	info := &TraceInfo{
		Source:   msg.From,
		Path:     []string{msg.From},
		HopCount: 1,
	}
	
	// 从元数据中提取信息
	if msg.Metadata != nil {
		if traceID, ok := msg.Metadata["trace_id"].(string); ok {
			info.TraceID = traceID
		}
		if maxHops, ok := msg.Metadata["max_hops"].(int); ok {
			info.MaxHops = maxHops
		} else if maxHopsStr, ok := msg.Metadata["max_hops"].(string); ok {
			if maxHops, err := strconv.Atoi(maxHopsStr); err == nil {
				info.MaxHops = maxHops
			}
		}
		if source, ok := msg.Metadata["source"].(string); ok {
			info.Source = source
		}
		if path, ok := msg.Metadata["path"].([]interface{}); ok {
			for _, hop := range path {
				if hopStr, ok := hop.(string); ok {
					info.Path = append(info.Path, hopStr)
				}
			}
		}
	}
	
	// 设置默认值
	if info.MaxHops == 0 {
		info.MaxHops = 8
	}
	if info.TraceID == "" {
		info.TraceID = fmt.Sprintf("trace_%d", time.Now().UnixNano())
	}
	
	// 添加当前节点到路径
	myPubKey := h.pineconeService.GetPineconeAddr()
	info.Path = append(info.Path, myPubKey)
	info.HopCount = len(info.Path)
	
	return info
}

// generateTraceResponse 生成追踪响应
func (h *TraceMessageHandler) generateTraceResponse(msg *network.Message, traceInfo *TraceInfo) network.MessagePacket {
	// 获取当前节点信息
	myPubKey := h.pineconeService.GetPineconeAddr()
	networkInfo := h.pineconeService.GetNetworkInfo()
	
	// 构建响应数据
	responseData := map[string]interface{}{
		"trace_id":    traceInfo.TraceID,
		"hop_number":  traceInfo.HopCount,
		"node_pubkey": myPubKey,
		"timestamp":   time.Now().UnixNano(),
		"path":        traceInfo.Path,
		"max_hops":    traceInfo.MaxHops,
		"source":      traceInfo.Source,
	}
	
	// 添加节点详细信息
	if networkInfo != nil {
		if nodeID, ok := networkInfo["node_id"].(string); ok {
			responseData["node_id"] = nodeID
		}
		if listenAddr, ok := networkInfo["listen_addr"].(string); ok {
			responseData["listen_addr"] = listenAddr
		}
		if peerCount, ok := networkInfo["peer_count"].(int); ok {
			responseData["peer_count"] = peerCount
		}
	}
	
	// 添加用户名信息
	if username, ok := h.pineconeService.GetUsernameByPubKey(myPubKey); ok {
		responseData["username"] = username
	}
	
	// 添加连接信息
	peers := networkInfo["peers"].([]map[string]interface{})
	peerInfo := make([]map[string]interface{}, 0)
	for _, peer := range peers {
		peerData := map[string]interface{}{
			"public_key": peer["public_key"],
			"peer_type":  peer["peer_type"],
		}
		if remoteIP, ok := peer["remote_ip"].(string); ok {
			peerData["remote_ip"] = remoteIP
		}
		if remotePort, ok := peer["remote_port"].(int); ok {
			peerData["remote_port"] = remotePort
		}
		if zone, ok := peer["zone"].(string); ok {
			peerData["zone"] = zone
		}
		peerInfo = append(peerInfo, peerData)
	}
	responseData["peers"] = peerInfo
	
	return network.MessagePacket{
		From:     myPubKey,
		To:       traceInfo.Source,
		Type:     network.MessageTypeCommand,
		Content:  "trace_response",
		Metadata: responseData,
	}
}

// GetMessageType 获取消息类型
func (h *TraceMessageHandler) GetMessageType() string {
	return network.MessageTypeCommand
}

// GetPriority 获取优先级
func (h *TraceMessageHandler) GetPriority() int {
	return network.MessagePriorityHigh
}
