//go:build !linux && !darwin && !netbsd && !freebsd && !openbsd && !dragonflybsd && !windows
// +build !linux,!darwin,!netbsd,!freebsd,!openbsd,!dragonflybsd,!windows

package bluetooth

// OtherBluetoothService 其他平台蓝牙服务
type OtherBluetoothService struct {
	*BluetoothService
}

// NewOtherBluetoothService 创建其他平台蓝牙服务
func NewOtherBluetoothService(bs *BluetoothService) *OtherBluetoothService {
	return &OtherBluetoothService{
		BluetoothService: bs,
	}
}

// scanForPineconeDevices 其他平台蓝牙设备扫描
func (obs *OtherBluetoothService) scanForPineconeDevices() {
	// 当前平台不支持蓝牙功能，静默处理
}

// connectToPeer 其他平台蓝牙连接
func (obs *OtherBluetoothService) connectToPeer(peer *BluetoothPeer) {
	// 其他平台暂不支持蓝牙功能，静默处理
}

// isPineconeDevice 检查设备是否是 Pinecone 设备
func (obs *OtherBluetoothService) isPineconeDevice(name, address string) bool {
	// 其他平台暂不支持蓝牙功能
	return false
}
