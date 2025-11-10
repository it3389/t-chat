//go:build windows
// +build windows

package bluetooth

// createPlatformBluetoothService Windows 平台实现
func createPlatformBluetoothService(bs *BluetoothService) PlatformBluetoothService {
	return NewWindowsBluetoothService(bs)
}
