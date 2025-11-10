package util

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"time"
)

type SlowConn struct {
	net.Conn
	ReadDelay   time.Duration
	ReadJitter  time.Duration
	WriteDelay  time.Duration
	WriteJitter time.Duration
}

func (p *SlowConn) Read(b []byte) (n int, err error) {
	duration := p.ReadDelay
	if j := p.ReadJitter; j > 0 {
		// 使用加密安全的随机数生成器
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err == nil {
			jitter := binary.BigEndian.Uint32(buf[:]) % uint32(j)
			duration += time.Duration(jitter)
		}
	}
	if duration > 0 {
		time.Sleep(duration)
	}
	return p.Conn.Read(b)
}

func (p *SlowConn) Write(b []byte) (n int, err error) {
	duration := p.WriteDelay
	if j := p.WriteJitter; j > 0 {
		// 使用加密安全的随机数生成器
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err == nil {
			jitter := binary.BigEndian.Uint32(buf[:]) % uint32(j)
			duration += time.Duration(jitter)
		}
	}
	if duration > 0 {
		time.Sleep(duration)
	}
	return p.Conn.Write(b)
}
