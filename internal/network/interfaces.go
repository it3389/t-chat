package network

import (
	"context"
	"net"
	"time"
	
	"t-chat/internal/pinecone/router"
)

// NetworkServiceInterface 网络服务统一接口
type NetworkServiceInterface interface {
	Start(ctx context.Context) error
	Stop() error
	IsRunning() bool
	GetConfig() *NetworkConfig
	SetConfig(config *NetworkConfig) error
}

// MessageHandlerInterface 消息处理器接口
type MessageHandlerInterface interface {
	HandleMessage(msg *Message) error
	GetMessageType() string
	GetPriority() int
	CanHandle(msg *Message) bool
	Initialize() error
	Cleanup() error
}

// MessageDispatcherInterface 消息分发器接口
type MessageDispatcherInterface interface {
	RegisterHandler(handler MessageHandlerInterface) error
	UnregisterHandler(messageType string) error
	DispatchMessage(msg *Message) error
	GetHandlers() map[string]MessageHandlerInterface
	SetDefaultHandler(handler MessageHandlerInterface)
	GetHandlerCount() int
}

// Connection 网络连接接口
type Connection interface {
	Read(b []byte) (n int, err error)
	Write(b []byte) (n int, err error)
	Close() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	GetPeerID() string
	IsClosed() bool
}

// Listener 网络监听器接口
type Listener interface {
	Accept() (net.Conn, error)
	Close() error
	Addr() net.Addr
	IsClosed() bool
}

// ConnectionManagerInterface 连接管理器接口
type ConnectionManagerInterface interface {
	CreateConnection(peerID string, address string) (Connection, error)
	GetConnection(peerID string) (Connection, bool)
	CloseConnection(peerID string) error
	ListConnections() []string
	CreateListener(address string) (Listener, error)
	GetListener(address string) (Listener, bool)
	CloseListener(address string) error
	GetConnectionCount() int
	Cleanup() error
}

// ServiceManagerInterface 服务管理器接口
type ServiceManagerInterface interface {
	NetworkServiceInterface
	RegisterService(name string, service NetworkServiceInterface) error
	UnregisterService(name string) error
	GetService(name string) (NetworkServiceInterface, bool)
	GetAllServices() map[string]NetworkServiceInterface
	StartService(name string) error
	StopService(name string) error
}

// HeartbeatServiceInterface removed - heartbeat mechanism has been disabled

// MDNSServiceInterface mDNS服务接口
type MDNSServiceInterface interface {
	NetworkServiceInterface
	StartMDNS(username, nkn, pubkey string, port int) error
	StopMDNS() error
	DiscoverPeers(timeout time.Duration) ([]PeerInfo, error)
	SetDiscoveryCallback(callback func(peer PeerInfo))
	StartDiscovery() error
	CleanupExpiredPeers()
	ForceCleanupPeer(peerID string)
}

// RouteDiscoveryServiceInterface 路由发现服务接口
type RouteDiscoveryServiceInterface interface {
	Start() error
	Stop() error
	GetRouteFailures() map[string]*RouteFailureInfo
	GetStats() map[string]interface{}
	DiscoverRoute(targetAddr string) error
}

// RouteRedundancyServiceInterface 路由冗余服务接口
type RouteRedundancyServiceInterface interface {
	Start() error
	Stop() error
	GetStats() map[string]interface{}
	GetBestRoute(destination string) *RouteInfo
	GetNextRoute(destination string) *RouteInfo
	UpdateRoutes() error
	EnableLoadBalancing(destination string)
	DisableLoadBalancing(destination string)
	GetRouteStats() map[string]interface{}
	GetMultiPathRoutes() map[string]*MultiPathRoute
}

// PineconeServiceInterface Pinecone服务接口
type PineconeServiceInterface interface {
	NetworkServiceInterface
	GetPineconeAddr() string
	GetNetworkInfo() map[string]interface{}
	SendMessagePacket(toAddr string, packet *MessagePacket) error
	GetAllPeerAddrs() []string
	GetStartTime() time.Time
	FriendList() FriendListLike
	GetUsernameByPubKey(pubkey string) (string, bool)
	GetPubKeyByUsername(username string) (string, bool)
	// 节点标识相关方法
	GetNodeID() string
	GetPublicKeyHex() string
	// 消息发送方法
	SendMessage(peerID string, message *Message) error
	// 路由发现相关方法
	GetRouteDiscoveryService() RouteDiscoveryServiceInterface
	GetMDNSService() MDNSServiceInterface
	GetRouter() *router.Router
	// 连接管理方法
	ConnectToPeer(peerAddr string) error
	TriggerRouteRediscovery(targetAddr string)
	// 动态缓冲区管理方法
	GetBufferStats() map[string]interface{}
	GetBufferManagerInfo() map[string]interface{}
}

// Logger 日志接口
type Logger interface {
	Printf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// NetworkConfig 网络配置
type NetworkConfig struct {
	// 基础网络配置
	Port              int           `json:"port"`
	ListenPort        int           `json:"listen_port"`
	MaxConnections    int           `json:"max_connections"`
	ConnectionTimeout int           `json:"connection_timeout"` // 秒
	ReadTimeout       time.Duration `json:"read_timeout"`
	WriteTimeout      time.Duration `json:"write_timeout"`
	
	// Pinecone配置
	PineconeListen    string        `json:"pinecone_listen"`    // Pinecone监听端口，如":7777"
	PineconePeers     []string      `json:"pinecone_peers"`     // Pinecone对等节点列表
	
	// 心跳检测机制已禁用
	
	// mDNS配置
	ServiceName       string        `json:"service_name"`
	MDNSPort          int           `json:"mdns_port"`
	MDNSServiceName   string        `json:"mdns_service_name"`
	MDNSDiscoveryInterval time.Duration `json:"mdns_discovery_interval"`
	
	// 消息配置
	MaxMessageSize   int           `json:"max_message_size"`
	MessageQueueSize int           `json:"message_queue_size"`
	RetryAttempts    int           `json:"retry_attempts"`
	RetryInterval    time.Duration `json:"retry_interval"`
}

// DefaultNetworkConfig 返回默认网络配置
func DefaultNetworkConfig() *NetworkConfig {
	return &NetworkConfig{
			Port:              8080,
			ListenPort:        8080,
			MaxConnections:    100,
			ConnectionTimeout: 30, // 秒
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			
			PineconeListen:    ":7777", // 默认Pinecone监听端口
		PineconePeers:     []string{}, // 默认无对等节点
		
		// 心跳检测机制已禁用
		
		ServiceName:       "tchat",
		MDNSPort:          5353,
		MDNSServiceName:   "_tchat._tcp",
		// 优化：增加MDNS发现间隔从30秒到120秒，减少网络扫描频率
		MDNSDiscoveryInterval: 120 * time.Second,
		
		MaxMessageSize:   1024 * 1024, // 1MB
		MessageQueueSize: 1000,
		RetryAttempts:    3,
		RetryInterval:    5 * time.Second,
	}
}

// PeerInfo 对等节点信息
type PeerInfo struct {
	ID        string            `json:"id"`
	Username  string            `json:"username"`
	PublicKey string            `json:"public_key"`
	Address   string            `json:"address"`
	Port      int               `json:"port"`
	IsOnline  bool              `json:"is_online"`
	LastSeen  time.Time         `json:"last_seen"`
	RTT       time.Duration     `json:"rtt"`
	Metadata  map[string]string `json:"metadata"`
}

// MessagePacket 消息包结构
type MessagePacket struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Content   string                 `json:"content"`
	Data      []byte                 `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Priority  int                    `json:"priority"`
	ReplyTo   string                 `json:"reply_to,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Message 消息结构
type Message struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Content    string                 `json:"content"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	ReplyTo    string                 `json:"reply_to,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	// 文件传输相关字段
	FileID     string                 `json:"file_id,omitempty"`
	ChunkIndex int                    `json:"chunk_index,omitempty"`
	Priority   int                    `json:"priority,omitempty"`
}

// 消息类型常量
const (
	MessageTypeText              = "text"
	MessageTypeImage             = "image"
	MessageTypeFile              = "file"
	MessageTypeVoice             = "voice"
	MessageTypeLocation          = "location"
	MessageTypeCommand           = "command"
	MessageTypeSystem            = "system"
// MessageTypeHeartbeat and MessageTypeHeartbeatResponse removed - heartbeat mechanism disabled
	MessageTypeTrace             = "trace"
	MessageTypeAck               = "ack"
	MessageTypeNack              = "nack"
)

// 消息优先级常量（用于消息传输优先级）
const (
	MessagePriorityLow    = 1
	MessagePriorityNormal = 5
	MessagePriorityHigh   = 10
	MessagePriorityUrgent = 15
)

// 网络错误类型
type NetworkError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *NetworkError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// BluetoothPeerInfo 蓝牙设备信息
type BluetoothPeerInfo struct {
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	PublicKey string    `json:"public_key"`
	Connected bool      `json:"connected"`
	LastSeen  time.Time `json:"last_seen"`
}

// BluetoothServiceInterface 蓝牙服务接口
type BluetoothServiceInterface interface {
	Start() error
	Stop() error
	IsStarted() bool
	GetDiscoveredPeers() []interface{} // 返回蓝牙设备列表
}



// 常见网络错误代码
const (
	ErrCodeConnectionFailed = 1001
	ErrCodeTimeout         = 1002
	ErrCodeInvalidMessage  = 1003
	ErrCodeHandlerNotFound = 1004
	ErrCodeServiceNotFound = 1005
)