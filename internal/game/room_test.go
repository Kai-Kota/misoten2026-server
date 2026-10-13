package game

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	gnet "misoten2026/server/internal/net"
)

type sentFrame struct {
	msgType uint16
	payload []byte
}

type fakeConn struct {
	id       uint64
	sent     []sentFrame
	isClosed bool
}

func (c *fakeConn) ID() uint64 { return c.id }

func (c *fakeConn) Send(msgType uint16, payload []byte) bool {
	c.sent = append(c.sent, sentFrame{msgType, payload})
	return true
}

func (c *fakeConn) Close() { c.isClosed = true }

// last は最後に受け取った msgType のメッセージを target に読み込む。
func (c *fakeConn) last(t *testing.T, msgType uint16, target any) bool {
	t.Helper()
	for index := len(c.sent) - 1; index >= 0; index-- {
		if c.sent[index].msgType == msgType {
			if err := json.Unmarshal(c.sent[index].payload, target); err != nil {
				t.Fatal(err)
			}
			return true
		}
	}
	return false
}

type sentPacket struct {
	addr    netip.AddrPort
	header  gnet.UDPHeader
	payload []byte
}

type fakeUDP struct{ sent []sentPacket }

func (u *fakeUDP) SendTo(addr netip.AddrPort, packet []byte) error {
	header, payload, err := gnet.DecodePacket(packet)
	if err != nil {
		return err
	}
	u.sent = append(u.sent, sentPacket{addr, header, append([]byte(nil), payload...)})
	return nil
}

type roomFixture struct {
	t      *testing.T
	room   *Room
	udp    *fakeUDP
	now    time.Time
	nextID uint64
}

func newFixture(t *testing.T) *roomFixture {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	udp := &fakeUDP{}
	room := NewRoom(RoomConfig{
		TickRate:        30,
		MaxPlayers:      4,
		PingTimeout:     3 * time.Second,
		ReconnectWindow: 30 * time.Second,
		StartDelayTicks: 15,
		StageName:       "TestStage",
	}, udp, slog.New(slog.NewTextHandler(io.Discard, nil)), now)
	return &roomFixture{t: t, room: room, udp: udp, now: now}
}

func (f *roomFixture) send(conn *fakeConn, msgType uint16, message any) {
	payload, err := json.Marshal(message)
	if err != nil {
		f.t.Fatal(err)
	}
	f.room.HandleFrame(conn, gnet.Frame{MsgType: msgType, Payload: payload}, f.now)
}

func (f *roomFixture) join(name string) (*fakeConn, gnet.JoinAccepted) {
	f.t.Helper()
	f.nextID++
	conn := &fakeConn{id: f.nextID}
	f.send(conn, gnet.MsgJoinRequest, gnet.JoinRequest{Version: int(gnet.ProtocolVersion), PlayerName: name})
	var accepted gnet.JoinAccepted
	if !conn.last(f.t, gnet.MsgJoinAccepted, &accepted) {
		f.t.Fatalf("%s was not accepted", name)
	}
	return conn, accepted
}

func (f *roomFixture) advance(duration time.Duration) {
	f.now = f.now.Add(duration)
	f.room.Step(f.now)
}

func (f *roomFixture) udpPacket(accepted gnet.JoinAccepted, msgType uint8, seq uint16, message any) gnet.UDPPacket {
	var payload []byte
	if message != nil {
		payload, _ = json.Marshal(message)
	}
	return gnet.UDPPacket{
		Addr:    netip.MustParseAddrPort("192.168.0.10:50000"),
		Header:  gnet.UDPHeader{MsgType: msgType, PlayerID: uint8(accepted.PlayerID), Seq: seq, Token: accepted.UDPToken},
		Payload: payload,
	}
}

func rejectReason(t *testing.T, conn *fakeConn) int {
	t.Helper()
	var rejected gnet.JoinRejected
	if !conn.last(t, gnet.MsgJoinRejected, &rejected) {
		t.Fatal("JoinRejected was not sent")
	}
	return rejected.Reason
}

func TestJoinAssignsSlotsAndRejectsWhenFull(t *testing.T) {
	f := newFixture(t)
	tokens := map[uint32]bool{}
	for index := 0; index < 4; index++ {
		_, accepted := f.join("P")
		if accepted.PlayerID != index || accepted.TickRate != 30 || accepted.UDPToken == 0 || tokens[accepted.UDPToken] {
			t.Fatalf("join %d: %+v", index, accepted)
		}
		tokens[accepted.UDPToken] = true
	}
	fifth := &fakeConn{id: 99}
	f.send(fifth, gnet.MsgJoinRequest, gnet.JoinRequest{Version: int(gnet.ProtocolVersion)})
	if reason := rejectReason(t, fifth); reason != gnet.RejectFull {
		t.Fatalf("reason = %d, want FULL", reason)
	}
}

func TestJoinRejectsVersionMismatch(t *testing.T) {
	f := newFixture(t)
	conn := &fakeConn{id: 1}
	f.send(conn, gnet.MsgJoinRequest, gnet.JoinRequest{Version: 999})
	if reason := rejectReason(t, conn); reason != gnet.RejectVersion {
		t.Fatalf("reason = %d, want VERSION", reason)
	}
}

func TestAllReadyStartsGame(t *testing.T) {
	f := newFixture(t)
	first, _ := f.join("A")
	second, _ := f.join("B")
	f.send(first, gnet.MsgSetReady, gnet.SetReady{IsReady: true})
	if f.room.phase != gnet.PhaseLobby {
		t.Fatal("started before everyone was ready")
	}
	f.send(second, gnet.MsgSetReady, gnet.SetReady{IsReady: true})
	var load gnet.LoadStage
	if f.room.phase != gnet.PhaseLoading || !second.last(t, gnet.MsgLoadStage, &load) || load.StageName != "TestStage" {
		t.Fatalf("phase = %d, load = %+v", f.room.phase, load)
	}

	f.send(first, gnet.MsgLoadComplete, gnet.LoadComplete{})
	if f.room.phase != gnet.PhaseLoading {
		t.Fatal("started before everyone loaded")
	}
	f.send(second, gnet.MsgLoadComplete, gnet.LoadComplete{})
	var start gnet.GameStart
	if f.room.phase != gnet.PhaseInGame || !first.last(t, gnet.MsgGameStart, &start) {
		t.Fatal("GameStart was not sent")
	}
	if want := f.room.TickAt(f.now) + 15; start.StartTick != want {
		t.Fatalf("startTick = %d, want %d", start.StartTick, want)
	}

	late := &fakeConn{id: 50}
	f.send(late, gnet.MsgJoinRequest, gnet.JoinRequest{Version: int(gnet.ProtocolVersion)})
	if reason := rejectReason(t, late); reason != gnet.RejectInGame {
		t.Fatalf("reason = %d, want IN_GAME", reason)
	}
}

func TestPingIsAnsweredWithServerTick(t *testing.T) {
	f := newFixture(t)
	conn, _ := f.join("A")
	f.now = f.now.Add(time.Second)
	f.send(conn, gnet.MsgPing, gnet.Ping{ClientTimeMs: 1234})
	var pong gnet.Ping
	if !conn.last(t, gnet.MsgPing, &pong) || !pong.IsPong || pong.ClientTimeMs != 1234 || pong.ServerTick != 30 {
		t.Fatalf("pong = %+v", pong)
	}
}

func TestPingTimeoutAndReconnect(t *testing.T) {
	f := newFixture(t)
	conn, accepted := f.join("A")
	f.advance(2 * time.Second)
	if !f.room.players[0].IsConnected() {
		t.Fatal("disconnected too early")
	}
	f.advance(1500 * time.Millisecond)
	if f.room.players[0] == nil || f.room.players[0].IsConnected() || !conn.isClosed {
		t.Fatal("expected ping timeout to keep the slot as disconnected")
	}

	// 空き枠があっても、新規入室は切断中の枠を奪わない。
	_, other := f.join("B")
	if other.PlayerID != 1 {
		t.Fatalf("new player took slot %d", other.PlayerID)
	}

	f.advance(10 * time.Second)
	reconnected := &fakeConn{id: 100}
	f.send(reconnected, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: accepted.UDPToken})
	var again gnet.JoinAccepted
	if !reconnected.last(t, gnet.MsgJoinAccepted, &again) || again.PlayerID != 0 || again.UDPToken != accepted.UDPToken {
		t.Fatalf("reconnect = %+v", again)
	}
	if !f.room.players[0].IsConnected() {
		t.Fatal("slot is not connected after reconnect")
	}
}

func TestReconnectRejectedAfterWindowOrWrongToken(t *testing.T) {
	f := newFixture(t)
	conn, accepted := f.join("A")
	f.room.HandleClosed(conn, f.now)

	wrong := &fakeConn{id: 10}
	f.send(wrong, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: accepted.UDPToken + 1})
	if reason := rejectReason(t, wrong); reason != gnet.RejectNotFound {
		t.Fatalf("reason = %d, want NOT_FOUND", reason)
	}

	f.advance(31 * time.Second)
	if f.room.players[0] != nil {
		t.Fatal("slot was not freed after reconnect window")
	}
	late := &fakeConn{id: 11}
	f.send(late, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: accepted.UDPToken})
	if reason := rejectReason(t, late); reason != gnet.RejectNotFound {
		t.Fatalf("reason = %d, want NOT_FOUND", reason)
	}
}

func TestReconnectDuringGameResendsGameStart(t *testing.T) {
	f := newFixture(t)
	conn, accepted := f.join("A")
	f.send(conn, gnet.MsgSetReady, gnet.SetReady{IsReady: true})
	f.send(conn, gnet.MsgLoadComplete, gnet.LoadComplete{})
	f.room.HandleClosed(conn, f.now)
	if f.room.phase != gnet.PhaseInGame {
		t.Fatal("game should keep running while the slot is reserved")
	}
	reconnected := &fakeConn{id: 20}
	f.send(reconnected, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: accepted.UDPToken})
	var start gnet.GameStart
	if !reconnected.last(t, gnet.MsgGameStart, &start) || start.StartTick != f.room.startTick {
		t.Fatal("GameStart was not resent")
	}
}

func TestLeaveFreesSlotAndResetsToLobby(t *testing.T) {
	f := newFixture(t)
	conn, _ := f.join("A")
	f.send(conn, gnet.MsgSetReady, gnet.SetReady{IsReady: true})
	f.send(conn, gnet.MsgLoadComplete, gnet.LoadComplete{})
	f.send(conn, gnet.MsgLeave, gnet.Leave{})
	if f.room.players[0] != nil || f.room.phase != gnet.PhaseLobby || !conn.isClosed {
		t.Fatalf("phase = %d", f.room.phase)
	}
}

func TestUDPBindAndSnapshot(t *testing.T) {
	f := newFixture(t)
	_, a := f.join("A")
	_, b := f.join("B")

	f.room.HandleUDP(f.udpPacket(a, gnet.MsgUDPBind, 1, nil), f.now)
	if len(f.udp.sent) != 1 || f.udp.sent[0].header.MsgType != gnet.MsgUDPBindAck ||
		f.udp.sent[0].header.Token != a.UDPToken || f.udp.sent[0].header.PlayerID != gnet.ServerPlayerID {
		t.Fatalf("bind ack = %+v", f.udp.sent)
	}

	// token が違うパケットは無視する。
	forged := f.udpPacket(b, gnet.MsgUDPBind, 1, nil)
	forged.Header.Token++
	f.room.HandleUDP(forged, f.now)
	if len(f.udp.sent) != 1 {
		t.Fatal("forged packet was answered")
	}

	state := gnet.PlayerState{Tick: 5, Pos: gnet.Vector3{X: 1, Y: 0, Z: 2}, Yaw: 90}
	f.room.HandleUDP(f.udpPacket(a, gnet.MsgPlayerState, 3, state), f.now)
	// seq が古いパケットは捨てる。
	stale := state
	stale.Pos.X = -100
	f.room.HandleUDP(f.udpPacket(a, gnet.MsgPlayerState, 2, stale), f.now)
	if got := f.room.players[0].State.Pos.X; got != 1 {
		t.Fatalf("stale packet overwrote state: x = %v", got)
	}

	// ゲーム開始前はスナップショットを送らない。
	f.udp.sent = nil
	f.advance(time.Second / 30)
	if len(f.udp.sent) != 0 {
		t.Fatal("snapshot sent in lobby")
	}
	f.room.phase = gnet.PhaseInGame
	f.advance(time.Second / 30)
	if len(f.udp.sent) != 2 || f.udp.sent[0].header.MsgType != gnet.MsgWorldSnapshot ||
		f.udp.sent[1].header.MsgType != gnet.MsgEnemySnapshot {
		t.Fatalf("snapshot packets = %+v", f.udp.sent)
	}
}

func TestSnapshotFitsInOnePacket(t *testing.T) {
	snapshot := gnet.WorldSnapshot{ServerTick: 4294967295}
	for index := 0; index < MaxPlayerSlots; index++ {
		snapshot.Players = append(snapshot.Players, gnet.SnapshotPlayer{PlayerID: index, PlayerState: gnet.PlayerState{
			Tick:      4294967295,
			Pos:       gnet.Vector3{X: -123.456789, Y: -123.456789, Z: -123.456789},
			Yaw:       -359.999999,
			Pitch:     -89.9999999,
			Vel:       gnet.Vector3{X: -123.456789, Y: -123.456789, Z: -123.456789},
			AnimState: 255,
			Flags:     255,
			Health:    100000,
			MaxHealth: 100000,
			WireTip:   gnet.Vector3{X: -123.456789, Y: -123.456789, Z: -123.456789},
		}})
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > gnet.MaxUDPPayloadSize {
		t.Fatalf("snapshot payload %d bytes exceeds %d", len(payload), gnet.MaxUDPPayloadSize)
	}
}

func TestSanitizeName(t *testing.T) {
	if got := sanitizeName("  ", 2); got != "Player3" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeName("あいうえおかきくけこさしすせそたちつ", 0); got != "あいうえおかきくけこさしすせそた" {
		t.Fatalf("got %q", got)
	}
}

// startGame は2人入室させてゲームを開始した状態にする。
func startGame(f *roomFixture) (*fakeConn, gnet.JoinAccepted, *fakeConn, gnet.JoinAccepted) {
	f.t.Helper()
	first, a := f.join("A")
	second, b := f.join("B")
	for _, conn := range []*fakeConn{first, second} {
		f.send(conn, gnet.MsgSetReady, gnet.SetReady{IsReady: true})
	}
	for _, conn := range []*fakeConn{first, second} {
		f.send(conn, gnet.MsgLoadComplete, gnet.LoadComplete{})
	}
	if f.room.phase != gnet.PhaseInGame {
		f.t.Fatal("game did not start")
	}
	return first, a, second, b
}

func countSent(conn *fakeConn, msgType uint16) int {
	count := 0
	for _, frame := range conn.sent {
		if frame.msgType == msgType {
			count++
		}
	}
	return count
}

func TestFireIsRelayedToOthersOnly(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	fire := gnet.Fire{Origin: gnet.Vector3{X: 1, Y: 1.5, Z: 2}, Velocity: gnet.Vector3{Z: 12}}
	f.send(first, gnet.MsgFire, fire)

	var notify gnet.FireNotify
	if !second.last(t, gnet.MsgFireNotify, &notify) {
		t.Fatal("FireNotify was not relayed")
	}
	if notify.PlayerID != 0 || notify.Origin != fire.Origin || notify.Velocity != fire.Velocity {
		t.Fatalf("notify = %+v", notify)
	}
	if countSent(first, gnet.MsgFireNotify) != 0 {
		t.Fatal("FireNotify was echoed back to the shooter")
	}
}

func TestFireIgnoredOutsideGame(t *testing.T) {
	f := newFixture(t)
	first, _ := f.join("A")
	second, _ := f.join("B")
	f.send(first, gnet.MsgFire, gnet.Fire{})
	if countSent(second, gnet.MsgFireNotify) != 0 {
		t.Fatal("FireNotify relayed in lobby")
	}
}

func TestBossDamageAccumulates(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	var state gnet.BossState
	if !second.last(t, gnet.MsgBossState, &state) || state.TotalDamage != 0 {
		t.Fatalf("initial boss state = %+v", state)
	}

	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: 10})
	f.send(second, gnet.MsgBossDamage, gnet.BossDamage{Amount: 25})
	for _, conn := range []*fakeConn{first, second} {
		if !conn.last(t, gnet.MsgBossState, &state) || state.TotalDamage != 35 {
			t.Fatalf("boss state = %+v, want 35", state)
		}
	}

	// 不正な値は数えない。
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: 0})
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: -5})
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: maxBossDamagePerReport + 1})
	if f.room.bossTotalDamage != 35 {
		t.Fatalf("total = %d, want 35", f.room.bossTotalDamage)
	}
}

func TestBossStateResentOnReconnectAndResetAfterGame(t *testing.T) {
	f := newFixture(t)
	first, a, second, _ := startGame(f)
	f.send(second, gnet.MsgBossDamage, gnet.BossDamage{Amount: 40})

	f.room.HandleClosed(first, f.now)
	reconnected := &fakeConn{id: 100}
	f.send(reconnected, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: a.UDPToken})
	var state gnet.BossState
	if !reconnected.last(t, gnet.MsgBossState, &state) || state.TotalDamage != 40 {
		t.Fatalf("boss state after reconnect = %+v", state)
	}

	f.send(reconnected, gnet.MsgLeave, gnet.Leave{})
	f.send(second, gnet.MsgLeave, gnet.Leave{})
	if f.room.phase != gnet.PhaseLobby || f.room.bossTotalDamage != 0 {
		t.Fatalf("phase = %d, total = %d", f.room.phase, f.room.bossTotalDamage)
	}
}

func TestBossDamageIgnoredOutsideGame(t *testing.T) {
	f := newFixture(t)
	conn, _ := f.join("A")
	f.send(conn, gnet.MsgBossDamage, gnet.BossDamage{Amount: 10})
	if f.room.bossTotalDamage != 0 {
		t.Fatal("boss damage counted in lobby")
	}
}
