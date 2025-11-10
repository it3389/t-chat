//go:build linux || netbsd || freebsd || openbsd || dragonflybsd
// +build linux netbsd freebsd openbsd dragonflybsd

package bluetooth

import (
    "bytes"
    "crypto/aes"
    "crypto/cipher"
    "crypto/ecdh"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "fmt"
    "io"
    "os"
    "os/exec"
    "time"

    "golang.org/x/crypto/hkdf"
)

// UnixBluetoothService Unix/Linux 平台蓝牙服务
type UnixBluetoothService struct {
    *BluetoothService
    privateKey *ecdh.PrivateKey
    publicKey  *ecdh.PublicKey
}

// NewUnixBluetoothService 创建 Unix 蓝牙服务
func NewUnixBluetoothService(bs *BluetoothService) *UnixBluetoothService {
    // 初始化 ECDH 密钥对（P-256）
    curve := ecdh.P256()
    priv, err := curve.GenerateKey(rand.Reader)
    if err != nil {
        bs.logger.Printf("[Bluetooth][Unix] 生成 ECDH 私钥失败: %v", err)
    }
    var pub *ecdh.PublicKey
    if priv != nil {
        pub = priv.PublicKey()
    }
    return &UnixBluetoothService{
        BluetoothService: bs,
        privateKey:       priv,
        publicKey:        pub,
    }
}

// scanForPineconeDevices Unix 平台蓝牙设备扫描
// 使用 BlueZ 或其他 Linux 蓝牙 API 进行真实的蓝牙设备扫描
func (ubs *UnixBluetoothService) scanForPineconeDevices() {
	// TODO: 实现真正的 BlueZ 蓝牙扫描
	// 1. 使用 BlueZ D-Bus API 扫描设备
	// 2. 检查设备名称或服务是否包含 Pinecone 标识
	// 3. 尝试建立连接并交换公钥信息
	
	// 当前暂时不执行任何操作，等待真正的 BlueZ 实现
	// 这避免了模拟数据的产生
}



func (ubs *UnixBluetoothService) SendData(peer *BluetoothPeer, data []byte) error {
    dev := "/dev/rfcomm0"
    f, err := os.OpenFile(dev, os.O_RDWR|os.O_SYNC, 0666)
    if err != nil {
        return err
    }
    defer f.Close()
    // 根据会话状态决定是否加密
    payload := data
    if peer.Encrypted && len(peer.SharedKey) > 0 {
        enc, err := ubs.encryptData(peer.SharedKey, data)
        if err != nil {
            return err
        }
        payload = enc
    }
    _, err = f.Write(payload)
    return err
}

func (ubs *UnixBluetoothService) connectToPeer(peer *BluetoothPeer) {
	port := 1 // SPP 通常在 channel 1
	rfcommDev := "/dev/rfcomm0"
	cmd := exec.Command("rfcomm", "bind", "0", peer.Address, fmt.Sprintf("%d", port))
	if err := cmd.Run(); err != nil {
		// 屏蔽连接失败的详细日志
		return
	}
    peer.Connected = true
    peer.LastSeen = time.Now()
	
	// 只在成功连接时输出日志
	
	
    // 自动发送本地身份信息
    if ubs.BluetoothService != nil {
        ubs.BluetoothService.SendPineconeIdentity(peer)
    }
    f, err := os.OpenFile(rfcommDev, os.O_RDWR|os.O_SYNC, 0666)
    if err != nil {
        // 屏蔽打开串口失败的详细日志
        return
    }
    // 发起 ECDH 握手
    ubs.performHandshake(peer)
    go func() {
        buf := make([]byte, 1024)
        for {
            n, err := f.Read(buf)
            if err != nil || n == 0 {
                break
            }
            // 如果已加密则先解密
            data := buf[:n]
            if peer.Encrypted && len(peer.SharedKey) > 0 {
                dec, derr := ubs.decryptData(peer.SharedKey, data)
                if derr == nil {
                    data = dec
                }
            }
            ubs.handleReceivedData(peer, data)
            // 同步到通用处理（路由等）
            if ubs.BluetoothService != nil {
                ubs.BluetoothService.handleBluetoothData(peer, data)
            }
        }
        f.Close()
        exec.Command("rfcomm", "release", "0").Run()
    }()
}

// isPineconeDevice 检查设备是否是 Pinecone 设备
func (ubs *UnixBluetoothService) isPineconeDevice(name, address string) bool {
	// 检查设备名称是否包含 Pinecone 标识
	if len(name) > 0 && (name == "Pinecone" || name == "TChat" || name == "T-Chat") {
		return true
	}

	// 检查设备地址是否在已知的 Pinecone 设备列表中
	knownAddresses := []string{
		"11:22:33:44:55:66", // 示例地址
		"22:33:44:55:66:77", // 示例地址
	}

	for _, knownAddr := range knownAddresses {
		if address == knownAddr {
			return true
		}
	}

	return false
}

// performHandshake 发送本地 ECDH 公钥以发起密钥协商
func (ubs *UnixBluetoothService) performHandshake(peer *BluetoothPeer) {
    if ubs.publicKey == nil {
        return
    }
    pubBytes := ubs.publicKey.Bytes()
    pubB64 := base64.StdEncoding.EncodeToString(pubBytes)
    msg := []byte("ECDH_KEY_EXCHANGE:" + pubB64)
    _ = ubs.SendData(peer, msg)
    if ubs.BluetoothService != nil {
        ubs.BluetoothService.logger.Printf("[Bluetooth][Unix] 已发送本地公钥进行握手: peer=%s", peer.Address)
    }
}

// handleReceivedData 处理接收到的数据（含 ECDH 握手）
func (ubs *UnixBluetoothService) handleReceivedData(peer *BluetoothPeer, data []byte) {
    if bytes.HasPrefix(data, []byte("ECDH_KEY_EXCHANGE:")) {
        remoteB64 := string(bytes.TrimPrefix(data, []byte("ECDH_KEY_EXCHANGE:")))
        ubs.handleECDHKeyExchange(peer, remoteB64)
        // 对方未发送过，我们也发送一次，确保双向
        ubs.performHandshake(peer)
        return
    }
    if bytes.HasPrefix(data, []byte("HANDSHAKE_RESPONSE:")) {
        // 兼容旧握手，使用旧的共享密钥协商逻辑，但不覆盖已有安全密钥
        if ubs.BluetoothService != nil {
            ubs.BluetoothService.negotiateSharedKey(peer)
        }
        return
    }
}

// handleECDHKeyExchange 执行 ECDH 并派生会话密钥（HKDF-SHA256）
func (ubs *UnixBluetoothService) handleECDHKeyExchange(peer *BluetoothPeer, remotePubKeyB64 string) {
    if ubs.privateKey == nil {
        return
    }
    curve := ecdh.P256()
    remoteBytes, err := base64.StdEncoding.DecodeString(remotePubKeyB64)
    if err != nil {
        if ubs.BluetoothService != nil {
            ubs.BluetoothService.logger.Printf("[Bluetooth][Unix] 远端公钥解析失败: %v", err)
        }
        return
    }
    remotePub, err := curve.NewPublicKey(remoteBytes)
    if err != nil {
        if ubs.BluetoothService != nil {
            ubs.BluetoothService.logger.Printf("[Bluetooth][Unix] 远端公钥构造失败: %v", err)
        }
        return
    }
    sharedSecret := ubs.privateKey.ECDH(remotePub)
    // HKDF 派生密钥
    salt := make([]byte, 32)
    _, _ = rand.Read(salt)
    hkdfReader := hkdf.New(sha256.New, sharedSecret, salt, []byte("tchat-bluetooth-ecdh"))
    sessionKey := make([]byte, 32)
    if _, err := io.ReadFull(hkdfReader, sessionKey); err != nil {
        if ubs.BluetoothService != nil {
            ubs.BluetoothService.logger.Printf("[Bluetooth][Unix] HKDF 派生失败: %v", err)
        }
        return
    }

    peer.SharedKey = sessionKey
    peer.Encrypted = true
    peer.HandshakeCompleted = true
    peer.HandshakeTime = time.Now()
    if ubs.BluetoothService != nil {
        ubs.BluetoothService.logger.Printf("[Bluetooth][Unix] 握手完成并启用加密: peer=%s", peer.Address)
    }
}

// encryptData 使用 AES-GCM 加密
func (ubs *UnixBluetoothService) encryptData(key, data []byte) ([]byte, error) {
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
    ciphertext := gcm.Seal(nonce, nonce, data, nil)
    return ciphertext, nil
}

// decryptData 使用 AES-GCM 解密
func (ubs *UnixBluetoothService) decryptData(key, data []byte) ([]byte, error) {
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
    nonceSize := gcm.NonceSize()
    if len(data) < nonceSize {
        return nil, fmt.Errorf("数据长度不足以解密")
    }
    nonce := data[:nonceSize]
    ciphertext := data[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, err
    }
    return plaintext, nil
}
