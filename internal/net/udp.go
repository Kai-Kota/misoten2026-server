package net

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
)

// UDPPacket は検証済みのヘッダと payload。
type UDPPacket struct {
	Addr    netip.AddrPort
	Header  UDPHeader
	Payload []byte
}

// UDPServer は PlayerState の受信と WorldSnapshot の送信に使う。
type UDPServer struct {
	conn   *net.UDPConn
	logger *slog.Logger
}

func ListenUDP(port int, logger *slog.Logger) (*UDPServer, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return nil, err
	}
	return &UDPServer{conn: conn, logger: logger.With("component", "udp")}, nil
}

// Serve は ctx が終わるまで受信し、形式の正しいパケットだけを packets に流す。
// ゲームループが詰まっているときは再送しない前提なので捨てる。
func (s *UDPServer) Serve(ctx context.Context, packets chan<- UDPPacket) {
	go func() {
		<-ctx.Done()
		_ = s.conn.Close()
	}()
	buffer := make([]byte, 2048)
	for {
		n, addr, err := s.conn.ReadFromUDPAddrPort(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Debug("read", "err", err)
			continue
		}
		header, payload, err := DecodePacket(buffer[:n])
		if err != nil {
			s.logger.Debug("drop packet", "addr", addr.String(), "err", err)
			continue
		}
		packet := UDPPacket{Addr: addr, Header: header, Payload: append([]byte(nil), payload...)}
		select {
		case packets <- packet:
		default:
			s.logger.Warn("udp queue full; dropped", "addr", addr.String())
		}
	}
}

// SendTo は1パケットを送る。ゲームループから呼ぶ。
func (s *UDPServer) SendTo(addr netip.AddrPort, packet []byte) error {
	_, err := s.conn.WriteToUDPAddrPort(packet, addr)
	return err
}
