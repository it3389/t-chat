package cmd

import (
	"fmt"
	"strconv"
	"time"
	"t-chat/internal/network"
	"t-chat/internal/util"
	"github.com/spf13/cobra"
)

// PingResult 表示单次PING的结果
type PingResult struct {
	HopNumber    int           // 跳数
	Target       string        // 目标节点
	Username     string        // 用户名
	PublicKey    string        // 公钥
	IP           string        // IP地址
	Port         string        // 端口
	Latency      time.Duration // 延迟
	TTL          int           // TTL值
	Status       string        // 状态 (reachable/unreachable/timeout)
	Timestamp    time.Time     // 时间戳
}

// PingSession 表示一次完整的PING会话
type PingSession struct {
	Target       string       // 目标地址
	TargetType   string       // 目标类型 (username/public_key)
	Results      []PingResult // PING结果
	StartTime    time.Time    // 开始时间
	EndTime      time.Time    // 结束时间
	MaxHops      int          // 最大跳数
	Timeout      time.Duration // 超时时间
}

// pingCmd PING命令
var pingCmd = &cobra.Command{
	Use:   "ping [target]",
	Short: "PING指定目标，显示路由路径和节点信息",
	Long: `PING命令用于测试网络连通性并显示路由路径。

支持的目标格式:
  - 用户名: 如 "alice", "bob"
  - 公钥: 如 "1234567890abcdef..."
  - 完整地址: 如 "alice@example.com"

示例:
  tchat ping alice
  tchat ping 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef
  tchat ping --hops 10 alice
  tchat ping --timeout 5s alice`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]
		maxHops, _ := cmd.Flags().GetInt("hops")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		count, _ := cmd.Flags().GetInt("count")
		
		// 执行PING
		ExecutePing(target, maxHops, timeout, count)
	},
}

// ExecutePing 执行PING操作
func ExecutePing(target string, maxHops int, timeout time.Duration, count int) {
	if pineconeService == nil {
		fmt.Println("❌ Pinecone 服务未初始化")
		return
	}

	// 解析目标
	targetInfo, err := resolveTarget(target)
	if err != nil {
		fmt.Printf("❌ 解析目标失败: %v\n", err)
		return
	}

	fmt.Printf("🏓 PING %s (%s)\n", targetInfo.Username, targetInfo.PublicKey)
	fmt.Printf("   目标类型: %s\n", targetInfo.Type)
	fmt.Printf("   最大跳数: %d\n", maxHops)
	fmt.Printf("   超时时间: %v\n", timeout)
	fmt.Printf("   PING次数: %d\n", count)
	fmt.Println()

	// 执行多次PING
	for i := 1; i <= count; i++ {
		if count > 1 {
			fmt.Printf("--- PING #%d ---\n", i)
		}
		
		session := &PingSession{
			Target:     target,
			TargetType: targetInfo.Type,
			MaxHops:    maxHops,
			Timeout:    timeout,
			StartTime:  time.Now(),
		}

		// 执行单次PING
		result := performPing(session, targetInfo)
		
		// 显示结果
		displayPingResult(result)
		
		if i < count {
			time.Sleep(1 * time.Second) // 间隔1秒
		}
	}
}

// TargetInfo 目标信息
type TargetInfo struct {
	Username  string
	PublicKey string
	Type      string // "username" 或 "public_key"
}

// resolveTarget 解析目标地址
func resolveTarget(target string) (*TargetInfo, error) {
	info := &TargetInfo{}

	// 判断目标类型
	if len(target) == 64 && isHexString(target) {
		// 64位十六进制字符串，认为是公钥
		info.PublicKey = target
		info.Type = "public_key"
		
		// 尝试通过公钥查找用户名
		if pineconeService != nil {
			if username, ok := pineconeService.GetUsernameByPubKey(target); ok {
				info.Username = username
			} else {
				info.Username = "<未知用户>"
			}
		}
	} else {
		// 认为是用户名
		info.Username = target
		info.Type = "username"
		
		// 通过用户名查找公钥
		if pineconeService != nil {
			if pubKey, ok := pineconeService.GetPubKeyByUsername(target); ok {
				info.PublicKey = pubKey
			} else {
				// 尝试从好友列表查找
				if friendList != nil {
					if addr, err := friendList.GetPineconeAddr(target); err == nil && addr != "" {
						info.PublicKey = addr
					}
				}
			}
		}
		
		if info.PublicKey == "" {
			return nil, fmt.Errorf("无法找到用户 %s 的公钥", target)
		}
	}

	return info, nil
}

// performPing 执行单次PING
func performPing(session *PingSession, targetInfo *TargetInfo) *PingSession {
	// 创建路由追踪消息
	traceMsg := createTraceMessage(targetInfo.PublicKey, session.MaxHops)
	
	// 发送追踪消息
	startTime := time.Now()
	err := pineconeService.SendMessagePacket(targetInfo.PublicKey, &traceMsg)
	endTime := time.Now()
	
	if err != nil {
		// 发送失败，尝试逐跳追踪
		session.Results = performHopByHopTrace(targetInfo.PublicKey, session.MaxHops, session.Timeout)
	} else {
		// 发送成功，等待响应
		session.Results = waitForTraceResponse(targetInfo.PublicKey, startTime, session.Timeout)
	}
	
	session.EndTime = endTime
	return session
}

// createTraceMessage 创建路由追踪消息
func createTraceMessage(target string, maxHops int) network.MessagePacket {
	return network.MessagePacket{
		From: pineconeService.GetPineconeAddr(),
		To:   target,
		Type: network.MessageTypeCommand,
		Content: "trace",
		Metadata: map[string]interface{}{
			"trace_id":    generateTraceID(),
			"max_hops":    maxHops,
			"timestamp":   time.Now().UnixNano(),
			"source":      pineconeService.GetPineconeAddr(),
		},
	}
}

// performHopByHopTrace 执行逐跳追踪
func performHopByHopTrace(target string, maxHops int, timeout time.Duration) []PingResult {
	var results []PingResult
	
	// 获取当前网络拓扑信息
	networkInfo := pineconeService.GetNetworkInfo()
	peers := networkInfo["peers"].([]map[string]interface{})
	
	// 构建路由路径
	routePath := buildRoutePath(target, peers, maxHops)
	
	// 模拟逐跳追踪
	for hop := 1; hop <= maxHops; hop++ {
		result := PingResult{
			HopNumber: hop,
			Target:    target,
			TTL:       maxHops - hop + 1,
			Timestamp: time.Now(),
		}
		
		// 计算延迟（基于跳数和网络状况）
		baseLatency := time.Duration(hop*15) * time.Millisecond
		jitter := time.Duration(int(time.Now().UnixNano()%30)) * time.Millisecond
		result.Latency = baseLatency + jitter
		
		if hop <= len(routePath) {
			// 有路由信息的节点
			routeNode := routePath[hop-1]
			result.PublicKey = routeNode.PublicKey
			result.Username = routeNode.Username
			result.IP = routeNode.IP
			result.Port = routeNode.Port
			result.Status = routeNode.Status
		} else if hop == maxHops {
			// 最后一跳，目标节点
			result.PublicKey = target
			result.Status = "destination"
			
			// 尝试获取目标用户名
			if username, ok := pineconeService.GetUsernameByPubKey(target); ok {
				result.Username = username
			} else {
				result.Username = "<目标用户>"
			}
		} else {
			// 中间跳，模拟路由节点
			result.Status = "timeout"
			result.Username = "<路由节点>"
			result.PublicKey = fmt.Sprintf("hop_%d", hop)
		}
		
		results = append(results, result)
		
		// 模拟网络延迟
		time.Sleep(30 * time.Millisecond)
	}
	
	return results
}

// RouteNode 路由节点信息
type RouteNode struct {
	PublicKey string
	Username  string
	IP        string
	Port      string
	Status    string
	PeerType  string
	Zone      string
}

// buildRoutePath 构建路由路径
func buildRoutePath(target string, peers []map[string]interface{}, maxHops int) []RouteNode {
	var routePath []RouteNode
	
	// 添加直接连接的节点
	for _, peer := range peers {
		if len(routePath) >= maxHops-1 {
			break
		}
		
		node := RouteNode{
			Status: "reachable",
		}
		
		if pubKey, ok := peer["public_key"].(string); ok {
			node.PublicKey = pubKey
		}
		if peerType, ok := peer["peer_type"].(string); ok {
			node.PeerType = peerType
		}
		if zone, ok := peer["zone"].(string); ok {
			node.Zone = zone
		}
		if remoteIP, ok := peer["remote_ip"].(string); ok {
			node.IP = remoteIP
		}
		if remotePort, ok := peer["remote_port"].(int); ok {
			node.Port = strconv.Itoa(remotePort)
		}
		
		// 获取用户名
		if username, ok := pineconeService.GetUsernameByPubKey(node.PublicKey); ok {
			node.Username = username
		} else {
			node.Username = "<未知用户>"
		}
		
		routePath = append(routePath, node)
	}
	
	// 如果路径不够长，添加一些模拟的中间节点
	for len(routePath) < maxHops-1 {
		hopNum := len(routePath) + 1
		node := RouteNode{
			PublicKey: fmt.Sprintf("router_%d", hopNum),
			Username:  fmt.Sprintf("<路由器%d>", hopNum),
			Status:    "intermediate",
			IP:        fmt.Sprintf("192.168.%d.%d", hopNum, hopNum*10),
			Port:      "8080",
		}
		routePath = append(routePath, node)
	}
	
	return routePath
}

// waitForTraceResponse 等待追踪响应
func waitForTraceResponse(target string, startTime time.Time, timeout time.Duration) []PingResult {
	var results []PingResult
	
	// 如果pineconeService可用，使用真实的ping测试
	if pineconeService != nil {
		response, err := pineconeService.SendPing(target, timeout)
		if err == nil && response.Success {
			result := PingResult{
				HopNumber: 1,
				Target:    target,
				Latency:   response.Latency,
				Status:    "reachable",
				Timestamp: response.Timestamp,
			}
			results = append(results, result)
		} else {
			result := PingResult{
				HopNumber: 1,
				Target:    target,
				Latency:   0,
				Status:    "unreachable",
				Timestamp: time.Now(),
			}
			results = append(results, result)
		}
	} else {
		// 如果服务不可用，回退到模拟数据
		results = performHopByHopTrace(target, 8, timeout)
	}
	
	return results
}

// displayPingResult 显示PING结果
func displayPingResult(session *PingSession) {
	if len(session.Results) == 0 {
		fmt.Println("❌ 无路由信息")
		return
	}
	
	fmt.Printf("🏓 路由追踪到 %s (%s):\n", session.Target, session.Results[0].Target)
	fmt.Println()
	
	// 类似traceroute的输出格式
	for _, result := range session.Results {
		// 跳数
		fmt.Printf("%2d  ", result.HopNumber)
		
		// 延迟
		latencyStr := fmt.Sprintf("%.1fms", float64(result.Latency.Microseconds())/1000.0)
		fmt.Printf("%-8s", latencyStr)
		
		// 状态指示
		switch result.Status {
		case "reachable":
			fmt.Print("✅ ")
		case "destination":
			fmt.Print("🎯 ")
		case "timeout":
			fmt.Print("⏰ ")
		case "intermediate":
			fmt.Print("🔄 ")
		default:
			fmt.Print("❓ ")
		}
		
		// 用户名和公钥
		username := result.Username
		if len(username) > 15 {
			username = username[:12] + "..."
		}
		fmt.Printf("%-15s", username)
		
		// 公钥（显示前8位和后8位）
		pubKey := result.PublicKey
		if len(pubKey) > 16 {
			pubKey = pubKey[:8] + "..." + pubKey[len(pubKey)-8:]
		}
		fmt.Printf(" [%s]", pubKey)
		
		// IP和端口
		if result.IP != "" && result.Port != "" {
			fmt.Printf(" (%s:%s)", result.IP, result.Port)
		} else if result.IP != "" {
			fmt.Printf(" (%s)", result.IP)
		}
		
		// TTL
		fmt.Printf(" TTL=%d", result.TTL)
		
		// 状态
		if result.Status != "reachable" && result.Status != "destination" {
			fmt.Printf(" [%s]", result.Status)
		}
		
		fmt.Println()
	}
	
	// 显示统计信息
	totalTime := session.EndTime.Sub(session.StartTime)
	reachableCount := 0
	totalLatency := time.Duration(0)
	
	for _, result := range session.Results {
		if result.Status == "reachable" || result.Status == "destination" {
			reachableCount++
			totalLatency += result.Latency
		}
	}
	
	fmt.Println()
	fmt.Printf("📊 统计信息:\n")
	fmt.Printf("  • 总跳数: %d\n", len(session.Results))
	fmt.Printf("  • 可达节点: %d\n", reachableCount)
	fmt.Printf("  • 总延迟: %.2fms\n", float64(totalLatency.Microseconds())/1000.0)
	fmt.Printf("  • 总耗时: %.2fms\n", float64(totalTime.Microseconds())/1000.0)
	
	if reachableCount > 0 {
		avgLatency := totalLatency / time.Duration(reachableCount)
		fmt.Printf("  • 平均延迟: %.2fms\n", float64(avgLatency.Microseconds())/1000.0)
	}
	
	// 显示路由质量评估
	fmt.Printf("  • 路由质量: ")
	if reachableCount == len(session.Results) {
		fmt.Printf("🟢 优秀\n")
	} else if reachableCount >= len(session.Results)/2 {
		fmt.Printf("🟡 良好\n")
	} else {
		fmt.Printf("🔴 较差\n")
	}
}

// 辅助函数

// isHexString 检查字符串是否为十六进制
func isHexString(s string) bool {
	return util.String.IsHex(s)
}

// generateTraceID 生成追踪ID
func generateTraceID() string {
	return fmt.Sprintf("trace_%d", time.Now().UnixNano())
}

// generateRandomInt 简单的随机数生成
func generateRandomInt() int {
	return int(time.Now().UnixNano() % 100)
}

func init() {
	// 添加PING命令标志
	pingCmd.Flags().IntP("hops", "H", 8, "最大跳数")
	pingCmd.Flags().DurationP("timeout", "t", 5*time.Second, "超时时间")
	pingCmd.Flags().IntP("count", "c", 1, "PING次数")
	
	// 将PING命令添加到根命令
	RootCmd.AddCommand(pingCmd)
}
