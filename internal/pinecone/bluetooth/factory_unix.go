//go:build linux || netbsd || freebsd || openbsd || dragonflybsd
// +build linux netbsd freebsd openbsd dragonflybsd

package bluetooth

// createPlatformBluetoothService Unix 平台实现
func createPlatformBluetoothService(bs *BluetoothService) PlatformBluetoothService {
	return NewUnixBluetoothService(bs)
}
