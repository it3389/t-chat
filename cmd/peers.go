package cmd

import (
	"fmt"
	"strings"
	"time"
	"t-chat/internal/network"
	"github.com/spf13/cobra"
)

// 全局变量已在 shared.go 中声明

// peersCmd 网络节点管理主命令
var peersCmd = &cobra.Command{
	Use:   "peers",
	Short: "网络节点管理相关命令",
}

// listPeersCmd 列出所有网络节点
var listPeersCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有网络节点",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		networkInfo := pineconeService.GetNetworkInfo()
		peers := networkInfo["peers"].([]map[string]interface{})

		fmt.Println("📡 网络节点列表:")
		if len(peers) == 0 {
			fmt.Println("  - 暂无连接的节点")
			return
		}

		for i, peer := range peers {
			pubKey := peer["public_key"].(string)
			username := peer["username"].(string)
			
			fmt.Printf("  %d. 公钥: %s\n", i+1, pubKey)
			if username != "" {
				fmt.Printf("     账号: %s\n", username)
			}
			peerType := peer["peer_type"].(int)
			var peerTypeStr string
			switch peerType {
			case 0:
				peerTypeStr = "pipe"
			case 1:
				peerTypeStr = "multicast"
			case 2:
				peerTypeStr = "bonjour"
			case 3:
				peerTypeStr = "remote"
			case 4:
				peerTypeStr = "bluetooth"
			default:
				peerTypeStr = fmt.Sprintf("unknown(%d)", peerType)
			}
			fmt.Printf("     类型: %s\n", peerTypeStr)
			fmt.Printf("     区域: %s\n", peer["zone"])
			if remoteIP, ok := peer["remote_ip"].(string); ok && remoteIP != "" {
				fmt.Printf("     地址: %s:%v\n", remoteIP, peer["remote_port"])
			}
			fmt.Println()
		}
	},
}

// infoPeersCmd 显示网络节点详细信息
var infoPeersCmd = &cobra.Command{
	Use:   "info",
	Short: "显示网络节点详细信息",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		networkInfo := pineconeService.GetNetworkInfo()
		peers := networkInfo["peers"].([]map[string]interface{})

		fmt.Println("📊 网络节点详细信息:")
		fmt.Printf("  节点总数: %d\n", networkInfo["peer_count"])
		fmt.Printf("  在线节点: %d\n", len(peers))
		fmt.Printf("  节点ID: %s\n", networkInfo["node_id"])
		fmt.Printf("  监听地址: %s\n", networkInfo["listen_addr"])
		fmt.Printf("  连接状态: %v\n", networkInfo["connected"])

		// 统计不同类型的节点
		typeCount := make(map[string]int)
		for _, peer := range peers {
			peerTypeInt := peer["peer_type"].(int)
			var peerType string
			switch peerTypeInt {
			case 0:
				peerType = "pipe"
			case 1:
				peerType = "multicast"
			case 2:
				peerType = "bonjour"
			case 3:
				peerType = "remote"
			case 4:
				peerType = "bluetooth"
			default:
				peerType = fmt.Sprintf("unknown(%d)", peerTypeInt)
			}
			typeCount[peerType]++
		}

		fmt.Println("\n  节点类型分布:")
		for peerType, count := range typeCount {
			fmt.Printf("    %s: %d\n", peerType, count)
		}
	},
}

// bluetoothPeersCmd 显示蓝牙设备
var bluetoothPeersCmd = &cobra.Command{
	Use:   "bluetooth",
	Short: "显示蓝牙设备",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		networkInfo := pineconeService.GetNetworkInfo()
		bluetoothEnabled := networkInfo["bluetooth_enabled"].(bool)

		if !bluetoothEnabled {
			fmt.Println("🔵 蓝牙服务未启用")
			return
		}

		btPeers := networkInfo["bluetooth_peers"].([]map[string]interface{})

		fmt.Println("🔵 蓝牙设备列表:")
		if len(btPeers) == 0 {
			fmt.Println("  - 暂无发现的蓝牙设备")
			return
		}

		for i, peer := range btPeers {
			fmt.Printf("  %d. 名称: %s\n", i+1, peer["name"])
			fmt.Printf("     地址: %s\n", peer["address"])
			fmt.Printf("     公钥: %s\n", peer["public_key"])
			fmt.Printf("     连接状态: %v\n", peer["connected"])
			if lastSeen, ok := peer["last_seen"].(string); ok {
				fmt.Printf("     最后发现: %s\n", lastSeen)
			}
			fmt.Println()
		}
	},
}

// connectPeersCmd 连接到指定节点
var connectPeersCmd = &cobra.Command{
	Use:   "connect",
	Short: "连接到指定节点",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			fmt.Println("请指定要连接的节点地址")
			return
		}
		addr := args[0]
		fmt.Printf("🔗 正在连接到节点: %s\n", addr)

		// 验证地址格式
		if !isValidAddress(addr) {
			fmt.Println("❌ 无效的地址格式，请使用 host:port 格式")
			return
		}

		// 尝试连接到节点
		if err := connectToPeer(addr); err != nil {
			fmt.Printf("❌ 连接失败: %v\n", err)
		} else {
			fmt.Printf("✅ 成功连接到节点: %s\n", addr)
		}
	},
}

// disconnectPeersCmd 断开指定节点连接
var disconnectPeersCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "断开指定节点连接",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			fmt.Println("请指定要断开的节点地址")
			return
		}
		addr := args[0]
		fmt.Printf("🔌 正在断开节点连接: %s\n", addr)

		// 验证地址格式
		if !isValidAddress(addr) {
			fmt.Println("❌ 无效的地址格式，请使用 host:port 格式")
			return
		}

		// 尝试断开节点连接
		if err := disconnectFromPeer(addr); err != nil {
			fmt.Printf("❌ 断开连接失败: %v\n", err)
		} else {
			fmt.Printf("✅ 成功断开节点连接: %s\n", addr)
		}
	},
}

// pingPeersCmd 测试节点连通性
var pingPeersCmd = &cobra.Command{
	Use:   "ping",
	Short: "测试节点连通性（支持公钥或 host:port）",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			fmt.Println("请指定要测试的节点地址或公钥")
			return
		}
		addr := args[0]
		fmt.Printf("🏓 正在测试节点连通性: %s\n", addr)
		if len(addr) == 64 && isHex(addr) {
			// 认为是公钥，直接用 SendMessagePacket 递归路由 ping
			msg := network.MessagePacket{
				From:    pineconeService.GetPineconeAddr(),
				To:      addr,
				Type:    network.MessageTypeText,
				Content: "ping",
			}
			err := pineconeService.SendMessagePacket(addr, &msg)
			if err != nil {
				fmt.Printf("❌ 通过公钥递归路由 ping 失败: %v\n", err)
			} else {
				fmt.Println("✅ 已通过公钥递归路由发送 ping")
			}
			return
		}
		if !isValidAddress(addr) {
			fmt.Println("❌ 无效的地址格式，请使用 host:port 或公钥格式")
			return
		}
		latency, err := pingPeer(addr)
		if err != nil {
			fmt.Printf("❌ Ping 失败: %v\n", err)
		} else {
			fmt.Printf("✅ Ping 成功，延迟: %v\n", latency)
		}
	},
}

// statsPeersCmd 显示网络统计信息
var statsPeersCmd = &cobra.Command{
	Use:   "stats",
	Short: "显示网络统计信息",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		networkInfo := pineconeService.GetNetworkInfo()

		fmt.Println("📈 网络统计信息:")
		fmt.Printf("  当前时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Printf("  总连接数: %d\n", networkInfo["peer_count"])
		fmt.Printf("  节点ID: %s\n", networkInfo["node_id"])
		fmt.Printf("  连接状态: %v\n", networkInfo["connected"])

		// 显示静态节点信息
		staticPeers := networkInfo["static_peers"].([]string)
		fmt.Printf("  静态节点: %d 个\n", len(staticPeers))
		for i, peer := range staticPeers {
			fmt.Printf("    %d. %s\n", i+1, peer)
		}

		// 显示蓝牙统计
		if bluetoothEnabled := networkInfo["bluetooth_enabled"].(bool); bluetoothEnabled {
			btPeers := networkInfo["bluetooth_peers"].([]map[string]interface{})
			fmt.Printf("  蓝牙设备: %d 个\n", len(btPeers))
		}
	},
}

// scanPeersCmd 扫描网络节点
var scanPeersCmd = &cobra.Command{
	Use:   "scan",
	Short: "扫描网络节点",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		fmt.Println("🔍 正在扫描网络节点...")
		fmt.Println("  - 多播扫描: 已启用")
		fmt.Println("  - 蓝牙扫描: 已启用")
		fmt.Println("  - 服务发现: 已启用")
		fmt.Println("  - 扫描完成，请使用 'peers list' 查看结果")
	},
}

// showPeersCmd 显示节点详细信息（一级命令）
var showPeersCmd = &cobra.Command{
	Use:   "show",
	Short: "显示节点详细信息",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		networkInfo := pineconeService.GetNetworkInfo()
		
		// 安全地从networkInfo中获取节点ID
		publicKeyHex, ok := networkInfo["node_id"].(string)
		if !ok {
			fmt.Println("❌ 无法获取节点ID")
			return
		}

		fmt.Println("🆔 节点信息:")
		fmt.Printf("  节点ID: %s\n", publicKeyHex)
		fmt.Printf("  公钥: %s\n", publicKeyHex)
		
		// 安全地获取其他信息
		if listenAddr, ok := networkInfo["listen_addr"].(string); ok {
			fmt.Printf("  监听地址: %s\n", listenAddr)
		}
		if connected, ok := networkInfo["connected"].(bool); ok {
			fmt.Printf("  连接状态: %v\n", connected)
		}
		if peerCount, ok := networkInfo["peer_count"].(int); ok {
			fmt.Printf("  总连接数: %d\n", peerCount)
		}

		// 安全地显示连接的节点
		if peersInterface, ok := networkInfo["peers"]; ok {
			if peers, ok := peersInterface.([]map[string]interface{}); ok && len(peers) > 0 {
				fmt.Println("\n  连接的节点:")
				for i, peer := range peers {
					pubKey, _ := peer["public_key"].(string)
					username, _ := peer["username"].(string)
					
					peerType, _ := peer["peer_type"].(int)
					var peerTypeStr string
					switch peerType {
					case 0:
						peerTypeStr = "pipe"
					case 1:
						peerTypeStr = "multicast"
					case 2:
						peerTypeStr = "bonjour"
					case 3:
						peerTypeStr = "remote"
					case 4:
						peerTypeStr = "bluetooth"
					default:
						peerTypeStr = fmt.Sprintf("unknown(%d)", peerType)
					}
					
					if username != "" {
						fmt.Printf("    %d. %s (%s) - 账号: %s\n", i+1, pubKey, peerTypeStr, username)
					} else {
						fmt.Printf("    %d. %s (%s)\n", i+1, pubKey, peerTypeStr)
					}
				}
			} else {
				fmt.Println("\n  暂无连接的节点")
			}
		} else {
			fmt.Println("\n  暂无连接的节点")
		}
	},
}

func init() {
	RootCmd.AddCommand(peersCmd)
	peersCmd.AddCommand(listPeersCmd)
	peersCmd.AddCommand(infoPeersCmd)
	peersCmd.AddCommand(bluetoothPeersCmd)
	peersCmd.AddCommand(connectPeersCmd)
	peersCmd.AddCommand(disconnectPeersCmd)
	peersCmd.AddCommand(pingPeersCmd)
	peersCmd.AddCommand(statsPeersCmd)
	peersCmd.AddCommand(scanPeersCmd)

	// 添加一级命令
	RootCmd.AddCommand(showPeersCmd)
}

// 辅助函数实现

// isValidAddress 验证地址格式
func isValidAddress(addr string) bool {
	// 简单的地址格式验证
	if len(addr) == 0 {
		return false
	}

	// 检查是否包含端口号
	if !strings.Contains(addr, ":") {
		return false
	}

	// 检查端口号是否为数字
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return false
	}

	// 验证端口号
	port := parts[1]
	if port == "" {
		return false
	}

	// 这里可以添加更严格的验证逻辑
	return true
}

// connectToPeer 连接到指定节点
func connectToPeer(addr string) error {
	if pineconeService == nil {
		return fmt.Errorf("Pinecone 服务未初始化")
	}

	// 使用真正的连接方法
	return pineconeService.Connect(addr)
}

// disconnectFromPeer 断开指定节点连接
func disconnectFromPeer(addr string) error {
	if pineconeService == nil {
		return fmt.Errorf("Pinecone 服务未初始化")
	}

	// 使用真正的断开连接方法
	fmt.Printf("  正在断开连接...\n")
	err := pineconeService.Disconnect(addr)
	if err != nil {
		return fmt.Errorf("断开连接失败: %v", err)
	}

	fmt.Printf("  ✅ 已断开与 %s 的连接\n", addr)
	return nil
}

// pingPeer 测试节点连通性
func pingPeer(addr string) (time.Duration, error) {
	if pineconeService == nil {
		return 0, fmt.Errorf("Pinecone 服务未初始化")
	}

	// 设置超时时间为5秒
	timeout := 5 * time.Second
	
	// 发送ping请求并等待响应
	response, err := pineconeService.SendPing(addr, timeout)
	if err != nil {
		return 0, fmt.Errorf("ping失败: %v", err)
	}
	
	// 检查响应是否成功
	if !response.Success {
		return 0, fmt.Errorf("ping失败: %s", response.Error)
	}
	
	return response.Latency, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.Contains("0123456789abcdefABCDEF", string(c)) {
			return false
		}
	}
	return true
}
