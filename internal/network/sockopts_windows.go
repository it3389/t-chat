//go:build windows

package network

import (
    "net"
    "syscall"
)

// EnableMulticastOptionsUDP 在 Windows 平台上为 UDP 连接启用常见多播选项
func EnableMulticastOptionsUDP(conn *net.UDPConn) error {
    file, err := conn.File()
    if err != nil {
        return err
    }
    defer file.Close()

    fd := syscall.Handle(file.Fd())

    if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1); err != nil {
        return err
    }
    if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255); err != nil {
        return err
    }
    if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
        return err
    }
    return nil
}

// SetReuseAddr 在 Windows 平台上为给定 FD 启用 SO_REUSEADDR
func SetReuseAddr(fd uintptr) error {
    return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}