// Package main 是t-chat应用程序的主入口点
//
// t-chat是一个基于Pinecone协议的去中心化聊天应用，支持：
// - 点对点消息传输
// - 文件分享功能
// - 好友管理系统
// - 多种网络传输方式（TCP、蓝牙、多播）
//
// 主要功能：
// - 账户管理：创建、登录、切换用户账户
// - 聊天通信：发送文本消息、表情符号
// - 文件传输：支持大文件分片传输
// - 网络发现：mDNS自动发现、好友搜索
//
// 使用方式：
//   tchat --help           # 查看帮助信息
//   tchat account create   # 创建新账户
//   tchat chat start       # 启动聊天模式
//   tchat file send        # 发送文件
package main

import (
	"bufio"
	"context"
	"encoding/hex"

	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"t-chat/cmd"
	"t-chat/config"
	"t-chat/internal/account"
	"t-chat/internal/chat"
	"t-chat/internal/cli"
	"t-chat/internal/friend"
	"t-chat/internal/logger"
	"t-chat/internal/network"
	"t-chat/internal/pinecone/bluetooth"
)

// Application 应用程序主类
//
// 核心功能：
// - 应用程序初始化和启动
// - 配置管理和日志系统初始化
// - 各模块的依赖注入和协调
//
// 设计目标：
// - 提供清晰的应用程序启动流程
// - 统一管理应用程序生命周期
// - 确保各模块正确初始化
type Application struct {
	config           *config.Config
	logger           *logger.Logger
	accountMgr       *account.Manager
	loginService     account.LoginService
	pineconeService  *network.PineconeService
	mdnsService      *network.MDNSService
	bluetoothService *bluetooth.BluetoothService
	serviceManager   *network.ServiceManager
	networkCtx       context.Context
	networkCancel    context.CancelFunc
	currentPassword  string // 当前用户密码，用于解密私钥
	networkConfig    *network.NetworkConfig // 网络配置
	autoUsername     string // 自动登录用户名
	autoPassword     string // 自动登录密码
	fileTransferIntegration *network.FileTransferIntegration // 文件传输集成器
}

// NewApplication 创建新的应用程序实例
//
// 参数：
//   configPath - 可选的配置文件路径
//
// 返回值：
//   *Application - 应用程序实例
//   error - 初始化失败时的错误信息
//
// 处理逻辑：
//   1. 加载应用程序配置
//   2. 初始化日志系统
//   3. 创建账户管理器
//   4. 创建登录服务
//   5. 设置模块间依赖关系
func NewApplication(configPath ...string) (*Application, error) {
	return NewApplicationWithLogin("", "", configPath...)
}

// NewApplicationWithLogin 创建带自动登录的应用程序实例
func NewApplicationWithLogin(username, password string, configPath ...string) (*Application, error) {
	// 加载配置文件，获取应用程序运行参数
	cfg, err := config.LoadConfig(configPath...)
	if err != nil {
		return nil, err
	}

	// 初始化日志系统，用于记录应用程序运行状态
	// 从配置中解析日志级别
	logLevel := logger.ParseLogLevel(cfg.LogLevel)
	appLogger := logger.NewLoggerWithLevel(cfg.LogFile, logLevel)

	// 创建账户管理器，负责用户身份管理
	accountMgr := account.NewManager()
	// 设置全局账户管理器
	account.SetGlobalManager(accountMgr)

	// 创建登录服务，负责用户登录验证
	loginService := account.NewLoginService(accountMgr)

	// 创建网络配置
	networkConfig := network.DefaultNetworkConfig()
	// 从全局配置中设置Pinecone监听端口
	if cfg.PineconeListen != "" {
		networkConfig.PineconeListen = cfg.PineconeListen
	}
	// 从全局配置中设置Pinecone对等节点
	if len(cfg.PineconePeers) > 0 {
		networkConfig.PineconePeers = cfg.PineconePeers
	}

	// 创建网络服务管理器
	serviceManager := network.NewServiceManager(networkConfig, appLogger)

	// 创建Pinecone服务
	pineconeService := network.NewPineconeService(networkConfig, appLogger)

	// 创建文件传输集成器，确保文件传输处理器正确初始化
	fileTransferIntegration := network.NewFileTransferIntegration(pineconeService, appLogger)

	// 创建MDNS服务
	mdnsService := network.NewMDNSService(networkConfig, appLogger)

	// 创建蓝牙服务
	bluetoothService := bluetooth.NewBluetoothService(appLogger, pineconeService.GetRouter())

	// 创建网络上下文
	networkCtx, networkCancel := context.WithCancel(context.Background())

	return &Application{
		config:           cfg,
		logger:           appLogger,
		accountMgr:       accountMgr,
		loginService:     loginService,
		pineconeService:  pineconeService,
		mdnsService:      mdnsService,
		bluetoothService: bluetoothService,
		serviceManager:   serviceManager,
		networkCtx:       networkCtx,
		networkCancel:    networkCancel,
		networkConfig:    networkConfig,
		autoUsername:     username,
		autoPassword:     password,
		fileTransferIntegration: fileTransferIntegration,
	}, nil
}

// Start 启动应用程序
//
// 处理逻辑：
//   1. 记录启动信息
//   2. 强制要求用户登录
//   3. 初始化网络服务
//   4. 检查是否有快捷命令，有则执行快捷命令，否则进入交互式循环
//
// 异常情况：
//   - 如果登录失败，程序将退出
//   - 如果命令执行失败，显示错误但继续运行
func (app *Application) Start() {
	app.logger.Info("🚀 启动TChat CLI模式...")

	// 检查是否有自动登录参数
	var password string
	var err error
	if app.autoUsername != "" && app.autoPassword != "" {
		app.logger.Info("🔍 使用自动登录...")
		password, err = app.loginService.AutoLoginWithPassword(app.autoUsername, app.autoPassword)
		if err != nil {
			log.Fatalf("自动登录失败: %v", err)
		}
	} else {
		// 使用交互式登录
		app.logger.Info("🔍 进入交互式登录...")
		password, err = app.loginService.RequireLoginWithPassword()
		if err != nil {
			log.Fatalf("登录失败: %v", err)
		}
	}

	// 保存密码用于后续解密私钥
	app.currentPassword = password

	// 初始化网络服务
	app.logger.Info("🔄 正在初始化网络服务...")
	if err := app.initializeNetworkServices(); err != nil {
		log.Fatalf("网络服务初始化失败: %v", err)
	}
	app.logger.Info("✅ 网络服务初始化完成")

	// 将账户管理器注入到CLI命令中，实现依赖注入
	cmd.SetAccountManager(app.accountMgr)

	app.logger.Info("✅ TChat CLI模式启动完成")

	// 检查是否有快捷命令参数
	if app.hasQuickCommand() {
		// 执行快捷命令后退出
		app.executeQuickCommand()
		return
	}

	app.logger.Info("💡 输入 'help' 查看可用命令，输入 'exit' 或 'quit' 退出程序")

	// 进入交互式命令循环
	app.runInteractiveLoop()
}

// initializeNetworkServices 初始化网络服务
//
// 处理逻辑：
//   1. 获取当前登录账户的密钥
//   2. 设置Pinecone服务的密钥对
//   3. 启动网络服务管理器
//   4. 启动Pinecone服务
//   5. 将服务注入到命令模块
func (app *Application) initializeNetworkServices() error {
	// 获取当前登录的账户
	currentAccount := app.accountMgr.GetCurrentAccount()
	if currentAccount == nil {
		return fmt.Errorf("未找到当前登录账户")
	}

	// 检查账户是否有明文公钥（登录时已解密）
	if currentAccount.PublicKey == "" {
		return fmt.Errorf("账户公钥未解密，请重新登录")
	}

	// 将十六进制公钥字符串解码为字节数组
	publicKeyBytes, err := hex.DecodeString(currentAccount.PublicKey)
	if err != nil {
		return fmt.Errorf("解码公钥失败: %v", err)
	}

	// 获取账户的私钥（需要密码解密）
	// 注意：这里需要从登录过程中获取密码，或者要求用户重新输入
	// 为了简化，我们假设密码已经在某个地方存储或可以获取
	// 在实际应用中，应该从安全的地方获取密码
	var privateKeyBytes []byte
	if app.currentPassword != "" {
		_, privateKey, err := currentAccount.DecryptKeys(app.currentPassword)
		if err != nil {
			return fmt.Errorf("解密私钥失败: %v", err)
		}
		privateKeyBytes = privateKey
	}

	// 设置Pinecone服务的密钥对（包含私钥和公钥）
	app.pineconeService.SetKeys(privateKeyBytes, publicKeyBytes)

	// 设置CLI文件传输确认回调（在初始化之前设置）
	app.logger.Info("🔄 正在设置文件传输确认回调...")
	downloadDir := "./downloads" // 默认下载目录
	cliCallback := cli.NewCLIFileTransferCallback(downloadDir)
	app.fileTransferIntegration.SetUserConfirmationCallback(cliCallback)
	app.logger.Info("✅ 文件传输确认回调设置完成")

	// 初始化文件传输集成器
	app.logger.Info("🔄 正在初始化文件传输集成器...")
	if err := app.fileTransferIntegration.Initialize(); err != nil {
		return fmt.Errorf("初始化文件传输集成器失败: %v", err)
	}
	app.logger.Info("✅ 文件传输集成器初始化完成")

	// 设置SwitchCommandHandler的文件传输集成器
	app.logger.Info("🔄 正在设置SwitchCommandHandler文件传输集成器...")
	cmd.SetSwitchHandlerFileTransferIntegration(app.fileTransferIntegration)
	app.logger.Info("✅ SwitchCommandHandler文件传输集成器设置完成")

	// 注册Pinecone服务到服务管理器
	app.serviceManager.RegisterService("pinecone", app.pineconeService)

	// 注册MDNS服务到服务管理器
	app.serviceManager.RegisterService("mdns", app.mdnsService)

	// 心跳检测机制已禁用

	// 启动服务管理器
	app.logger.Info("🔄 正在启动服务管理器...")
	if err := app.serviceManager.Start(app.networkCtx); err != nil {
		return fmt.Errorf("启动服务管理器失败: %v", err)
	}
	app.logger.Info("✅ 服务管理器启动完成")

	// 等待Pinecone服务启动并获取实际监听端口
	app.logger.Info("⏳ 等待Pinecone服务完全启动...")
	time.Sleep(1 * time.Second) // 给Pinecone服务一些时间来启动和查找可用端口
	app.logger.Info("✅ Pinecone服务等待完成")

	// 启动MDNS服务进行节点发现（改为配置并公告，避免重复启动）
	app.logger.Info("🔄 正在配置MDNS服务...")
	if currentAccount != nil {
	    // 从Pinecone服务获取实际的监听端口信息
	    actualPort := 7777 // 默认端口
	    
	    // 获取Pinecone服务的网络信息，包含实际监听地址
	    if app.pineconeService != nil {
	        networkInfo := app.pineconeService.GetNetworkInfo()
	        if listeningAddresses, ok := networkInfo["listening_addresses"].([]string); ok && len(listeningAddresses) > 0 {
	            // 从第一个监听地址中提取端口
	            firstAddr := listeningAddresses[0]
	            app.logger.Infof("从Pinecone服务获取监听地址: %s", firstAddr)
	            
	            // 解析地址格式：ip:port 或 pinecone://ip:port
	            addrToParse := firstAddr
	            if strings.Contains(firstAddr, "://") {
	                addrToParse = strings.Split(firstAddr, "://")[1]
	            }
	            
	            if strings.Contains(addrToParse, ":") {
	                portStr := strings.Split(addrToParse, ":")[1]
	                if parsedPort, err := strconv.Atoi(portStr); err == nil {
	                    actualPort = parsedPort
	                    app.logger.Infof("从监听地址解析到端口: %d", actualPort)
	                }
	            }
	        } else {
	            app.logger.Warn("未能从Pinecone服务获取监听地址，使用默认端口")
	        }
	    }
	    
	    // 使用 StartMDNS 启动 mDNS 服务并配置广播信息
    err = app.mdnsService.StartMDNS(currentAccount.Username, "", currentAccount.PublicKey, actualPort)
    if err != nil {
        app.logger.Errorf("启动MDNS服务失败: %v", err)
    } else {
        app.logger.Infof("✅ MDNS服务启动完成 (端口: %d)", actualPort)
    }
	} else {
		app.logger.Info("⚠️ MDNS服务配置跳过 - 无当前账户")
	}

	// 启动蓝牙服务
	app.logger.Info("🔄 正在启动蓝牙服务...")
	if err := app.bluetoothService.Start(); err != nil {
		app.logger.Errorf("启动蓝牙服务失败: %v", err)
		// 蓝牙服务启动失败不应该阻止程序运行
	} else {
		app.logger.Info("✅ 蓝牙服务启动成功")
	}
	
	// 将蓝牙服务设置到PineconeService中
	app.logger.Info("🔄 正在创建蓝牙适配器...")
	bluetoothAdapter := network.NewBluetoothServiceAdapter(app.bluetoothService)
	app.logger.Info("✅ 蓝牙适配器创建成功")
	// 设置获取蓝牙设备的函数
	app.logger.Info("🔄 正在设置蓝牙设备获取函数...")
	bluetoothAdapter.SetGetPeersFunc(func() []network.BluetoothPeerInfo {
		// 使用带超时的channel来避免死锁
		resultChan := make(chan []network.BluetoothPeerInfo, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					resultChan <- []network.BluetoothPeerInfo{}
				}
			}()
			peers := app.bluetoothService.GetDiscoveredPeers()
			result := make([]network.BluetoothPeerInfo, 0, len(peers))
			for _, peer := range peers {
				peerInfo := network.BluetoothPeerInfo{
					Name:      peer.Name,
					Address:   peer.Address,
					PublicKey: peer.PublicKey,
					Connected: peer.Connected,
					LastSeen:  peer.LastSeen,
				}
				result = append(result, peerInfo)
			}
			resultChan <- result
		}()
		
		// 使用超时机制避免死锁
		select {
		case result := <-resultChan:
			return result
		case <-time.After(100 * time.Millisecond):
			// 超时返回空列表，避免死锁
			return []network.BluetoothPeerInfo{}
		}
	})
	app.logger.Info("✅ 蓝牙设备获取函数设置完成")
	app.logger.Info("🔄 正在设置蓝牙服务到Pinecone...")
	app.pineconeService.SetBluetoothService(bluetoothAdapter)
	app.logger.Info("✅ 蓝牙服务设置到Pinecone完成")

	// 将MDNS服务设置到PineconeService中，实现自动节点发现和连接
	app.logger.Info("🔄 正在设置MDNS服务到Pinecone...")
	app.pineconeService.SetMDNSService(app.mdnsService)
	app.logger.Info("✅ MDNS服务设置到Pinecone完成")

	// 创建好友列表管理器
	app.logger.Info("🔄 正在创建好友列表管理器...")
	friendList := friend.NewList()
	// 设置当前登录账户
	friendList.SetCurrentAccount(currentAccount.Username)
	app.logger.Info("✅ 好友列表管理器创建完成")
	
	// 创建消息存储管理器
	app.logger.Info("🔄 正在创建消息存储管理器...")
	msgStore := chat.NewMessageStore()
	app.logger.Info("✅ 消息存储管理器创建完成")
	
	// 将服务注入到命令模块
	app.logger.Info("🔄 正在注入服务到命令模块...")
	cmd.SetPineconeService(app.pineconeService)
	cmd.SetFriendList(friendList)
	cmd.SetMsgStore(msgStore)
	app.logger.Info("✅ 服务注入到命令模块完成")

	app.logger.Info("🎉 网络服务初始化完成")
	return nil
}

// runInteractiveLoop 运行交互式命令循环
//
// 处理逻辑：
//   1. 显示命令提示符
//   2. 读取用户输入
//   3. 解析并执行命令
//   4. 处理特殊命令（exit、quit、help）
//   5. 循环直到用户退出
func (app *Application) runInteractiveLoop() {
	reader := bufio.NewReader(os.Stdin)
	currentUser := app.accountMgr.GetCurrentAccount()
	userPrompt := "tchat"
	if currentUser != nil {
		userPrompt = fmt.Sprintf("tchat@%s", currentUser.Username)
	}

	// 添加调试信息确认进入交互循环
	app.logger.Info("🎯 进入交互式命令循环")
	fmt.Println("\n=== TChat 交互式命令行 ===")
	fmt.Println("💡 输入 'help' 查看可用命令，输入 'exit' 或 'quit' 退出程序")

	for {
		// 显示命令提示符
		fmt.Printf("\n%s> ", userPrompt)
		// 强制刷新输出缓冲区
		os.Stdout.Sync()

		// 读取用户输入
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			continue
		}

		// 清理输入
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// 处理特殊命令
		if app.handleSpecialCommands(input) {
			return // 退出程序
		}

		// 执行普通命令
		app.executeCommand(input)
	}
}

// handleSpecialCommands 处理特殊命令
//
// 参数：
//   input - 用户输入的命令
//
// 返回值：
//   bool - 是否应该退出程序
func (app *Application) handleSpecialCommands(input string) bool {
	switch strings.ToLower(input) {
	case "exit", "quit", "q":
		fmt.Println("\n👋 再见！感谢使用TChat")
		// 清理网络服务
		app.cleanup()
		return true
	case "clear", "cls":
		app.clearScreen()
		return false
	default:
		return false
	}
}

// executeCommand 执行普通命令
//
// 参数：
//   input - 用户输入的命令字符串
func (app *Application) executeCommand(input string) {
	// 将输入转换为命令行参数格式
	args := strings.Fields(input)
	if len(args) == 0 {
		return
	}

	// 创建一个临时的根命令副本，避免重复执行Start()
	tempRootCmd := &cobra.Command{
		Use:   "tchat",
		Short: "TChat - 完全去中心化CLI聊天工具",
		Long:  `TChat 是完全去中心化 CLI 聊天系统。`,
		// 不设置Run函数，避免重复调用Start()
	}

	// 复制所有子命令到临时根命令
	for _, subCmd := range cmd.RootCmd.Commands() {
		tempRootCmd.AddCommand(subCmd)
	}

	// 设置参数并执行
	tempRootCmd.SetArgs(args)
	if err := tempRootCmd.Execute(); err != nil {
		// 为避免潜在的信息泄露，这里不再输出底层错误详情或命令建议
		fmt.Println("❌ 命令执行失败")
		fmt.Println("💡 输入 'help' 查看可用命令")
	}
}



// clearScreen 清空屏幕
func (app *Application) clearScreen() {
	fmt.Print("\033[H\033[2J")
	fmt.Println("✅ 屏幕已清空")
}

// hasQuickCommand 检查是否有快捷命令参数
func (app *Application) hasQuickCommand() bool {
	// 获取命令行参数，跳过程序名
	args := os.Args[1:]
	
	// 过滤掉登录相关的参数
	var filteredArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// 跳过登录相关参数
		if arg == "--username" || arg == "--password" || arg == "--config" || arg == "--log-level" || arg == "--account" {
			// 跳过参数名和参数值
			if i+1 < len(args) {
				i++ // 跳过参数值
			}
			continue
		}
		// 跳过以--开头的参数（如--username=value格式）
		if strings.HasPrefix(arg, "--username=") || strings.HasPrefix(arg, "--password=") || 
		   strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--log-level=") || strings.HasPrefix(arg, "--account=") {
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}
	
	// 如果有剩余参数，说明有快捷命令
	return len(filteredArgs) > 0
}

// executeQuickCommand 执行快捷命令
func (app *Application) executeQuickCommand() {
	// 获取命令行参数，跳过程序名
	args := os.Args[1:]
	
	// 检查是否是--login命令，如果是则直接启动交互式模式
	for i := 0; i < len(args); i++ {
		if args[i] == "--login" {
			app.logger.Infof("🚀 执行快捷命令: %s", strings.Join(args, " "))
			// 等待网络服务发现和连接其他节点
			app.logger.Info("⏳ 等待网络节点发现和连接...")
			time.Sleep(15 * time.Second) // 给网络服务更多时间来发现和连接节点
			app.logger.Info("✅ 网络节点发现等待完成")
			// 启动交互式聊天模式
			app.runInteractiveLoop()
			return
		}
	}
	
	// 过滤掉登录相关的参数
	var filteredArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// 跳过登录相关参数
		if arg == "--username" || arg == "--password" || arg == "--config" || arg == "--log-level" {
			// 跳过参数名和参数值
			if i+1 < len(args) {
				i++ // 跳过参数值
			}
			continue
		}
		// 跳过以--开头的参数（如--username=value格式）
		if strings.HasPrefix(arg, "--username=") || strings.HasPrefix(arg, "--password=") || 
		   strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--log-level=") {
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}
	
	if len(filteredArgs) == 0 {
		return
	}
	
	app.logger.Infof("🚀 执行快捷命令: %s", strings.Join(filteredArgs, " "))
	
	// 等待网络服务发现和连接其他节点
	app.logger.Info("⏳ 等待网络节点发现和连接...")
	time.Sleep(15 * time.Second) // 给网络服务更多时间来发现和连接节点
	app.logger.Info("✅ 网络节点发现等待完成")
	
	// 创建一个临时的根命令副本
	tempRootCmd := &cobra.Command{
		Use:   "tchat",
		Short: "TChat - 完全去中心化CLI聊天工具",
		Long:  `TChat 是完全去中心化 CLI 聊天系统。`,
	}
	
	// 复制所有子命令到临时根命令
	for _, subCmd := range cmd.RootCmd.Commands() {
		tempRootCmd.AddCommand(subCmd)
	}
	
	// 设置参数并执行
	tempRootCmd.SetArgs(filteredArgs)
	if err := tempRootCmd.Execute(); err != nil {
		fmt.Printf("❌ 快捷命令执行失败: %v\n", err)
		os.Exit(1)
	}
	
	// 执行完快捷命令后清理资源
	app.cleanup()
}

// cleanup 清理资源
//
// 处理逻辑：
//   1. 停止网络服务管理器
//   2. 停止Pinecone服务
//   3. 取消网络上下文
func (app *Application) cleanup() {
	if app.serviceManager != nil {
		if err := app.serviceManager.Stop(); err != nil {
			app.logger.Errorf("停止服务管理器失败: %v", err)
		}
	}

	if app.pineconeService != nil {
		if err := app.pineconeService.Stop(); err != nil {
			app.logger.Errorf("停止Pinecone服务失败: %v", err)
		}
	}

	if app.networkCancel != nil {
		app.networkCancel()
	}

	app.logger.Info("🧹 资源清理完成")
}

// main 程序主入口函数
//
// 处理逻辑：
//   1. 检查是否有快捷命令参数
//   2. 如果有快捷命令，先登录并初始化网络服务，然后执行快捷命令
//   3. 如果没有快捷命令，正常启动交互式模式
func main() {
	// 检查是否有快捷命令参数（在cobra处理之前）
	hasQuickCmd := hasQuickCommandInMain()
	
	if hasQuickCmd {
		// 有快捷命令，需要先初始化然后执行
		// 手动解析登录参数
		username, password, configPath, accountName := parseLoginArgsManually()
		
		// 创建应用程序实例，进行初始化
		var app *Application
		var err error
		if configPath != "" {
			app, err = NewApplicationWithLogin(username, password, configPath)
		} else {
			app, err = NewApplicationWithLogin(username, password)
		}
		if err != nil {
			log.Fatalf("应用程序初始化失败: %v", err)
		}
		
		// 设置账户管理器到命令处理器
		cmd.SetAccountManager(app.accountMgr)
		
		// 执行登录流程
		var loginPassword string
		if app.autoUsername != "" && app.autoPassword != "" {
			app.logger.Info("🔍 使用自动登录...")
			loginPassword, err = app.loginService.AutoLoginWithPassword(app.autoUsername, app.autoPassword)
			if err != nil {
				log.Fatalf("自动登录失败: %v", err)
			}
		} else if accountName != "" {
			// 使用指定账户登录，但仍需要密码
			app.logger.Info("🔍 使用指定账户登录...")
			// 获取所有账户
			accounts := app.accountMgr.GetAllAccounts()
			var targetAccount *account.Account
			for _, acc := range accounts {
				if acc.Username == accountName {
					targetAccount = acc
					break
				}
			}
			if targetAccount == nil {
				log.Fatalf("未找到指定的账户: %s", accountName)
			}
			// 提示输入密码
			fmt.Printf("请输入账户 '%s' 的密码: ", accountName)
			loginPassword, err = app.loginService.ReadPassword()
			if err != nil {
				log.Fatalf("读取密码失败: %v", err)
			}
			// 设置当前账户
			err = app.accountMgr.SetCurrentAccountWithPassword(accountName, loginPassword)
			if err != nil {
				log.Fatalf("账户登录失败: %v", err)
			}
			fmt.Printf("✅ 登录成功！欢迎回来，%s\n", accountName)
		} else {
			// 使用交互式登录
			app.logger.Info("🔍 进入交互式登录...")
			loginPassword, err = app.loginService.RequireLoginWithPassword()
			if err != nil {
				log.Fatalf("登录失败: %v", err)
			}
		}
		
		// 保存密码用于后续解密私钥
		app.currentPassword = loginPassword
		
		// 初始化网络服务
		app.logger.Info("🔄 正在初始化网络服务...")
		if err := app.initializeNetworkServices(); err != nil {
			log.Fatalf("网络服务初始化失败: %v", err)
		}
		app.logger.Info("✅ 网络服务初始化完成")
		
		// 将账户管理器注入到CLI命令中
		cmd.SetAccountManager(app.accountMgr)
		
		// 执行快捷命令
		app.executeQuickCommand()
	} else {
		// 没有快捷命令，需要为cobra子命令初始化账户管理器
		// 创建基础的账户管理器用于子命令
		accountMgr := account.NewManager()
		cmd.SetAccountManager(accountMgr)
		
		// 设置交互式模式的处理函数
		cmd.RootCmd.Run = func(cobraCmd *cobra.Command, args []string) {
			// 从cobra获取参数
			username, password, configPath := cmd.GetLoginParams()
			
			// 创建应用程序实例，进行初始化
			var app *Application
			var err error
			if configPath != "" {
				app, err = NewApplicationWithLogin(username, password, configPath)
			} else {
				app, err = NewApplicationWithLogin(username, password)
			}
			if err != nil {
				log.Fatalf("应用程序初始化失败: %v", err)
			}
			
			// 设置账户管理器到命令处理器
			cmd.SetAccountManager(app.accountMgr)
			
			// 启动应用程序主流程
			app.Start()
		}
		
		// 执行cobra命令
		cmd.Execute()
	}
}

// parseLoginArgsManually 手动解析登录参数
func parseLoginArgsManually() (username, password, configPath, accountName string) {
	args := os.Args[1:]
	
	for i := 0; i < len(args); i++ {
		arg := args[i]
		
		if arg == "--login" && i+2 < len(args) {
			// --login username password 格式
			username = args[i+1]
			password = args[i+2]
			i += 2
		} else if arg == "--username" && i+1 < len(args) {
			username = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--username=") {
			username = strings.TrimPrefix(arg, "--username=")
		} else if arg == "--password" && i+1 < len(args) {
			password = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--password=") {
			password = strings.TrimPrefix(arg, "--password=")
		} else if arg == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--config=") {
			configPath = strings.TrimPrefix(arg, "--config=")
		} else if arg == "--account" && i+1 < len(args) {
			accountName = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--account=") {
			accountName = strings.TrimPrefix(arg, "--account=")
		}
	}
	
	return username, password, configPath, accountName
}

// hasQuickCommandInMain 在main函数中检查是否有快捷命令参数
func hasQuickCommandInMain() bool {
	// 获取命令行参数，跳过程序名
	args := os.Args[1:]
	
	// 检查是否有--login参数
	for i := 0; i < len(args); i++ {
		if args[i] == "--login" {
			// 如果有--login参数，认为这是快捷命令
			return true
		}
	}
	
	// 检查是否是cobra子命令（如account, chat等）
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		// 第一个参数不是以--开头，说明是子命令，不是快捷命令
		return false
	}
	
	// 过滤掉登录相关的参数
	var filteredArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// 跳过登录相关参数
		if arg == "--username" || arg == "--password" || arg == "--config" || arg == "--log-level" || arg == "--account" {
			// 跳过参数名和参数值
			if i+1 < len(args) {
				i++ // 跳过参数值
			}
			continue
		}
		// 跳过以--开头的参数（如--username=value格式）
		if strings.HasPrefix(arg, "--username=") || strings.HasPrefix(arg, "--password=") || 
		   strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--log-level=") || strings.HasPrefix(arg, "--account=") {
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}
		
	// 如果有剩余参数，说明有快捷命令
	return len(filteredArgs) > 0
}
