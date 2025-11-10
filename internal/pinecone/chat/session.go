package chat

import (
	"encoding/hex"
	"log"
	"net"
)

type Session struct {
	conn net.Conn
}

func (s *Session) ReadMessage() ([]byte, error) {
	buf := make([]byte, 4096)
	n, err := s.conn.Read(buf)
	if err != nil {
		log.Printf("[Session][Read] 从 %s 收到数据失败: %v", s.conn.RemoteAddr(), err)
		return nil, err
	}
	log.Printf("[Session][Read] 从 %s 收到数据: 长度=%d, hex=%s, str=%q", s.conn.RemoteAddr(), n, hex.EncodeToString(buf[:n]), string(buf[:n]))
	return buf[:n], nil
}

func (s *Session) WriteMessage(data []byte) error {
	n, err := s.conn.Write(data)
	if err != nil {
		log.Printf("[Session][Write] 向 %s 发送数据失败: %v", s.conn.RemoteAddr(), err)
		return err
	}
	log.Printf("[Session][Write] 向 %s 发送数据: 长度=%d, hex=%s, str=%q", s.conn.RemoteAddr(), n, hex.EncodeToString(data[:n]), string(data[:n]))
	return nil
}
