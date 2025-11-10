package handler

import (
	"fmt"
	"strings"
	"t-chat/internal/network"
)

type CommandMessageHandler struct {
	pineconeService network.PineconeServiceLike
	logger         network.Logger
}

func NewCommandMessageHandler(p network.PineconeServiceLike, logger network.Logger) *CommandMessageHandler {
	return &CommandMessageHandler{pineconeService: p, logger: logger}
}

func (h *CommandMessageHandler) HandleMessage(msg *network.Message) error {
	parts := strings.Fields(msg.Content)
	if len(parts) == 0 {
		return fmt.Errorf("空命令")
	}
	command := parts[0]
	// 命令处理逻辑 - 仅在需要时输出详细信息
	switch command {
	case "ping", "status", "time", "version", "uptime", "memory", "network", "peers", "help":
		// 命令执行 - 仅在需要时输出结果
	default:
		// 未知命令处理
	}
	return nil
}

func (h *CommandMessageHandler) GetMessageType() string {
	return network.MessageTypeCommand
}

func (h *CommandMessageHandler) GetPriority() int {
	return network.MessagePriorityHigh
}