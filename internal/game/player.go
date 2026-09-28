package game

import (
	"net/netip"
	"time"

	gnet "misoten2026/server/internal/net"
)

// Conn はプレイヤーの TCP 接続。テストで差し替えられるよう interface にしている。
type Conn interface {
	ID() uint64
	Send(msgType uint16, payload []byte) bool
	Close()
}

// Player は1つの枠（playerId 0〜3）。切断中も再接続の猶予期間は枠を保持する。
// ゲームループの goroutine 以外から触らないこと。
type Player struct {
	ID    int
	Name  string
	Token uint32

	Conn           Conn // 切断中は nil
	LastReceivedAt time.Time
	DisconnectedAt time.Time
	IsReady        bool
	IsLoaded       bool

	UDPAddr    netip.AddrPort // UdpBind が届くまでは無効
	HasUDPSeq  bool
	LastUDPSeq uint16
	SendSeq    uint16

	State    gnet.PlayerState
	HasState bool
}

func (p *Player) IsConnected() bool { return p.Conn != nil }

func (p *Player) markDisconnected(now time.Time) {
	p.Conn = nil
	p.DisconnectedAt = now
	p.IsReady = false
	p.IsLoaded = false
	p.UDPAddr = netip.AddrPort{}
	p.HasUDPSeq = false
	p.HasState = false
}
