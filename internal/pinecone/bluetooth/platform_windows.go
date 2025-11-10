//go:build windows
// +build windows

package bluetooth

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/ecdh"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io"
    "os/exec"
    "strings"
    "time"
    "unsafe"

    "github.com/tarm/serial"
    "golang.org/x/crypto/hkdf"
    "golang.org/x/sys/windows"
)

// Windows 蓝牙 API 常量
const (
	BLUETOOTH_MAX_NAME_SIZE             = 248
	BLUETOOTH_ADDRESS_STRUCT_SIZE       = 6
	BLUETOOTH_DEVICE_INFO_STRUCT_SIZE   = 560
	BLUETOOTH_DEVICE_SEARCH_PARAMS_SIZE = 40
)

// Windows 蓝牙 API 结构体
type BLUETOOTH_ADDRESS struct {
	ullLong uint64
}

type BLUETOOTH_DEVICE_INFO struct {
	DwSize          uint32
	Address         BLUETOOTH_ADDRESS
	UlClassofDevice uint32
	FConnected      uint32
	FRemembered     uint32
	FAuthenticated  uint32
	StLastSeen      windows.Systemtime
	StLastUsed      windows.Systemtime
	SzName          [BLUETOOTH_MAX_NAME_SIZE]uint16
}

type BLUETOOTH_DEVICE_SEARCH_PARAMS struct {
	DwSize               uint32
	FReturnAuthenticated uint32
	FReturnRemembered    uint32
	FReturnUnknown       uint32
	FReturnConnected     uint32
	FIssueInquiry        uint32
	CtimeoutMultiplier   uint32
	HRadio               windows.Handle
}

type BLUETOOTH_RADIO_INFO struct {
	DwSize          uint32
	Address         BLUETOOTH_ADDRESS
	SzName          [BLUETOOTH_MAX_NAME_SIZE]uint16
	UlClassofDevice uint32
	LmpSubversion   uint16
	Manufacturer    uint16
}

// Windows 蓝牙 API 函数
var (
	bluetoothFindFirstDevice = windows.NewLazySystemDLL("Bthprops.cpl").NewProc("BluetoothFindFirstDevice")
	bluetoothFindNextDevice  = windows.NewLazySystemDLL("Bthprops.cpl").NewProc("BluetoothFindNextDevice")
	bluetoothFindDeviceClose = windows.NewLazySystemDLL("Bthprops.cpl").NewProc("BluetoothFindDeviceClose")
	bluetoothFindFirstRadio  = windows.NewLazySystemDLL("Bthprops.cpl").NewProc("BluetoothFindFirstRadio")
	bluetoothFindRadioClose  = windows.NewLazySystemDLL("Bthprops.cpl").NewProc("BluetoothFindRadioClose")
)

//// WindowsBluetoothService Windows 平台的蓝牙服务实现
type WindowsBluetoothService struct {
	*BluetoothService
	radioHandle windows.Handle // 蓝牙无线电句柄
	// ECDH密钥对用于安全密钥交换
	privateKey *ecdh.PrivateKey
	publicKey  *ecdh.PublicKey
}

// NewWindowsBluetoothService 创建 Windows 蓝牙服务
func NewWindowsBluetoothService(bs *BluetoothService) *WindowsBluetoothService {
	// 初始化实例
	wbs := &WindowsBluetoothService{BluetoothService: bs}
	// 生成 ECDH 密钥对
	curve := ecdh.P256()
	priv, err := curve.GenerateKey(rand.Reader)
	if err == nil {
		wbs.privateKey = priv
		wbs.publicKey = priv.PublicKey()
		if bs != nil && bs.logger != nil {
			bs.logger.Printf("Windows: 已初始化 ECDH 密钥对")
		}
	} else {
		if bs != nil && bs.logger != nil {
			bs.logger.Printf("Windows: 初始化 ECDH 密钥对失败: %v", err)
		}
	}
	return wbs
}

// scanForPineconeDevices Windows 平台蓝牙设备扫描
// 使用真实的 Windows Bluetooth API 进行设备扫描
func (wbs *WindowsBluetoothService) scanForPineconeDevices() {
	// 在单独的goroutine中进行蓝牙扫描，避免阻塞主线程
	go func() {
		// 查找第一个蓝牙无线电
		var findHandle windows.Handle
		radioInfo := BLUETOOTH_RADIO_INFO{
			DwSize: uint32(unsafe.Sizeof(BLUETOOTH_RADIO_INFO{})),
		}

		ret, _, _ := bluetoothFindFirstRadio.Call(
			uintptr(unsafe.Pointer(&radioInfo)),
			uintptr(unsafe.Pointer(&findHandle)),
		)
		if ret == 0 {
			// 屏蔽未找到蓝牙无线电的日志
			return
		}
		defer bluetoothFindRadioClose.Call(uintptr(findHandle))

		wbs.radioHandle = windows.Handle(findHandle)

		// 设置设备搜索参数
		searchParams := BLUETOOTH_DEVICE_SEARCH_PARAMS{
			DwSize:               uint32(unsafe.Sizeof(BLUETOOTH_DEVICE_SEARCH_PARAMS{})),
			FReturnAuthenticated: 1,
			FReturnRemembered:    1,
			FReturnUnknown:       1,
			FReturnConnected:     1,
			FIssueInquiry:        1,
			CtimeoutMultiplier:   2, // 减少超时时间
			HRadio:               wbs.radioHandle,
		}

		// 查找第一个设备
		var deviceInfo BLUETOOTH_DEVICE_INFO
		deviceInfo.DwSize = uint32(unsafe.Sizeof(BLUETOOTH_DEVICE_INFO{}))

		deviceHandle, _, _ := bluetoothFindFirstDevice.Call(
			uintptr(unsafe.Pointer(&searchParams)),
			uintptr(unsafe.Pointer(&deviceInfo)),
		)

		if deviceHandle == 0 {
			// 屏蔽未找到蓝牙设备的日志
			return
		}
		defer bluetoothFindDeviceClose.Call(deviceHandle)

			// 遍历所有设备
			for {
				deviceName := windows.UTF16ToString(deviceInfo.SzName[:])
				address := wbs.formatBluetoothAddress(deviceInfo.Address)

				// 检查是否是 Pinecone 设备
				if wbs.isPineconeDevice(deviceName, address) {
					peer := &BluetoothPeer{
						PublicKey:        fmt.Sprintf("bt_%s", address),
						Address:          address,
						Name:             deviceName,
						LastSeen:         wbs.getCurrentTime(),
						Connected:        deviceInfo.FConnected != 0,
						HandshakeCompleted: false,
						Encrypted:        false,
					}

					wbs.peersMutex.Lock()
					// 检查是否是新发现的设备
					existingPeer, exists := wbs.discovered[peer.PublicKey]
					wbs.discovered[peer.PublicKey] = peer
					wbs.peersMutex.Unlock()

					// 只在发现新设备时输出日志
					if !exists || existingPeer == nil {
						wbs.logger.Printf("🔵 发现新的 Pinecone 蓝牙设备: %s (%s)", deviceName, address)
					}
				}

				// 查找下一个设备
				ret, _, _ := bluetoothFindNextDevice.Call(
					deviceHandle,
					uintptr(unsafe.Pointer(&deviceInfo)),
				)

				if ret == 0 {
					break
				}
			}
	}()
}

// isPineconeDevice 检查设备是否是 Pinecone 设备
func (wbs *WindowsBluetoothService) isPineconeDevice(name, address string) bool {
	// 检查设备名称是否包含 Pinecone 标识
	if len(name) > 0 {
		nameLower := strings.ToLower(name)
		if strings.Contains(nameLower, "pinecone") || 
		   strings.Contains(nameLower, "tchat") || 
		   strings.Contains(nameLower, "t-chat") {
			return true
		}
	}

	// 检查设备地址是否在已知的 Pinecone 设备列表中
	knownAddresses := []string{
		"00:11:22:33:44:55", // 示例地址
		"AA:BB:CC:DD:EE:FF", // 示例地址
	}

	for _, knownAddr := range knownAddresses {
		if address == knownAddr {
			return true
		}
	}

	return false
}

// connectToPeer 连接到指定设备
func (wbs *WindowsBluetoothService) connectToPeer(peer *BluetoothPeer) {
	// 获取蓝牙串口
	portName, err := wbs.getBluetoothComPort(peer.Address)
	if err != nil {
		// 屏蔽连接失败的详细日志
		return
	}

	// 配置串口
	config := &serial.Config{
		Name:        portName,
		Baud:        9600,
		ReadTimeout: time.Second * 5,
		Size:        8,
		Parity:      serial.ParityNone,
		StopBits:    serial.Stop1,
	}

	// 打开串口连接
	port, err := serial.OpenPort(config)
	if err != nil {
		// 屏蔽连接失败的详细日志
		return
	}

	// 更新设备状态
	peer.Connected = true
	peer.LastSeen = time.Now()

	// 创建连接记录
	conn := &BluetoothConnection{
		Peer:         peer,
		ConnectedAt:  time.Now(),
		LastActivity: time.Now(),
		Status:       "connected",
	}

	wbs.poolMutex.Lock()
	wbs.connectionPool[peer.PublicKey] = conn
	wbs.poolMutex.Unlock()

	// 只在成功连接时输出日志
	wbs.logger.Printf("🔗 成功连接到蓝牙设备: %s (%s)", peer.Name, peer.Address)

	// 启动数据接收协程
	go wbs.receiveDataLoop(peer, port)

	// 执行握手过程
	go wbs.performHandshake(peer)
}

// SendData 发送数据到指定设备
func (wbs *WindowsBluetoothService) SendData(peer *BluetoothPeer, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	// 检查连接状态
	wbs.poolMutex.RLock()
	conn, exists := wbs.connectionPool[peer.PublicKey]
	wbs.poolMutex.RUnlock()

	if !exists || conn.Status != "connected" {
		return fmt.Errorf("设备未连接: %s", peer.Name)
	}

	// 获取蓝牙串口
	portName, err := wbs.getBluetoothComPort(peer.Address)
	if err != nil {
		return fmt.Errorf("获取蓝牙串口失败: %w", err)
	}

	// 配置串口
	config := &serial.Config{
		Name:        portName,
		Baud:        9600,
		ReadTimeout: time.Second * 5,
	}

	// 打开串口
	port, err := serial.OpenPort(config)
	if err != nil {
		return fmt.Errorf("打开串口失败: %w", err)
	}
	defer port.Close()

	// 加密数据（如果已建立加密连接）
	if peer.Encrypted && len(peer.SharedKey) > 0 {
		data, err = wbs.encryptData(data, peer.SharedKey)
		if err != nil {
			return fmt.Errorf("数据加密失败: %w", err)
		}
	}

	// 发送数据
	_, err = port.Write(data)
	if err != nil {
		return fmt.Errorf("发送数据失败: %w", err)
	}

	// 更新统计信息
	conn.BytesSent += int64(len(data))
	conn.MessageCount++
	conn.LastActivity = time.Now()

	wbs.stats.TotalBytesSent += int64(len(data))
	wbs.stats.TotalMessagesSent++
	wbs.stats.LastUpdated = time.Now()

	wbs.logger.Printf("Windows: 向设备 %s 发送 %d 字节数据", peer.Name, len(data))
	return nil
}

// 私有方法

// getBluetoothComPort 获取目标蓝牙设备的 COM 端口号
func (wbs *WindowsBluetoothService) getBluetoothComPort(address string) (string, error) {
	// 使用 PowerShell 查询蓝牙串口映射
	cmd := exec.Command("powershell", "Get-WmiObject Win32_SerialPort | Select-Object DeviceID,PNPDeviceID")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("查询串口失败: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	addrNoColon := strings.ReplaceAll(address, ":", "")

	for _, line := range lines {
		if strings.Contains(line, addrNoColon) {
			for _, part := range strings.Fields(line) {
				if strings.HasPrefix(part, "COM") {
					return part, nil
				}
			}
		}
	}

	return "", fmt.Errorf("未找到蓝牙串口: %s", address)
}

// formatBluetoothAddress 格式化蓝牙地址
func (wbs *WindowsBluetoothService) formatBluetoothAddress(addr BLUETOOTH_ADDRESS) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		byte(addr.ullLong),
		byte(addr.ullLong>>8),
		byte(addr.ullLong>>16),
		byte(addr.ullLong>>24),
		byte(addr.ullLong>>32),
		byte(addr.ullLong>>40),
	)
}

// getCurrentTime 获取当前时间
func (wbs *WindowsBluetoothService) getCurrentTime() time.Time {
	return time.Now()
}

// receiveDataLoop 数据接收循环
func (wbs *WindowsBluetoothService) receiveDataLoop(peer *BluetoothPeer, port *serial.Port) {
	defer port.Close()

	buf := make([]byte, 1024)
	for {
		n, err := port.Read(buf)
		if err != nil || n == 0 {
			wbs.logger.Printf("Windows: 设备 %s 连接断开", peer.Name)
			peer.Connected = false
			
			// 更新连接状态
			wbs.poolMutex.Lock()
			if conn, exists := wbs.connectionPool[peer.PublicKey]; exists {
				conn.Status = "disconnected"
			}
			wbs.poolMutex.Unlock()
			
			break
		}

		// 处理接收到的数据
		data := buf[:n]
		wbs.logger.Printf("Windows: 从设备 %s 接收 %d 字节数据", peer.Name, n)

		// 解密数据（如果已建立加密连接）
		if peer.Encrypted && len(peer.SharedKey) > 0 {
			decryptedData, err := wbs.decryptData(data, peer.SharedKey)
			if err != nil {
				wbs.logger.Printf("Windows: 数据解密失败: %v", err)
				continue
			}
			data = decryptedData
		}

		// 更新统计信息
		wbs.poolMutex.Lock()
		if conn, exists := wbs.connectionPool[peer.PublicKey]; exists {
			conn.BytesReceived += int64(n)
			conn.LastActivity = time.Now()
		}
		wbs.poolMutex.Unlock()

		wbs.stats.TotalBytesReceived += int64(n)
		wbs.stats.TotalMessagesRcvd++
		wbs.stats.LastUpdated = time.Now()

		// 处理接收到的消息
		wbs.handleReceivedData(peer, data)
	}
}

// performHandshake 执行握手过程
func (wbs *WindowsBluetoothService) performHandshake(peer *BluetoothPeer) {
	wbs.logger.Printf("Windows: 开始与设备 %s 的ECDH密钥交换", peer.Name)

	if wbs.publicKey == nil {
		wbs.logger.Printf("Windows: ECDH公钥未初始化")
		return
	}

	// 发送本地公钥
	localPubKeyBytes := wbs.publicKey.Bytes()
	handshakeMsg := fmt.Sprintf("ECDH_KEY_EXCHANGE:%s", hex.EncodeToString(localPubKeyBytes))
	err := wbs.SendData(peer, []byte(handshakeMsg))
	if err != nil {
		wbs.logger.Printf("Windows: 发送ECDH公钥失败: %v", err)
		return
	}

	wbs.logger.Printf("Windows: 已发送ECDH公钥给设备 %s", peer.Name)
	
	// 等待对方公钥响应（实际应用中应该有超时和重试机制）
	// 这里简化处理，实际的公钥接收在handleReceivedData中处理
}

// handleECDHKeyExchange 处理ECDH密钥交换
func (wbs *WindowsBluetoothService) handleECDHKeyExchange(peer *BluetoothPeer, remotePubKeyHex string) {
	if wbs.privateKey == nil {
		wbs.logger.Printf("Windows: ECDH私钥未初始化")
		return
	}

	// 解码远程公钥
	remotePubKeyBytes, err := hex.DecodeString(remotePubKeyHex)
	if err != nil {
		wbs.logger.Printf("Windows: 解码远程公钥失败: %v", err)
		return
	}

	// 重建远程公钥
	curve := ecdh.P256()
	remotePubKey, err := curve.NewPublicKey(remotePubKeyBytes)
	if err != nil {
		wbs.logger.Printf("Windows: 重建远程公钥失败: %v", err)
		return
	}

	// 执行ECDH密钥交换
	sharedSecret, err := wbs.privateKey.ECDH(remotePubKey)
	if err != nil {
		wbs.logger.Printf("Windows: ECDH密钥交换失败: %v", err)
		return
	}

	// 使用 HKDF-SHA256 从共享秘密派生会话密钥
	salt := make([]byte, 16)
	_, _ = rand.Read(salt) // 盐可选，失败时使用零值不会影响安全性显著，但尽量读取
	info := []byte("tchat-bluetooth-ecdh")
	kdf := hkdf.New(sha256.New, sharedSecret, salt, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(kdf, key); err != nil {
		// 回退：直接使用 SHA256(secret)
		h := sha256.Sum256(sharedSecret)
		key = h[:]
	}
	peer.SharedKey = key
	peer.Encrypted = true
	peer.HandshakeCompleted = true
	peer.HandshakeTime = time.Now()

	wbs.logger.Printf("Windows: 与设备 %s 的ECDH密钥交换完成", peer.Name)
}

// handleReceivedData 处理接收到的数据
func (wbs *WindowsBluetoothService) handleReceivedData(peer *BluetoothPeer, data []byte) {
	message := string(data)
	
	// 检查是否是ECDH密钥交换消息
	if strings.HasPrefix(message, "ECDH_KEY_EXCHANGE:") {
		remotePubKeyHex := strings.TrimPrefix(message, "ECDH_KEY_EXCHANGE:")
		wbs.logger.Printf("Windows: 收到设备 %s 的ECDH公钥", peer.Name)
		
		// 处理ECDH密钥交换
		wbs.handleECDHKeyExchange(peer, remotePubKeyHex)
		
		// 如果我们还没有发送自己的公钥，现在发送
		if wbs.publicKey != nil && !peer.HandshakeCompleted {
			localPubKeyBytes := wbs.publicKey.Bytes()
			responseMsg := fmt.Sprintf("ECDH_KEY_EXCHANGE:%s", hex.EncodeToString(localPubKeyBytes))
			wbs.SendData(peer, []byte(responseMsg))
		}
		return
	}

	// 检查是否是传统握手响应（向后兼容）
	if message == "HANDSHAKE_RESPONSE" {
		wbs.logger.Printf("Windows: 收到设备 %s 的传统握手响应", peer.Name)
		return
	}

	// 处理其他类型的消息
	wbs.logger.Printf("Windows: 处理来自设备 %s 的消息: %s", peer.Name, message)

	// 这里可以添加消息路由逻辑，将蓝牙消息转发到 Pinecone 网络
	if wbs.router != nil {
		// 将蓝牙消息转换为 Pinecone 消息并路由
		wbs.routeBluetoothMessageToPinecone(peer, data)
	}
}

// routeBluetoothMessageToPinecone 将蓝牙消息路由到 Pinecone 网络
func (wbs *WindowsBluetoothService) routeBluetoothMessageToPinecone(peer *BluetoothPeer, data []byte) {
	// 这里实现将蓝牙消息转发到 Pinecone 网络的逻辑
	wbs.logger.Printf("Windows: 将蓝牙消息路由到 Pinecone 网络: %s -> overlay", peer.Name)
	
	// 创建 Pinecone 消息
	// 这里需要根据实际的 Pinecone 消息格式来实现
	// 暂时只记录日志
}

// encryptData 加密数据
func (wbs *WindowsBluetoothService) encryptData(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	// 使用加密安全的随机数生成器
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("生成随机nonce失败: %v", err)
	}

	return gcm.Seal(nonce, nonce, data, nil), nil
}

// decryptData 解密数据
func (wbs *WindowsBluetoothService) decryptData(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("数据长度不足")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
