package config

import (
	"fmt"
	"os"
	"unicode/utf8"
	"t-chat/internal/performance"
)

// Config 是全局配置结构体，包含日志、账户、网络等主要参数
// 可通过 LoadConfig 加载 JSON 配置文件
// 所有字段均带默认值
type Config struct {
	LogLevel   string `json:"log_level"`   // 日志级别（info/debug/warn/error）
	LogFile    string `json:"log_file"`    // 日志文件路径
	AccountDir string `json:"account_dir"` // 账户数据存储目录
	MessageDir string `json:"message_dir"` // 聊天记录存储目录
	FileDir    string `json:"file_dir"`    // 文件传输存储目录
	MDNSPort   int    `json:"mdns_port"`   // mDNS 服务端口
	// 心跳检测机制已禁用
	PineconeAPIKey string   `json:"pinecone_api_key"`
	PineconeIndex  string   `json:"pinecone_index"`
	PineconeRegion string   `json:"pinecone_region"`
	PineconeListen string   `json:"pinecone_listen"` // Pinecone监听端口，如":7777"
	PineconePeers  []string `json:"pinecone_peers"`  // 静态peer地址数组
}

// DefaultConfig 返回带默认值的配置
func DefaultConfig() *Config {
	return &Config{
		LogLevel:   "info",
		LogFile:    "tchat.log",
		AccountDir: "accounts",
		MessageDir: "messages",
		FileDir:    "files",
		MDNSPort:   5353,
		// 心跳检测机制已禁用
		PineconeAPIKey: "your-pinecone-api-key-here",
		PineconeIndex:  "tchat-messages",
		PineconeRegion: "us-east-1",
		PineconeListen: ":7777",
		PineconePeers:  []string{},
	}
}

// LoadConfig 从JSON文件加载配置
func LoadConfig(configPath ...string) (*Config, error) {
	configFile := "config.json"
	if len(configPath) > 0 && configPath[0] != "" {
		configFile = configPath[0]
	}

	// 如果配置文件不存在，创建默认配置
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultConfig := DefaultConfig()

		if err := SaveConfig(defaultConfig); err != nil {
			return nil, fmt.Errorf("创建默认配置文件失败: %v", err)
		}

		fmt.Println("📝 已创建默认配置文件config.json")
		fmt.Println("⚠️  请编辑config.json文件，设置Pinecone监听端口和静态peer")
		return defaultConfig, nil
	}

	// 读取配置文件
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var config Config
	jsonOptimizer := performance.GetJSONOptimizer()
	if err := jsonOptimizer.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 检查并修复所有非法字符，确保为合法 UTF-8 编码
	config.LogLevel = sanitizeString(config.LogLevel)
	config.LogFile = sanitizeString(config.LogFile)
	config.AccountDir = sanitizeString(config.AccountDir)
	config.MessageDir = sanitizeString(config.MessageDir)
	config.FileDir = sanitizeString(config.FileDir)
	config.PineconeListen = sanitizeString(config.PineconeListen)

	return &config, nil
}

// SaveConfig 保存配置（已优化）
func SaveConfig(config *Config) error {
	jsonOptimizer := performance.GetJSONOptimizer()
	data, err := jsonOptimizer.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %v", err)
	}

	return os.WriteFile("config.json", data, 0644)
}

// sanitizeString 检查并修复非法字符，确保为合法 UTF-8 编码
func sanitizeString(s string) string {
	if s == "" {
		return s
	}

	// 检查是否为有效的 UTF-8 编码
	if !utf8.ValidString(s) {
		// 如果不是有效的 UTF-8，尝试修复
		valid := make([]rune, 0, len(s))
		for _, r := range s {
			if r == utf8.RuneError {
				// 替换无效字符为空格
				valid = append(valid, ' ')
			} else {
				valid = append(valid, r)
			}
		}
		return string(valid)
	}

	return s
}
