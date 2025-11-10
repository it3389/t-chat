package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"t-chat/internal/friend"
	"t-chat/internal/network"
	"time"

	"github.com/spf13/cobra"
)

// friendCmd 好友管理主命令
var friendCmd = &cobra.Command{
	Use:   "friend",
	Short: "好友管理相关命令",
}

// addFriendCmd 添加好友
var addFriendCmd = &cobra.Command{
	Use:   "add",
	Short: "添加新好友",
	Run: func(cmd *cobra.Command, args []string) {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("请输入好友用户名: ")
		username, _ := reader.ReadString('\n')
		username = strings.TrimSpace(username)
		fmt.Print("请输入好友 Pinecone 地址: ")
		pinecone, _ := reader.ReadString('\n')
		pinecone = strings.TrimSpace(pinecone)
		fmt.Print("请输入好友公钥（可选，回车跳过）: ")
		pubkey, _ := reader.ReadString('\n')
		pubkey = strings.TrimSpace(pubkey)

		// 创建好友对象
		friend := &friend.Friend{
			Username:     username,
			PineconeAddr: pinecone,
			IsOnline:     false,
			LastSeen:     time.Now(),
			AddedAt:      time.Now(),
		}

		// 添加到好友列表
		if err := friendList.AddFriend(friend); err != nil {
			fmt.Printf("❌ 添加好友失败: %v\n", err)
			return
		}

		fmt.Printf("✅ 已添加好友 %s (%s)\n", username, pinecone)
	},
}

// deleteFriendCmd 删除好友
var deleteFriendCmd = &cobra.Command{
	Use:   "delete",
	Short: "删除指定好友",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			fmt.Println("请指定要删除的好友用户名")
			return
		}
		username := args[0]

		if err := friendList.RemoveFriend(username); err != nil {
			fmt.Printf("❌ 删除好友失败: %v\n", err)
			return
		}

		fmt.Printf("✅ 已删除好友 %s\n", username)
	},
}

// listFriendCmd 列出所有好友
var listFriendCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有好友",
	Run: func(cmd *cobra.Command, args []string) {
		friends := friendList.GetAllFriends()
		if len(friends) == 0 {
			fmt.Println("📝 好友列表为空")
			return
		}

		fmt.Println("👥 好友列表：")
		for _, f := range friends {
			status := "离线"
			if f.IsOnline {
				status = "在线"
			}
			fmt.Printf("- %s (%s) [%s]\n", f.Username, f.PineconeAddr, status)
		}
	},
}

var (
	// 支持同名不同公钥和路径聚合
	searchResults      = make(map[string]map[string]*network.FriendSearchResponse) // name -> pubkey -> resp
	searchResultsMutex sync.Mutex
)

// Go 的 var 块不能混合声明和初始化，单独声明 bool 变量
var searchResponseHandlerRegistered bool

func HandleFriendSearchResponse(resp *network.FriendSearchResponse) {
	if resp == nil || resp.Name == "" || resp.PublicKey == "" {
		fmt.Println("[WARN][handleFriendSearchResponse] 收到空或无效的响应对象")
	}
	// 自动补全映射：协查响应时补全用户名-公钥映射
	if pineconeService != nil && resp.Name != "" && resp.PublicKey != "" {
		pineconeService.UserHostMap[resp.Name] = resp.PublicKey
		pineconeService.PeerHostMap[resp.PublicKey] = resp.Name
	}
	searchResultsMutex.Lock()
	defer searchResultsMutex.Unlock()
	if _, ok := searchResults[resp.Name]; !ok {
		searchResults[resp.Name] = make(map[string]*network.FriendSearchResponse)
	}
	// 路径聚合：如同名同公钥多路径，保留最短路径
	existing, exists := searchResults[resp.Name][resp.PublicKey]
	if !exists || len(resp.Path) < len(existing.Path) {
		searchResults[resp.Name][resp.PublicKey] = resp
	}
}

// 适配器函数，将新的方法签名适配到旧的处理函数
func friendSearchResponseAdapter(username string, pubkey string, addr string) {
	resp := &network.FriendSearchResponse{
		Name:      username,
		PublicKey: pubkey,
		Path:      []string{addr},
	}
	HandleFriendSearchResponse(resp)
}

var searchCmd = &cobra.Command{
	Use:   "search [keyword]",
	Short: "Search friends by name or public key (local and network)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		keyword := args[0]
		fmt.Println("[本地模糊查找]")
		found := false
		for _, f := range friendList.GetAllFriends() {
			if strings.Contains(f.Username, keyword) || strings.Contains(f.PineconeAddr, keyword) {
				fmt.Printf("- %s (%s)\n", f.Username, f.PineconeAddr)
				found = true
			}
		}
		if !found {
			fmt.Println("无本地匹配，开始网络协查...")
		} else {
			fmt.Println("\n[网络协查中...]")
		}
		// 清空历史结果
		searchResultsMutex.Lock()
		searchResults = make(map[string]map[string]*network.FriendSearchResponse)
		searchResultsMutex.Unlock()
		// 注册响应处理（只注册一次）
		if !searchResponseHandlerRegistered {
			pineconeService.OnFriendSearchResponse(friendSearchResponseAdapter)
			searchResponseHandlerRegistered = true
		}
		// 构造并广播请求
		pineconeService.BroadcastFriendSearchRequest(keyword)
		// 等待响应聚合（sleep 轮询，最多2秒）
		waitSeconds := 2
		interval := 50 * time.Millisecond
		deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
		for time.Now().Before(deadline) {
			searchResultsMutex.Lock()
			if len(searchResults) > 0 {
				searchResultsMutex.Unlock()
				break
			}
			searchResultsMutex.Unlock()
			time.Sleep(interval)
		}
		searchResultsMutex.Lock()
		searchResultsMutex.Unlock()
		fmt.Println("[网络协查结果]")
		if len(searchResults) == 0 {
			fmt.Println("未在网络中找到匹配好友")
		} else {
			for name, pubkeyMap := range searchResults {
				for pubkey, resp := range pubkeyMap {
					fmt.Printf("- %s (%s) 路径: %v\n", name, pubkey, resp.Path)
				}
			}
		}
	},
}

func init() {

	RootCmd.AddCommand(friendCmd)
	friendCmd.AddCommand(addFriendCmd)
	friendCmd.AddCommand(deleteFriendCmd)
	friendCmd.AddCommand(listFriendCmd)
	friendCmd.AddCommand(searchCmd)
}
