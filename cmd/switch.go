// Package cmd 提供t-chat应用程序的命令行界面
//
// 聊天模式切换命令模块负责处理聊天模式的切换和管理，包括：
// - 切换到聊天循环模式
// - 在聊天模式中处理快捷命令
// - 管理聊天会话状态
package cmd

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"t-chat/internal/chat"
	"t-chat/internal/file"
	"t-chat/internal/network"

	"github.com/spf13/cobra"
)

// SwitchCommandHandler 聊天模式切换处理器
//
// 核心功能：
// - 处理聊天模式的切换
// - 管理聊天循环模式
// - 处理聊天模式中的快捷命令
// - 维护聊天会话状态
type SwitchCommandHandler struct {
	msgStore                *chat.MessageStore                // 消息存储管理器
	pineconeService         interface{}                       // Pinecone网络服务
	friendList              interface{}                       // 好友列表管理器
	chatHandler             *ChatCommandHandler               // 聊天命令处理器
	fileTransmissionManager file.TransmissionManagerInterface // 文件传输管理器（旧版本）
	fileTransferIntegration *network.FileTransferIntegration  // 文件传输集成器（新版本）
}

// NewSwitchCommandHandler 创建聊天模式切换处理器
func NewSwitchCommandHandler() *SwitchCommandHandler {
	return &SwitchCommandHandler{}
}

// SetDependencies 设置依赖项
func (h *SwitchCommandHandler) SetDependencies(msgStore *chat.MessageStore, pineconeService, friendList interface{}, chatHandler *ChatCommandHandler) {
	h.msgStore = msgStore
	h.pineconeService = pineconeService
	h.friendList = friendList
	h.chatHandler = chatHandler
}

// SetFileTransmissionManager 设置文件传输管理器
func (h *SwitchCommandHandler) SetFileTransmissionManager(ftm file.TransmissionManagerInterface) {
	h.fileTransmissionManager = ftm
}

// SetFileTransferIntegration 设置文件传输集成器
func (h *SwitchCommandHandler) SetFileTransferIntegration(fti *network.FileTransferIntegration) {
	h.fileTransferIntegration = fti
}

// sendFileViaPinecone 通过Pinecone发送文件
func (h *SwitchCommandHandler) sendFileViaPinecone(target, filePath string) error {
	if h.fileTransmissionManager == nil {
		return fmt.Errorf("文件传输管理器未初始化")
	}

	// 创建Pinecone连接适配器
	adapter := NewPineconeConnAdapter(h.pineconeService, h.friendList, target)

	// 开始文件传输（使用默认分片大小64KB）
	return h.fileTransmissionManager.SendFile(adapter, filePath, target, 65536)
}

// PineconeConnAdapter Pinecone连接适配器，实现net.Conn接口
type PineconeConnAdapter struct {
	pineconeService interface{}
	friendList      interface{}
	target          string
	receiveBuffer   []byte
	receiveChan     chan []byte
}

// NewPineconeConnAdapter 创建新的Pinecone连接适配器
func NewPineconeConnAdapter(pineconeService interface{}, friendList interface{}, target string) *PineconeConnAdapter {
	return &PineconeConnAdapter{
		pineconeService: pineconeService,
		friendList:      friendList,
		target:          target,
		receiveChan:     make(chan []byte, 100), // 缓冲通道
	}
}

// 实现net.Conn接口的方法
func (p *PineconeConnAdapter) Read(b []byte) (n int, err error) {
	// 从接收通道读取数据
	select {
	case data := <-p.receiveChan:
		n = copy(b, data)
		return n, nil
	case <-time.After(30 * time.Second): // 30秒超时
		return 0, fmt.Errorf("read timeout")
	}
}

func (p *PineconeConnAdapter) Write(b []byte) (n int, err error) {
	// 通过Pinecone发送文件数据
	if ps, ok := p.pineconeService.(interface {
		SendMessagePacket(string, *network.MessagePacket) error
	}); ok {
		// 获取目标地址（支持好友用户名或直接使用公钥地址）
		var pineconeAddr string
		
		// 检查target是否为64字符的十六进制公钥地址
		if len(p.target) == 64 {
			// 验证是否为有效的十六进制字符串
			if _, err := hex.DecodeString(p.target); err == nil {
				pineconeAddr = p.target
			} else {
				return 0, fmt.Errorf("无效的公钥地址格式: %s", p.target)
			}
		} else {
			// 尝试从好友列表获取地址
			if fl, ok := p.friendList.(interface {
				GetPineconeAddr(string) (string, error)
			}); ok {
				addr, err := fl.GetPineconeAddr(p.target)
				if err != nil {
					return 0, fmt.Errorf("好友 '%s' 不存在: %v", p.target, err)
				}
				// 验证获取到的地址格式
				if len(addr) != 64 {
					return 0, fmt.Errorf("好友 '%s' 的 Pinecone 地址格式无效 (长度: %d，期望: 64): %s", p.target, len(addr), addr)
				}
				pineconeAddr = addr
			} else {
				return 0, fmt.Errorf("好友列表服务不可用")
			}
		}
		
		// 创建文件数据消息包
		packet := &network.MessagePacket{
			ID:        fmt.Sprintf("file_%d", time.Now().UnixNano()),
			Type:      "file_data",
			From:      "", // 将由SendMessagePacket自动设置
			To:        p.target,
			Content:   "file_chunk",
			Data:      b,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"chunk_size": len(b),
			},
		}
		
		// 使用公钥地址发送消息包
		err := ps.SendMessagePacket(pineconeAddr, packet)
		if err != nil {
			return 0, fmt.Errorf("failed to send file data via Pinecone: %v", err)
		}
		return len(b), nil
	}
	return 0, fmt.Errorf("PineconeService does not support SendMessagePacket")
}

func (p *PineconeConnAdapter) Close() error {
	return nil
}

func (p *PineconeConnAdapter) LocalAddr() net.Addr {
	return nil
}

func (p *PineconeConnAdapter) RemoteAddr() net.Addr {
	return nil
}

func (p *PineconeConnAdapter) SetDeadline(t time.Time) error {
	return nil
}

func (p *PineconeConnAdapter) SetReadDeadline(t time.Time) error {
	return nil
}

func (p *PineconeConnAdapter) SetWriteDeadline(t time.Time) error {
	return nil
}

// HandleSwitchToChat 处理切换到聊天模式命令
//
// 参数：
//   target - 聊天目标用户名或公钥
//
// 处理逻辑：
//   1. 验证目标用户
//   2. 进入聊天循环模式
//   3. 处理用户输入和快捷命令
func (h *SwitchCommandHandler) HandleSwitchToChat(target string) {
	reader := bufio.NewReader(os.Stdin)
	
	// 如果没有指定目标，交互式输入
	if target == "" {
		fmt.Print("请输入聊天目标用户名或公钥: ")
		input, _ := reader.ReadString('\n')
		target = strings.TrimSpace(input)
	}
	
	// 验证目标用户
	if target == "" {
		fmt.Println("错误：聊天目标不能为空")
		return
	}
	
	// 查找目标用户的Pinecone地址
	pineconeAddr := h.chatHandler.FindPineconeAddress(target)
	if pineconeAddr == "" {
		fmt.Printf("警告：未找到用户 %s 的在线节点，但仍可以发送消息\n", target)
		fmt.Println("消息将在目标用户上线时送达")
	}
	
	// 显示聊天模式提示
	fmt.Printf("\n=== 进入与 %s 的聊天模式 ===\n", target)
	fmt.Println("快捷命令：")
	fmt.Println("  /q     - 退出聊天模式")
	fmt.Println("  /f     - 发送文件")
	fmt.Println("  /fp    - 查看文件发送情况")
	fmt.Println("  /help  - 显示帮助信息")
	fmt.Println("直接输入消息内容即可发送，按回车确认")
	fmt.Println("========================================\n")
	
	// 进入聊天循环模式
	h.enterChatLoop(target, pineconeAddr, reader)
}

// enterChatLoop 进入聊天循环模式
//
// 参数：
//   target - 聊天目标
//   pineconeAddr - 目标的Pinecone地址
//   reader - 输入读取器
func (h *SwitchCommandHandler) enterChatLoop(target, pineconeAddr string, reader *bufio.Reader) {
	for {
		fmt.Printf("[%s] > ", target)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			continue
		}
		
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		
		// 处理快捷命令
		if strings.HasPrefix(input, "/") {
			if h.handleShortcutCommand(input, target, pineconeAddr) {
				// 如果返回true，表示退出聊天模式
				break
			}
			continue
		}
		
		// 发送普通消息
		h.sendChatMessage(target, input, pineconeAddr)
	}
}

// handleShortcutCommand 处理快捷命令
//
// 参数：
//   command - 快捷命令
//   target - 聊天目标
//   pineconeAddr - 目标的Pinecone地址
//
// 返回值：
//   bool - 是否退出聊天模式
func (h *SwitchCommandHandler) handleShortcutCommand(command, target, pineconeAddr string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	
	cmd := parts[0]
	switch cmd {
	case "/q", "/quit", "/exit":
		fmt.Println("退出聊天模式，返回主命令模式")
		return true
		
	case "/f", "/file":
		h.handleSendFile(target, parts[1:])
		
	case "/fp", "/fileprogress":
		h.handleFileProgress()
		
	case "/help", "/h":
		h.showChatHelp()
		
	case "/history", "/hist":
		h.showChatHistory(target)
		
	default:
		fmt.Printf("未知命令: %s\n", cmd)
		fmt.Println("输入 /help 查看可用命令")
	}
	
	return false
}

// sendChatMessage 发送聊天消息
func (h *SwitchCommandHandler) sendChatMessage(target, message, pineconeAddr string) {
	if h.chatHandler != nil {
		h.chatHandler.CreateAndSendMessage(target, message, pineconeAddr, false, 8*time.Second)
	} else {
		fmt.Println("错误：聊天处理器未初始化")
	}
}

// handleSendFile 处理发送文件命令
func (h *SwitchCommandHandler) handleSendFile(target string, args []string) {
	var filePath string
	if len(args) == 0 {
		fmt.Print("请输入文件路径: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		filePath = strings.TrimSpace(input)
	} else {
		filePath = strings.Join(args, " ")
	}
	
	if filePath == "" {
		fmt.Println("❌ 文件路径不能为空")
		return
	}
	
	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("❌ 文件不存在: %s\n", filePath)
		return
	}
	
	// 获取目标地址（支持好友用户名或直接使用公钥地址）
	var actualTarget string
	
	// 检查target是否为64字符的十六进制公钥地址
	if len(target) == 64 {
		// 验证是否为有效的十六进制字符串
		if _, err := hex.DecodeString(target); err == nil {
			actualTarget = target
			fmt.Printf("📤 开始发送文件 %s 给公钥地址 %s\n", filePath, target)
		} else {
			fmt.Printf("❌ 无效的公钥地址格式: %s\n", target)
			return
		}
	} else {
		// 尝试通过用户名查找公钥地址
		if h.chatHandler != nil {
			pineconeAddr := h.chatHandler.FindPineconeAddress(target)
			if pineconeAddr == "" {
				fmt.Printf("❌ 未找到用户 %s 的公钥地址，无法发送文件\n", target)
				fmt.Println("请确保：")
				fmt.Println("1. 输入的是正确的用户名或64位十六进制公钥")
				fmt.Println("2. 目标用户已添加为好友或直接使用公钥地址")
				return
			}
			actualTarget = pineconeAddr
			fmt.Printf("📤 开始发送文件 %s 给 %s (公钥: %s)\n", filePath, target, pineconeAddr)
		} else {
			fmt.Println("❌ 聊天处理器未初始化")
			return
		}
	}
	
	// 调用文件发送功能 - 优先使用新的文件传输集成器
	if h.fileTransferIntegration != nil && h.fileTransferIntegration.IsInitialized() {
		// 使用新的文件传输集成器
		fmt.Printf("📤 使用新文件传输服务发送文件...\n")
		if err := h.fileTransferIntegration.SendFile(filePath, actualTarget); err != nil {
			fmt.Printf("❌ 文件发送失败: %v\n", err)
		} else {
			fmt.Printf("✅ 文件发送请求已提交: %s\n", filePath)
		}
	} else {
		// 回退到旧的文件传输管理器
		fmt.Printf("📤 使用旧文件传输服务发送文件...\n")
		if err := h.sendFileViaPinecone(actualTarget, filePath); err != nil {
			fmt.Printf("❌ 文件发送失败: %v\n", err)
		} else {
			fmt.Printf("✅ 文件发送完成: %s\n", filePath)
		}
	}
}

// handleFileProgress 处理查看文件发送情况命令
func (h *SwitchCommandHandler) handleFileProgress() {
	fmt.Println("=== 文件传输状态 ===")
	
	// 获取文件传输管理器的状态
	if h.fileTransmissionManager != nil {
		sessions := h.fileTransmissionManager.ListSessions()
		if len(sessions) == 0 {
			fmt.Println("📋 当前没有活跃的文件传输任务")
		} else {
			fmt.Printf("📋 活跃的文件传输任务 (%d个):\n", len(sessions))
			for i, sessionID := range sessions {
				session := h.fileTransmissionManager.GetSession(sessionID)
				if session != nil {
					fmt.Printf("  %d. 文件ID: %s\n", i+1, sessionID)
					fmt.Printf("     状态: %s\n", session.Status)
					fmt.Printf("     进度: %.1f%%\n", session.Progress)
				}
			}
		}
	} else {
		fmt.Println("⚠️ 文件传输管理器未初始化")
	}
	
	fmt.Println("=====================")
}

// showChatHelp 显示聊天模式帮助信息
func (h *SwitchCommandHandler) showChatHelp() {
	fmt.Println("\n=== 聊天模式帮助 ===")
	fmt.Println("快捷命令：")
	fmt.Println("  /q, /quit, /exit    - 退出聊天模式")
	fmt.Println("  /f, /file [路径]    - 发送文件")
	fmt.Println("  /fp, /fileprogress  - 查看文件发送情况")
	fmt.Println("  /history, /hist     - 查看聊天历史")
	fmt.Println("  /help, /h           - 显示此帮助信息")
	fmt.Println("")
	fmt.Println("使用说明：")
	fmt.Println("  - 直接输入消息内容即可发送")
	fmt.Println("  - 按回车键确认发送")
	fmt.Println("  - 空消息将被忽略")
	fmt.Println("========================\n")
}

// showChatHistory 显示聊天历史
func (h *SwitchCommandHandler) showChatHistory(target string) {
	if h.chatHandler != nil {
		fmt.Printf("\n=== 与 %s 的聊天历史 ===\n", target)
		h.chatHandler.HandleChatHistory(target)
		fmt.Println("============================\n")
	} else {
		fmt.Println("错误：聊天处理器未初始化")
	}
}

// 全局变量
var (
	switchTarget  string
	switchHandler *SwitchCommandHandler
)

// switchCmd 切换到聊天模式命令
var switchCmd = &cobra.Command{
	Use:   "switch [target]",
	Short: "切换到与指定用户的聊天模式",
	Long: `切换到聊天循环模式，可以与指定用户进行持续对话。

在聊天模式中支持以下快捷命令：
  /q     - 退出聊天模式
  /f     - 发送文件
  /fp    - 查看文件发送情况
  /help  - 显示帮助信息

示例：
  tchat switch alice        # 切换到与alice的聊天模式
  tchat switch --target bob # 使用参数指定聊天目标`,
	Run: func(cmd *cobra.Command, args []string) {
		if switchHandler == nil {
			fmt.Println("错误：聊天模式处理器未初始化")
			return
		}
		
		target := switchTarget
		if len(args) > 0 {
			target = args[0]
		}
		
		switchHandler.HandleSwitchToChat(target)
	},
}

// SetSwitchHandlerDependencies 设置切换处理器的依赖项
func SetSwitchHandlerDependencies(msgStore *chat.MessageStore, pineconeService, friendList interface{}, chatHandler *ChatCommandHandler) {
	if switchHandler == nil {
		switchHandler = NewSwitchCommandHandler()
	}
	switchHandler.SetDependencies(msgStore, pineconeService, friendList, chatHandler)
}

// SetSwitchHandlerFileTransmissionManager 设置切换处理器的文件传输管理器
func SetSwitchHandlerFileTransmissionManager(ftm file.TransmissionManagerInterface) {
	if switchHandler == nil {
		switchHandler = NewSwitchCommandHandler()
	}
	switchHandler.SetFileTransmissionManager(ftm)
}

// SetSwitchHandlerFileTransferIntegration 设置切换处理器的文件传输集成器
func SetSwitchHandlerFileTransferIntegration(fti *network.FileTransferIntegration) {
	if switchHandler == nil {
		switchHandler = NewSwitchCommandHandler()
	}
	switchHandler.SetFileTransferIntegration(fti)
}

// initSwitchCommands 初始化切换命令
func initSwitchCommands() {
	// 初始化处理器
	if switchHandler == nil {
		switchHandler = NewSwitchCommandHandler()
	}
	
	// 添加参数
	switchCmd.Flags().StringVarP(&switchTarget, "target", "t", "", "聊天目标用户名或公钥")
	
	// 注册到根命令
	RootCmd.AddCommand(switchCmd)
}

func init() {
	// 初始化切换命令
	initSwitchCommands()
}