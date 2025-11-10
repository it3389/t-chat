package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// 全局变量已在 shared.go 中声明

// statusCmd 状态与路由主命令
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "状态与路由相关命令",
}

// onlineCmd 查看在线好友
var onlineCmd = &cobra.Command{
	Use:   "online",
	Short: "查看当前在线好友",
	Run: func(cmd *cobra.Command, args []string) {
		if friendList == nil {
			fmt.Println("❌ 好友列表未初始化")
			return
		}

		friends := friendList.GetAllFriends()
		onlineCount := 0

		fmt.Println("👥 当前在线好友：")
		if len(friends) == 0 {
			fmt.Println("  - 暂无好友")
			return
		}

		for _, f := range friends {
			if f.IsOnline {
				fmt.Printf("  - %s (在线) - 最后活跃: %s\n", f.Username, f.LastSeen.Format("15:04:05"))
				onlineCount++
			} else {
				fmt.Printf("  - %s (离线) - 最后活跃: %s\n", f.Username, f.LastSeen.Format("15:04:05"))
			}
		}

		fmt.Printf("\n📊 统计: %d/%d 好友在线\n", onlineCount, len(friends))
	},
}

// routeCmd 查看消息路由路径
var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "查看消息路由路径",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		networkInfo := pineconeService.GetNetworkInfo()
		peers := networkInfo["peers"].([]map[string]interface{})

		fmt.Println("🛣️  消息路由路径：")
		if len(peers) == 0 {
			fmt.Println("  - 暂无连接的节点")
			fmt.Println("  - 路由状态: 离线")
			return
		}

		fmt.Println("  - 路由状态: 在线")
		fmt.Printf("  - 连接节点数: %d\n", len(peers))
		fmt.Println("  - 路由节点列表:")
		for i, peer := range peers {
			username := "<未知>"
			if pineconeService != nil {
				if pub, ok := peer["public_key"].(string); ok && pub != "" {
					if name, ok := pineconeService.GetUsernameByPubKey(pub); ok && name != "" {
						username = name
					}
				}
			}
			zone := peer["zone"]
			if zone == nil || zone == "" {
				zone = "<未知>"
			}
			fmt.Printf("    %d. 节点: %s\n", i+1, peer["public_key"])
			fmt.Printf("       用户名: %s\n", username)
			fmt.Printf("       类型: %s\n", peer["peer_type"])
			fmt.Printf("       区域: %s\n", zone)
			if remoteIP, ok := peer["remote_ip"].(string); ok && remoteIP != "" {
				fmt.Printf("       地址: %s:%v\n", remoteIP, peer["remote_port"])
			}
			fmt.Println()
		}

		// 显示路由统计信息
		fmt.Println("  - 路由统计:")
		fmt.Printf("    总节点数: %d\n", networkInfo["peer_count"])
		fmt.Printf("    在线节点: %d\n", len(peers))
		fmt.Printf("    节点ID: %s\n", networkInfo["node_id"])
		fmt.Printf("    监听地址: %s\n", networkInfo["listen_addr"])
		fmt.Printf("    连接状态: %v\n", networkInfo["connected"])
	},
}

var mappingCmd = &cobra.Command{
	Use:   "mapping",
	Short: "查看用户名与公钥映射缓存",
	Run: func(cmd *cobra.Command, args []string) {
		if pineconeService == nil {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		// 增强：显示映射表统计信息
		fmt.Printf("📊 映射表统计信息:\n")
		fmt.Printf("  UserHostMap 大小: %d\n", len(pineconeService.UserHostMap))
		fmt.Printf("  PeerHostMap 大小: %d\n", len(pineconeService.PeerHostMap))

		// 修正：展示前自动用本地好友列表补全映射
		if friendList != nil {
			fmt.Printf("🔄 正在用本地好友列表补全映射...\n")
			addedCount := 0
			for _, f := range friendList.GetAllFriends() {
				if f.Username != "" && f.PineconeAddr != "" {
					oldName, _ := pineconeService.GetUsernameByPubKey(f.PineconeAddr)
					if oldName == "" {
						pineconeService.UserHostMap[f.Username] = f.PineconeAddr
						pineconeService.PeerHostMap[f.PineconeAddr] = f.Username
						addedCount++
						fmt.Printf("  + 添加映射: %s <-> %s\n", f.Username, f.PineconeAddr)
					}
				}
			}
			if addedCount > 0 {
				fmt.Printf("✅ 从好友列表补全了 %d 个映射\n", addedCount)
			} else {
				fmt.Printf("ℹ️  好友列表映射已完整\n")
			}
		}

		fmt.Println("\n[用户名 => 公钥] 映射:")
		if len(pineconeService.UserHostMap) == 0 {
			fmt.Println("  (空)")
		} else {
			for user, pub := range pineconeService.UserHostMap {
				if user != "" && pub != "" {
					fmt.Printf("  %s => %s\n", user, pub)
				}
			}
		}

		fmt.Println("\n[公钥 => 用户名] 映射:")
		if len(pineconeService.PeerHostMap) == 0 {
			fmt.Println("  (空)")
		} else {
			for pub, user := range pineconeService.PeerHostMap {
				if pub != "" && user != "" {
					fmt.Printf("  %s => %s\n", pub, user)
				}
			}
		}

		// 增强：显示调试信息
		fmt.Printf("\n🔍 调试信息:\n")
		fmt.Printf("  Pinecone 服务状态: %v\n", pineconeService.IsConnected())
		fmt.Printf("  当前节点地址: %s\n", pineconeService.GetPineconeAddr())
		if friendList != nil {
			acc := friendList.GetCurrentAccount()
			if acc != nil {
				fmt.Printf("  当前用户名: %s\n", acc.Username)
			} else {
				fmt.Printf("  当前用户名: (未设置)\n")
			}
		}
	},
}

func init() {
	RootCmd.AddCommand(statusCmd)
	statusCmd.AddCommand(onlineCmd)
	statusCmd.AddCommand(routeCmd)
	statusCmd.AddCommand(mappingCmd)
}
