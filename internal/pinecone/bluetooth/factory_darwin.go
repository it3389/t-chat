//go:build darwin
// +build darwin

package bluetooth

// createPlatformBluetoothService Darwin 平台实现
func createPlatformBluetoothService(bs *BluetoothService) PlatformBluetoothService {
	return NewDarwinBluetoothService(bs)
}
