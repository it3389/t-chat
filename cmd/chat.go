// Package cmd 提供t-chat应用程序的命令行界面
//
// 聊天命令模块负责处理聊天相关的所有操作，包括：
// - 消息发送和接收
// - 聊天历史记录查看
// - 消息加密和解密
// - 好友通信管理
package cmd

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"t-chat/internal/chat"

	"github.com/spf13/cobra"
)

// ChatCommandHandler 聊天命令处理器
//
// 核心功能：
// - 处理所有聊天相关的CLI命令
// - 管理消息发送和接收逻辑
// - 维护聊天历史记录
// - 协调网络服务和好友列表
//
// 设计目标：
// - 封装聊天命令的处理逻辑
// - 提供统一的消息管理接口
// - 确保消息安全传输和存储
type ChatCommandHandler struct {
	msgStore        *chat.MessageStore // 消息存储管理器
	pineconeService interface{}        // Pinecone网络服务（使用interface{}避免循环依赖）
	friendList      interface{}        // 好友列表管理器（使用interface{}避免循环依赖）
}

// NewChatCommandHandler 创建聊天命令处理器
//
// 返回值：
//   *ChatCommandHandler - 聊天命令处理器实例
//
// 注意：
//   创建后需要通过SetDependencies方法设置依赖项
func NewChatCommandHandler() *ChatCommandHandler {
	return &ChatCommandHandler{}
}

// SetDependencies 设置依赖项
//
// 参数：
//   msgStore - 消息存储管理器
//   pineconeService - Pinecone网络服务接口
//   friendList - 好友列表管理器接口
//
// 处理逻辑：
//   1. 注入消息存储管理器
//   2. 注入网络服务接口
//   3. 注入好友列表管理器
func (h *ChatCommandHandler) SetDependencies(msgStore *chat.MessageStore, pineconeService, friendList interface{}) {
	h.msgStore = msgStore
	h.pineconeService = pineconeService
	h.friendList = friendList
}

// generateMessageID 生成唯一的消息ID
//
// 返回值：
//   string - 32字符的十六进制字符串作为消息唯一标识符
//
// 处理逻辑：
//   1. 生成16字节的随机数据
//   2. 转换为十六进制字符串
//   3. 返回作为消息ID
//
// 安全性：
//   使用crypto/rand确保随机性，避免ID冲突
func (h *ChatCommandHandler) generateMessageID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// HandleSendMessage 处理发送消息命令
//
// 参数：
//   args - 命令行参数
//   to - 接收方用户名（可为空，将交互式输入）
//   msg - 消息内容（可为空，将交互式输入）
//
// 处理逻辑：
//   1. 优先使用命令行参数
//   2. 缺少参数时交互式输入
//   3. 查找接收方的Pinecone地址
//   4. 创建并发送加密消息
//
// 交互模式：
//   - 支持通过命令行参数直接指定
//   - 支持交互式输入接收方和消息内容
func (h *ChatCommandHandler) HandleSendMessage(args []string, to, msg string) {
	reader := bufio.NewReader(os.Stdin)
	
	// 优先使用命令行参数
	if len(args) >= 2 {
		to = args[0]
		msg = strings.Join(args[1:], " ")
	}
	
	// 交互式输入接收方
	if to == "" {
		fmt.Print("请输入用户名或公钥: ")
		input, _ := reader.ReadString('\n')
		to = strings.TrimSpace(input)
	}
	
	// 交互式输入消息内容
	if msg == "" {
		fmt.Print("请输入消息内容: ")
		input, _ := reader.ReadString('\n')
		msg = strings.TrimSpace(input)
	}
	
	// 输入验证
	if to == "" {
		fmt.Println("错误：接收方不能为空")
		return
	}
	if msg == "" {
		fmt.Println("错误：消息内容不能为空")
		return
	}
	
	// 查找 Pinecone 地址
	pineconeAddr := h.FindPineconeAddress(to)
	if pineconeAddr == "" {
		fmt.Println("未找到该用户名或公钥对应的在线节点，无法发送消息。请确保：")
		fmt.Println("1. 输入的是正确的用户名或64位十六进制公钥")
		fmt.Println("2. 目标节点当前在线并已连接到网络")
		return
	}
	
	// 创建并发送消息（根据开关决定是否等待送达确认）
	h.CreateAndSendMessage(to, msg, pineconeAddr, sendConfirm, ackTimeout)
}

// FindPineconeAddress 查找用户的 Pinecone 地址
//
// 参数：
//   to - 目标用户名或公钥
//
// 返回值：
//   string - Pinecone地址，未找到时返回空字符串
//
// 查找策略：
//   1. 检查是否为有效的公钥格式（64位十六进制字符串）
//   2. 通过用户名映射查找对应的公钥
//   3. 完全信任Pinecone网络路由，不做任何连接检测
func (h *ChatCommandHandler) FindPineconeAddress(to string) string {
	if h.pineconeService == nil {
		return ""
	}
	
	// 尝试获取PineconeService的方法
	ps, ok := h.pineconeService.(interface {
		GetPubKeyByUsername(string) (string, bool)
		GetUsernameByPubKey(string) (string, bool)
	})
	if !ok {
		return ""
	}
	
	var targetPubKey string
	
	// 检查输入是否为公钥格式（64位十六进制字符串）
	if len(to) == 64 {
		// 验证是否为有效的十六进制字符串
		if _, err := hex.DecodeString(to); err == nil {
			targetPubKey = to
		}
	}
	
	// 如果不是公钥格式，尝试通过用户名查找公钥
	if targetPubKey == "" {
		if pubKey, found := ps.GetPubKeyByUsername(to); found && pubKey != "" {
			targetPubKey = pubKey
		}
	}
	
	// 如果仍然没有找到公钥，返回空
	if targetPubKey == "" {
		return ""
	}
	
	// 直接返回目标公钥，完全信任Pinecone网络路由
	// 不做任何连接检测，让Pinecone网络自行处理路由
	
	return targetPubKey
}

// CreateAndSendMessage 创建并发送消息
// 包括消息创建、本地存储、网络发送和状态更新
// 优化后的版本：信任Pinecone网络路由能力，改进错误处理
//
// 参数：
//   to - 接收方用户名
//   msg - 消息内容
//   pineconeAddr - 接收方的Pinecone地址
//
// 处理逻辑：
//   1. 生成唯一消息ID
//   2. 创建消息对象
//   3. 通过Pinecone网络发送消息
//   4. 保存消息到本地存储
//   5. 输出发送结果
//
// 异常情况：
//   - 网络发送失败：显示警告但不立即标记为失败
//   - 消息存储失败：显示警告信息
func (h *ChatCommandHandler) CreateAndSendMessage(to, msg, pineconeAddr string, confirm bool, ackTimeout time.Duration) {
	// 创建消息
	msgID := h.generateMessageID()
	message := &chat.Message{
		ID:        msgID,
		From:      "me",
		To:        to,
		Content:   msg,
		Type:      "text",
		Timestamp: time.Now(),
		Status:    "sending", // 初始状态为发送中
	}
	
	// 检查消息存储是否初始化
	if h.msgStore == nil {
		fmt.Println("错误：消息存储未初始化")
		return
	}
	
	// 通过 Pinecone 网络发送消息
	if h.pineconeService != nil {
		// 注册ACK回调，当收到确认时显示双勾
		ackDone := make(chan bool, 1)
		if ackService, ok := h.pineconeService.(interface {
			RegisterAckCallback(string, func(interface{})) error
		}); ok {
			// 注册ACK回调函数
			_ = ackService.RegisterAckCallback(msgID, func(ackData interface{}) {
				// 持久化标记为已送达
				if h.msgStore != nil {
					if err := h.msgStore.MarkAsDelivered(msgID); err != nil {
						// 仅输出提示，不影响交互显示
						fmt.Printf(" (标记送达失败: %v)", err)
					}
				}
				// 收到ACK确认，在同一行追加双勾
				fmt.Printf(" ✓ (已送达)")
				select {
				case ackDone <- true:
				default:
				}
			})
		}
		
		if ps, ok := h.pineconeService.(interface {
			SendTextMessage(string, string, string) error
		}); ok {
			// 使用SendTextMessage发送文本消息，这样可以确保正确的消息类型和处理流程
			if err := ps.SendTextMessage(pineconeAddr, msg, msgID); err != nil {
				// 发送失败，显示错误信息
				currentTime := time.Now().Format("15:04:05")
				fmt.Printf("[%s] 我 → %s: %s ✗ (发送失败: %v)\n", currentTime, to, msg, err)
				message.Status = "failed"
				return
			} else {
				// 发送成功，显示单勾状态（不换行，等待ACK确认）
				currentTime := time.Now().Format("15:04:05")
				fmt.Printf("[%s] 我 → %s: %s ✓", currentTime, to, msg)
				message.Status = "sent" // 标记为已发送，等待ACK确认
			}
			
			// 只保存一次消息到本地存储（无论发送成功还是失败）
			if err := h.msgStore.AddMessage(message); err != nil {
				fmt.Println("保存消息失败:", err)
			}

			// 根据参数决定是否等待ACK送达确认
			if confirm {
				select {
				case <-ackDone:
					message.Status = "delivered"
					fmt.Printf("\n")
				case <-time.After(ackTimeout):
					fmt.Printf(" ⏳ (未确认)\n")
				}
			} else {
				// 不等待确认，直接换行
				fmt.Printf("\n")
			}
		} else {
			fmt.Println("❌ Pinecone 服务类型不匹配")
			message.Status = "failed"
			h.msgStore.AddMessage(message)
		}
	} else {
		fmt.Println("❌ Pinecone 服务未初始化")
		message.Status = "failed"
		h.msgStore.AddMessage(message)
	}
}

// 全局变量声明
var (
	sendTo      string
	sendMsg     string
	sendConfirm bool
	ackTimeout  time.Duration
	historyWith string
	chatHandler *ChatCommandHandler
)

// chatCmd 聊天主命令
var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "聊天相关命令",
	Long:  "聊天功能模块，提供消息发送和历史记录查看功能",
}

// sendMsgCmd 发送消息命令
var sendMsgCmd = &cobra.Command{
	Use:   "send",
	Short: "发送消息给用户",
	Long:  "通过 Pinecone 网络向指定用户发送加密消息。支持用户名或公钥作为接收方，支持 --to 和 --msg 参数，未指定时将交互式输入。",
	Run: func(cmd *cobra.Command, args []string) {
		if chatHandler == nil {
			fmt.Println("错误：聊天处理器未初始化")
			return
		}
		chatHandler.HandleSendMessage(args, sendTo, sendMsg)
	},
}

// HandleChatHistory 处理查看聊天记录命令
// 支持通过命令行参数或交互式输入指定要查看的好友
// HandleChatHistory 处理查看聊天历史命令
//
// 参数：
//   with - 聊天对象用户名（可为空，将交互式输入）
//
// 处理逻辑：
//   1. 验证或获取聊天对象用户名
//   2. 从消息存储中获取历史记录
//   3. 格式化并显示消息列表
//   4. 高亮显示新消息
//
// 交互模式：
//   - 支持通过参数直接指定聊天对象
//   - 支持交互式输入聊天对象用户名
func (h *ChatCommandHandler) HandleChatHistory(with string) {
	reader := bufio.NewReader(os.Stdin)
	
	// 交互式输入好友用户名
	if with == "" {
		fmt.Print("请输入好友用户名: ")
		input, _ := reader.ReadString('\n')
		with = strings.TrimSpace(input)
	}
	
	// 输入验证
	if with == "" {
		fmt.Println("错误：好友用户名不能为空")
		return
	}
	
	// 检查消息存储是否初始化
	if h.msgStore == nil {
		fmt.Println("错误：消息存储未初始化")
		return
	}
	
	// 获取聊天记录
	msgs := h.msgStore.GetMessages("me", with)
	if len(msgs) == 0 {
		fmt.Printf("未找到与 %s 的聊天记录\n", with)
		return
	}
	
	// 显示聊天记录
	fmt.Printf("与 %s 的聊天记录：\n", with)
	for _, m := range msgs {
		h.formatAndPrintMessage(m)
	}
}

// formatAndPrintMessage 格式化并打印消息
// 新消息（10秒内）使用绿色高亮显示
// formatAndPrintMessage 格式化并打印消息
//
// 参数：
//   msg - 要显示的消息对象
//
// 处理逻辑：
//   1. 格式化时间戳
//   2. 确定消息方向（发送/接收）
//   3. 应用颜色和格式
//   4. 输出到控制台
//
// 显示格式：
//   [时间] 发送方 -> 接收方: 消息内容
func (h *ChatCommandHandler) formatAndPrintMessage(msg *chat.Message) {
	// 过滤掉系统内部消息类型，避免显示原始JSON
	if msg.Type == "user_info_exchange" {
		// 用户信息交换消息不显示详细内容
		return
	}
	
	timeStr := msg.Timestamp.Format("15:04:05")
	
	// 根据消息来源确定显示格式
	if msg.From == "me" {
		// 自己发送的消息
		if time.Since(msg.Timestamp) < 10*time.Second {
			// 绿色高亮新消息
			fmt.Printf("\033[1;32m[%s] 我 → %s: %s\033[0m\n", timeStr, msg.To, msg.Content)
		} else {
			fmt.Printf("[%s] 我 → %s: %s\n", timeStr, msg.To, msg.Content)
		}
	} else {
		// 接收到的消息，使用高亮显示
		if time.Since(msg.Timestamp) < 10*time.Second {
			// 绿色高亮新消息
			fmt.Printf("\033[1;32m[%s] %s → 我: %s\033[0m\n", timeStr, msg.From, msg.Content)
		} else {
			fmt.Printf("\033[1;36m[%s] %s → 我: %s\033[0m\n", timeStr, msg.From, msg.Content)
		}
	}
}

// historyCmd 查看聊天记录命令
var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "查看与指定好友的聊天记录",
	Long:  "通过 --with 参数快速查看与指定好友的历史消息，未指定时交互式输入。新消息高亮显示。",
	Run: func(cmd *cobra.Command, args []string) {
		if chatHandler == nil {
			fmt.Println("错误：聊天处理器未初始化")
			return
		}
		chatHandler.HandleChatHistory(historyWith)
	},
}

// SetChatHandlerDependencies 设置聊天处理器的依赖项
// SetChatHandlerDependencies 设置聊天处理器的依赖项
//
// 参数：
//   msgStore - 消息存储管理器
//   pineconeService - Pinecone网络服务接口
//   friendList - 好友列表管理器接口
//
// 处理逻辑：
//   1. 确保聊天处理器已初始化
//   2. 设置所有必要的依赖项
//
// 注意：
//   此函数用于在应用启动时注入依赖项
func SetChatHandlerDependencies(msgStore *chat.MessageStore, pineconeService, friendList interface{}) {
	if chatHandler == nil {
		chatHandler = NewChatCommandHandler()
	}
	chatHandler.SetDependencies(msgStore, pineconeService, friendList)
}

// initChatCommands 初始化聊天相关命令
// initChatCommands 初始化聊天相关命令
//
// 处理逻辑：
//   1. 创建聊天命令处理器
//   2. 配置发送消息命令的参数
//   3. 配置历史记录命令的参数
//   4. 将子命令添加到主聊天命令
//   5. 将聊天命令添加到根命令
//
// 命令结构：
//   chat
//   ├── send    # 发送消息
//   └── history # 查看历史记录
func initChatCommands() {
	// 初始化聊天处理器
	chatHandler = NewChatCommandHandler()
	
	// 添加帮助标志和处理器

	
	// 注册命令
	RootCmd.AddCommand(chatCmd)
	chatCmd.AddCommand(sendMsgCmd)
	chatCmd.AddCommand(historyCmd)
	
	// 设置命令行标志
	sendMsgCmd.Flags().StringVarP(&sendTo, "to", "t", "", "接收方用户名")
	sendMsgCmd.Flags().StringVarP(&sendMsg, "msg", "m", "", "要发送的消息内容")
	sendMsgCmd.Flags().BoolVarP(&sendConfirm, "confirm", "c", false, "发送后等待送达确认")
	sendMsgCmd.Flags().DurationVar(&ackTimeout, "ack-timeout", 8*time.Second, "等待送达确认的超时时间")
	historyCmd.Flags().StringVarP(&historyWith, "with", "w", "", "要查看的好友用户名")
}

func init() {
	initChatCommands()
}
