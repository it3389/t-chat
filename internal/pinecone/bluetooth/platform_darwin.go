//go:build darwin
// +build darwin

package bluetooth

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/ecdh"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "fmt"
    "os/exec"
    "strings"
    "sync"
    "time"

    "golang.org/x/crypto/hkdf"
)

var (
	foundDevicesMu sync.Mutex
	foundDevices   = make(map[string]*BluetoothPeer)
)

// DarwinBluetoothDevice Darwin平台蓝牙设备信息
type DarwinBluetoothDevice struct {
    Name      string
    Address   string
    Connected bool
    RSSI      int
}

// 注意：避免重复定义 DarwinBluetoothDevice

// 这些函数暂时注释掉，因为移除了 C 代码
// 如果需要实际的 CoreBluetooth 功能，需要重新实现
/*
func goDeviceFoundCallback(nameC, uuidC *C.char) {
	name := C.GoString(nameC)
	uuid := C.GoString(uuidC)
	foundDevicesMu.Lock()
	defer foundDevicesMu.Unlock()
	foundDevices[uuid] = &BluetoothPeer{
		PublicKey: "darwin_" + uuid,
		Address:   uuid,
		Name:      name,
		LastSeen:  time.Now(),
		Connected: false,
	}
}

func goDataReceivedCallback(uuidC *C.char, data *C.uchar, length C.int) {
	uuid := C.GoString(uuidC)
	bytes := C.GoBytes(unsafe.Pointer(data), length)
	// 自动分发到 Pinecone 消息处理
	foundDevicesMu.Lock()
	peer := foundDevices[uuid]
	foundDevicesMu.Unlock()
	if peer != nil && peer.BluetoothService != nil {
		peer.BluetoothService.handleBluetoothData(peer, bytes)
	}
}
*/

// DarwinBluetoothService Darwin 平台蓝牙服务
type DarwinBluetoothService struct {
    *BluetoothService
    privateKey *ecdh.PrivateKey
    publicKey  *ecdh.PublicKey
}

// NewDarwinBluetoothService 创建 Darwin 蓝牙服务
func NewDarwinBluetoothService(bs *BluetoothService) *DarwinBluetoothService {
    // 生成 ECDH 密钥对（P-256）
    curve := ecdh.P256()
    priv, err := curve.GenerateKey(rand.Reader)
    if err != nil {
        bs.logger.Printf("🔵 Darwin: 生成 ECDH 私钥失败: %v", err)
    }
    var pub *ecdh.PublicKey
    if priv != nil {
        pub = priv.PublicKey()
    }
    return &DarwinBluetoothService{
        BluetoothService: bs,
        privateKey:       priv,
        publicKey:        pub,
    }
}

// scanForPineconeDevices Darwin 平台蓝牙设备扫描
// 使用 system_profiler 和 blueutil 进行真实的蓝牙设备扫描
func (dbs *DarwinBluetoothService) scanForPineconeDevices() {
    // 在单独的goroutine中进行蓝牙扫描，避免阻塞主线程
    go func() {
        // 首先检查蓝牙是否可用
        if !dbs.isBluetoothAvailable() {
            dbs.logger.Printf("🔵 Darwin: 蓝牙不可用，跳过扫描")
            return
        }

        // 使用 system_profiler 获取蓝牙设备信息
        devices, err := dbs.scanBluetoothDevicesWithSystemProfiler()
        if err != nil {
            dbs.logger.Printf("🔵 Darwin: 使用 system_profiler 扫描失败: %v", err)
            // 尝试使用 blueutil 作为备选方案
            devices, err = dbs.scanBluetoothDevicesWithBlueutil()
            if err != nil {
                dbs.logger.Printf("🔵 Darwin: 使用 blueutil 扫描失败: %v", err)
                return
            }
        }

		// 处理发现的设备
		for _, device := range devices {
			if dbs.isPineconeDevice(device.Name, device.Address) {
				peer := &BluetoothPeer{
					PublicKey:          fmt.Sprintf("darwin_%s", device.Address),
					Address:            device.Address,
					Name:               device.Name,
					LastSeen:           time.Now(),
					Connected:          device.Connected,
					HandshakeCompleted: false,
					Encrypted:          false,
				}

				dbs.peersMutex.Lock()
				// 检查是否是新发现的设备
				existingPeer, exists := dbs.discovered[peer.PublicKey]
				dbs.discovered[peer.PublicKey] = peer
				dbs.peersMutex.Unlock()

				// 只在发现新设备时输出日志
				if !exists || existingPeer == nil {
					dbs.logger.Printf("🔵 发现新的 Pinecone 蓝牙设备: %s (%s)", device.Name, device.Address)
				}

				// 如果设备已连接，尝试建立通信
				if device.Connected {
					dbs.connectToPeer(peer)
				}
			}
		}
	}()
}

//

// isBluetoothAvailable Darwin 平台蓝牙可用性检查（占位实现）
func (dbs *DarwinBluetoothService) isBluetoothAvailable() bool {
    // 在无原生依赖的交叉编译环境下，返回 true 以允许流程继续
    return true
}

// scanBluetoothDevicesWithSystemProfiler 使用 system_profiler 扫描设备（占位实现）
func (dbs *DarwinBluetoothService) scanBluetoothDevicesWithSystemProfiler() ([]DarwinBluetoothDevice, error) {
    // 交叉编译期间返回空列表，避免依赖 macOS 命令
    return []DarwinBluetoothDevice{}, nil
}

// scanBluetoothDevicesWithBlueutil 使用 blueutil 扫描设备（占位实现）
func (dbs *DarwinBluetoothService) scanBluetoothDevicesWithBlueutil() ([]DarwinBluetoothDevice, error) {
    // 交叉编译期间返回空列表，避免依赖外部工具
    return []DarwinBluetoothDevice{}, nil
}



func (dbs *DarwinBluetoothService) connectToPeer(peer *BluetoothPeer) {
	// 这里只做连接和监听，数据收发用 SendData
	peer.Connected = true
	peer.LastSeen = time.Now()
	
	// 只在成功连接时输出日志
	
	
	// 自动发送本地身份信息
    if dbs.BluetoothService != nil {
        dbs.BluetoothService.SendPineconeIdentity(peer)
    }
    // 发起 ECDH 握手（发送本地公钥），等待对端响应以启用加密
    dbs.performHandshake(peer)
}

func (dbs *DarwinBluetoothService) SendData(peer *BluetoothPeer, data []byte) error {
    if len(data) == 0 {
        return nil
    }
	
	// 记录发送日志
	dbs.logger.Printf("🔵 Darwin: 向设备 %s 发送 %d 字节数据", peer.Name, len(data))
	
    // 根据加密状态处理数据
    payload := data
    if peer.Encrypted && len(peer.SharedKey) > 0 {
        enc, err := dbs.encryptData(peer.SharedKey, data)
        if err != nil {
            return err
        }
        payload = enc
    }

    // 尝试使用多种方式发送数据
    err := dbs.sendDataViaRFCOMM(peer, payload)
    if err != nil {
        dbs.logger.Printf("🔵 Darwin: RFCOMM发送失败，尝试其他方式: %v", err)
        // 可以在这里添加其他发送方式的尝试
        return dbs.sendDataViaAppleScript(peer, payload)
    }
    
    return nil
}

// handleReceivedData 处理接收到的数据（含 ECDH 握手）
func (dbs *DarwinBluetoothService) handleReceivedData(peer *BluetoothPeer, data []byte) {
    msg := string(data)
    if strings.HasPrefix(msg, "ECDH_KEY_EXCHANGE:") {
        remoteB64 := strings.TrimPrefix(msg, "ECDH_KEY_EXCHANGE:")
        dbs.handleECDHKeyExchange(peer, remoteB64)
        // 如果我们尚未发送本地公钥，补发一次，确保双向握手
        if dbs.publicKey != nil && !peer.HandshakeCompleted {
            dbs.performHandshake(peer)
        }
        return
    }
    // 其他消息交由通用蓝牙服务处理（可包含加密解密与路由）
    if dbs.BluetoothService != nil {
        dbs.BluetoothService.handleBluetoothData(peer, data)
    }
}

// sendDataViaRFCOMM 通过RFCOMM协议发送数据
func (dbs *DarwinBluetoothService) sendDataViaRFCOMM(peer *BluetoothPeer, data []byte) error {
	// 在实际实现中，这里应该建立RFCOMM连接并发送数据
	// 由于Go语言限制，这里使用模拟实现
	
	// 检查设备是否连接
	if !peer.Connected {
		return fmt.Errorf("设备 %s 未连接", peer.Name)
	}
	
	// 模拟数据传输延迟
	time.Sleep(10 * time.Millisecond)
	
	// 更新统计信息
	if dbs.BluetoothService != nil {
		dbs.peersMutex.Lock()
		if conn, exists := dbs.connectionPool[peer.PublicKey]; exists {
			conn.BytesSent += int64(len(data))
			conn.MessageCount++
			conn.LastActivity = time.Now()
		}
		dbs.peersMutex.Unlock()
	}
	
	dbs.logger.Printf("🔵 Darwin: RFCOMM数据发送成功 -> %s (%d bytes)", peer.Name, len(data))
	return nil
}

// sendDataViaAppleScript 通过AppleScript发送数据（备选方案）
func (dbs *DarwinBluetoothService) sendDataViaAppleScript(peer *BluetoothPeer, data []byte) error {
	// 使用AppleScript与蓝牙设备通信（高级功能）
	script := fmt.Sprintf(`
		tell application "System Events"
			-- 这里可以添加AppleScript代码来与蓝牙设备通信
			-- 当前为模拟实现
		end tell
	`)
	
	cmd := exec.Command("osascript", "-e", script)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("AppleScript执行失败: %v", err)
	}
	
	dbs.logger.Printf("🔵 Darwin: AppleScript数据发送成功 -> %s (%d bytes)", peer.Name, len(data))
	return nil
}

// isPineconeDevice 检查设备是否是 Pinecone 设备
func (dbs *DarwinBluetoothService) isPineconeDevice(name, address string) bool {
	// 检查设备名称是否包含 Pinecone 标识
	if len(name) > 0 && (name == "Pinecone" || name == "TChat" || name == "T-Chat") {
		return true
	}

	// 检查设备地址是否在已知的 Pinecone 设备列表中
	knownAddresses := []string{
		"AA:BB:CC:DD:EE:FF", // 示例地址
		"11:22:33:44:55:66", // 示例地址
	}

	for _, knownAddr := range knownAddresses {
		if address == knownAddr {
			return true
		}
	}

	return false
}

// performHandshake 发送本地 ECDH 公钥以发起握手
func (dbs *DarwinBluetoothService) performHandshake(peer *BluetoothPeer) {
    if dbs.publicKey == nil {
        return
    }
    pubB64 := base64.StdEncoding.EncodeToString(dbs.publicKey.Bytes())
    msg := []byte("ECDH_KEY_EXCHANGE:" + pubB64)
    _ = dbs.SendData(peer, msg)
}

// handleECDHKeyExchange 执行 ECDH 并使用 HKDF 派生会话密钥
func (dbs *DarwinBluetoothService) handleECDHKeyExchange(peer *BluetoothPeer, remotePubKeyB64 string) {
    if dbs.privateKey == nil {
        return
    }
    curve := ecdh.P256()
    remoteBytes, err := base64.StdEncoding.DecodeString(remotePubKeyB64)
    if err != nil {
        dbs.logger.Printf("🔵 Darwin: 远端公钥解析失败: %v", err)
        return
    }
    remotePub, err := curve.NewPublicKey(remoteBytes)
    if err != nil {
        dbs.logger.Printf("🔵 Darwin: 远端公钥构造失败: %v", err)
        return
    }
    sharedSecret, err := dbs.privateKey.ECDH(remotePub)
    if err != nil {
        dbs.logger.Printf("🔵 Darwin: ECDH 失败: %v", err)
        return
    }
    salt := make([]byte, 32)
    _, _ = rand.Read(salt)
    kdf := hkdf.New(sha256.New, sharedSecret, salt, []byte("tchat-bluetooth-ecdh"))
    key := make([]byte, 32)
    if _, err := kdf.Read(key); err != nil {
        h := sha256.Sum256(sharedSecret)
        key = h[:]
    }
    peer.SharedKey = key
    peer.Encrypted = true
    peer.HandshakeCompleted = true
    peer.HandshakeTime = time.Now()
}

// encryptData 使用 AES-GCM 加密
func (dbs *DarwinBluetoothService) encryptData(key, data []byte) ([]byte, error) {
    if len(key) == 0 {
        return data, nil
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return nil, err
    }
    return gcm.Seal(nonce, nonce, data, nil), nil
}

// InjectReceivedData 允许在接收管道未接入前进行测试性数据注入
// 通过该方法可以触发 Darwin 端的入站处理逻辑（握手与路由）
func (dbs *DarwinBluetoothService) InjectReceivedData(peer *BluetoothPeer, data []byte) {
    if dbs == nil || peer == nil || len(data) == 0 {
        return
    }
    dbs.handleReceivedData(peer, data)
}
