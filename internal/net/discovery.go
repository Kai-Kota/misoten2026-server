package net

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/netip"
	"time"
)

// RunDiscovery は interval ごとに ServerAnnounce をブロードキャストする。
// 255.255.255.255 に加え、NIC が複数あっても届くよう各 NIC のブロードキャストアドレスにも送る。
func RunDiscovery(ctx context.Context, port int, interval time.Duration, announce func() ServerAnnounce, logger *slog.Logger) {
	logger = logger.With("component", "discovery")
	// Go の UDP ソケットは SO_BROADCAST が有効な状態で作られる。
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		logger.Error("listen", "err", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var seq uint16
	for {
		seq++
		payload, err := json.Marshal(announce())
		if err == nil {
			packet, encodeErr := EncodePacket(UDPHeader{MsgType: MsgServerAnnounce, PlayerID: ServerPlayerID, Seq: seq}, payload)
			if encodeErr == nil {
				for _, addr := range broadcastAddrs(port) {
					if _, err := conn.WriteToUDPAddrPort(packet, addr); err != nil {
						logger.Debug("send", "addr", addr.String(), "err", err)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func broadcastAddrs(port int) []netip.AddrPort {
	addrs := []netip.AddrPort{netip.AddrPortFrom(netip.AddrFrom4([4]byte{255, 255, 255, 255}), uint16(port))}
	interfaces, err := net.Interfaces()
	if err != nil {
		return addrs
	}
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		interfaceAddrs, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, interfaceAddr := range interfaceAddrs {
			ipNet, ok := interfaceAddr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || len(ipNet.Mask) != net.IPv4len {
				continue
			}
			var broadcast [4]byte
			for index := range broadcast {
				broadcast[index] = ip[index] | ^ipNet.Mask[index]
			}
			addrs = append(addrs, netip.AddrPortFrom(netip.AddrFrom4(broadcast), uint16(port)))
		}
	}
	return addrs
}
