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
	// 選んだキャラ（0〜3）。LoadComplete で届く。まだなら -1。
	CharacterID int

	UDPAddr    netip.AddrPort // UdpBind が届くまでは無効
	HasUDPSeq  bool
	LastUDPSeq uint16
	SendSeq    uint16

	State    gnet.PlayerState
	HasState bool

	// 自分が蘇生している相手。IsReviving のときだけ意味を持つ。
	IsReviving      bool
	ReviveTargetID  int
	ReviveStartedAt time.Time
	// 蘇生が成立し、本人のクライアントから生きている状態が届くのを待っている。
	IsRevivePending bool
}

// IsDead は本人のクライアントが送ってきた状態で判断する。
func (p *Player) IsDead() bool { return p.HasState && p.State.Flags&gnet.FlagDead != 0 }

func (p *Player) clearRevive() {
	p.IsReviving = false
	p.ReviveTargetID = 0
	p.ReviveStartedAt = time.Time{}
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
	p.IsRevivePending = false
	p.clearRevive()
}
