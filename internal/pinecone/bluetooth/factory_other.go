//go:build !linux && !darwin && !netbsd && !freebsd && !openbsd && !dragonflybsd && !windows
// +build !linux,!darwin,!netbsd,!freebsd,!openbsd,!dragonflybsd,!windows

package bluetooth

// createPlatformBluetoothService 其他平台实现
func createPlatformBluetoothService(bs *BluetoothService) PlatformBluetoothService {
	return NewOtherBluetoothService(bs)
}
