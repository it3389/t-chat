package cmd

import (
	"t-chat/internal/chat"
	"t-chat/internal/file"
	"t-chat/internal/friend"
	"t-chat/internal/network"
	"go.uber.org/zap"
)

// zapLoggerAdapter 适配器，将*zap.Logger适配为file.Logger接口
type zapLoggerAdapter struct {
	logger *zap.Logger
}

func (z *zapLoggerAdapter) Infof(format string, args ...interface{}) {
	z.logger.Sugar().Infof(format, args...)
}

func (z *zapLoggerAdapter) Debugf(format string, args ...interface{}) {
	z.logger.Sugar().Debugf(format, args...)
}

func (z *zapLoggerAdapter) Errorf(format string, args ...interface{}) {
	z.logger.Sugar().Errorf(format, args...)
}

// 全局变量声明（所有命令模块共享）
var (
	friendList              *friend.List
	msgStore                *chat.MessageStore
	pineconeService         *network.PineconeService
	fileTransmissionManager file.TransmissionManagerInterface
)

// SetFriendList 设置好友列表
func SetFriendList(fl *friend.List) {
	friendList = fl
	updateChatHandlerDependencies()
}

// SetMsgStore 设置消息存储
func SetMsgStore(store *chat.MessageStore) {
	msgStore = store
	updateChatHandlerDependencies()
}

// SetPineconeService 设置 Pinecone 服务
func SetPineconeService(ps *network.PineconeService) {
	pineconeService = ps
	updateChatHandlerDependencies()
}

// SetFileTransmissionManager 设置文件传输管理器
func SetFileTransmissionManager(ftm file.TransmissionManagerInterface) {
	fileTransmissionManager = ftm
	updateChatHandlerDependencies()
}

// updateChatHandlerDependencies 更新聊天处理器的依赖项
func updateChatHandlerDependencies() {
	if msgStore != nil && pineconeService != nil && friendList != nil {
		SetChatHandlerDependencies(msgStore, pineconeService, friendList)
		
		// 如果文件传输管理器未初始化，则创建一个
		if fileTransmissionManager == nil {
			// 创建一个简单的logger（如果需要的话）
			logger, _ := zap.NewDevelopment()
			adapter := &zapLoggerAdapter{logger: logger}
			fileTransmissionManager = file.NewTransmissionManager(adapter)
		}
		
		// 同时设置switch命令的依赖项
		if chatHandler != nil {
			SetSwitchHandlerDependencies(msgStore, pineconeService, friendList, chatHandler)
			SetSwitchHandlerFileTransmissionManager(fileTransmissionManager)
		}
	}
}