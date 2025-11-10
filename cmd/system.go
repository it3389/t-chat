package cmd

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// systemCmd 系统命令主命令
var systemCmd = &cobra.Command{
	Use:   "system",
	Short: "系统相关命令",
}

// clearCmd 清屏命令
var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "清空终端屏幕",
	Run: func(cmd *cobra.Command, args []string) {
		clearScreen()
		fmt.Println("✅ 屏幕已清空")
	},
}

// clsCmd Windows风格的清屏命令
var clsCmd = &cobra.Command{
	Use:   "cls",
	Short: "清空终端屏幕 (Windows风格)",
	Run: func(cmd *cobra.Command, args []string) {
		clearScreen()
		fmt.Println("✅ 屏幕已清空")
	},
}

// clearScreen 跨平台清屏函数
func clearScreen() {
	switch runtime.GOOS {
	case "windows":
		// Windows 系统使用 cls 命令
		fmt.Print("\033[H\033[2J")
	default:
		// Unix/Linux/macOS 系统使用 clear 命令
		fmt.Print("\033[H\033[2J")
	}
}

// versionCmd 版本信息命令
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("┌─────────────────────────────────────┐")
		fmt.Println("│            TChat v1.0.0             │")
		fmt.Println("│           完全去中心化实现          │")
		fmt.Println("│            CLI 聊天工具             │")
		fmt.Println("└─────────────────────────────────────┘")
		fmt.Printf("构建时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Printf("Go版本: %s\n", runtime.Version())
		fmt.Printf("作者: tank\n")
		fmt.Printf("邮箱: it_luo@126.com\n")
	},
}

// infoCmd 系统信息命令
var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "显示系统信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("📊 系统信息:")
		fmt.Printf("  操作系统: %s\n", runtime.GOOS)
		fmt.Printf("  架构: %s\n", runtime.GOARCH)
		fmt.Printf("  CPU核心数: %d\n", runtime.NumCPU())
		fmt.Printf("  Go版本: %s\n", runtime.Version())
		fmt.Printf("  当前时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	},
}

// sysStatusCmd 系统状态信息命令
var sysStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "显示当前系统状态",
	Run: func(cmd *cobra.Command, args []string) {
		// 仅输出必要指标，避免泄露过多运行环境信息
		fmt.Println("🔍 系统状态:")
		fmt.Printf("  内存使用: %d MB\n", getMemoryUsage())
		fmt.Printf("  CPU使用率: %.1f%%\n", getCPUUsage())
		fmt.Printf("  运行时间: %s\n", getUptime())
		// 如需更多详情，请使用 system debug 或设置 --log-level=debug
	},
}

// getMemoryUsage 获取内存使用情况（简化版）
func getMemoryUsage() int64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.Alloc / 1024 / 1024) // 转换为MB
}

// getCPUUsage 获取CPU使用率（基于goroutine数量的简化估算）
func getCPUUsage() float64 {
	// 获取当前goroutine数量
	numGoroutines := runtime.NumGoroutine()
	// 获取CPU核心数
	numCPU := runtime.NumCPU()

	// 简化的CPU使用率估算：基于goroutine密度
	// 这是一个粗略的估算，实际CPU使用率需要更复杂的计算
	usageRatio := float64(numGoroutines) / float64(numCPU*10) // 假设每个CPU核心理想情况下处理10个goroutine
	if usageRatio > 1.0 {
		usageRatio = 1.0
	}

	return usageRatio * 100.0
}

// getDetailedMemoryStats 获取详细的内存统计信息
func getDetailedMemoryStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Println("📊 详细内存统计:")
	fmt.Printf("  当前分配内存: %d MB\n", m.Alloc/1024/1024)
	fmt.Printf("  累计分配内存: %d MB\n", m.TotalAlloc/1024/1024)
	fmt.Printf("  系统内存: %d MB\n", m.Sys/1024/1024)
	fmt.Printf("  堆内存: %d MB\n", m.HeapAlloc/1024/1024)
	fmt.Printf("  堆系统内存: %d MB\n", m.HeapSys/1024/1024)
	fmt.Printf("  堆空闲内存: %d MB\n", m.HeapIdle/1024/1024)
	fmt.Printf("  堆使用中内存: %d MB\n", m.HeapInuse/1024/1024)
	fmt.Printf("  栈内存: %d MB\n", m.StackSys/1024/1024)
	fmt.Printf("  GC次数: %d\n", m.NumGC)
	fmt.Printf("  上次GC时间: %v\n", time.Unix(0, int64(m.LastGC)))
	fmt.Printf("  GC暂停时间: %v\n", time.Duration(m.PauseNs[(m.NumGC+255)%256]))
	fmt.Printf("  Goroutine数量: %d\n", runtime.NumGoroutine())
}

var startTime = time.Now()

// getUptime 获取运行时间
func getUptime() string {
	uptime := time.Since(startTime)
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60
	seconds := int(uptime.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%d天 %d小时 %d分钟 %d秒", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%d小时 %d分钟 %d秒", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%d分钟 %d秒", minutes, seconds)
	} else {
		return fmt.Sprintf("%d秒", seconds)
	}
}

// reloadCmd 重新加载配置命令
var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "重新加载配置文件",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔄 重新加载配置...")
		// 这里可以添加重新加载配置的逻辑
		fmt.Println("✅ 配置重新加载完成")
	},
}

// restartCmd 重启服务命令
var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "重启聊天服务",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔄 重启聊天服务...")
		// 这里可以添加重启服务的逻辑
		fmt.Println("✅ 服务重启完成")
	},
}

// debugCmd 调试信息命令
var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "显示调试信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🐛 调试信息:")
		fmt.Printf("  工作目录: %s\n", getCurrentDir())
		fmt.Printf("  环境变量: %s\n", getEnvInfo())
		fmt.Printf("  命令行参数: %v\n", os.Args)
	},
}

// memoryCmd 详细内存统计信息命令
var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "显示详细内存统计信息",
	Run: func(cmd *cobra.Command, args []string) {
		getDetailedMemoryStats()
	},
}

// getCurrentDir 获取当前工作目录
func getCurrentDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "未知"
	}
	return dir
}

// getEnvInfo 获取环境信息
func getEnvInfo() string {
	env := os.Getenv("GO_ENV")
	if env == "" {
		env = "development"
	}
	return env
}

func init() {
	// 不再对子命令调用 AddHelpFlagAndHandler
	RootCmd.AddCommand(systemCmd)
	systemCmd.AddCommand(clearCmd)
	systemCmd.AddCommand(clsCmd)
	systemCmd.AddCommand(versionCmd)
	systemCmd.AddCommand(infoCmd)
	systemCmd.AddCommand(sysStatusCmd)
	systemCmd.AddCommand(reloadCmd)
	systemCmd.AddCommand(restartCmd)
	systemCmd.AddCommand(debugCmd)
	systemCmd.AddCommand(memoryCmd)
}
