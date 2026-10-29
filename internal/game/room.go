package game

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	gnet "misoten2026/server/internal/net"
)

// MaxPlayerSlots は playerId に使える枠の数（0〜3）。
const MaxPlayerSlots = 4

const maxPlayerNameLength = 16

// 1回の BossDamage で受け付ける上限（壊れた値や不正な値を弾く）。
const maxBossDamagePerReport = 10000

// RoomConfig はゲームループの設定。
type RoomConfig struct {
	TickRate        int
	MaxPlayers      int
	PingTimeout     time.Duration
	ReconnectWindow time.Duration
	StartDelayTicks int
	StageName       string
	// 蘇生に必要な長押しの時間と、蘇生できる距離（m）。0 なら既定値。
	ReviveDuration time.Duration
	ReviveRadius   float32
	// 村の最大HPと、VillageDamageInterval ごとに減らす量。0 なら既定値。
	VillageMaxHealth      int
	VillageDamage         int
	VillageDamageInterval time.Duration
}

// UDPSender は UDP の送信先。テストで差し替えられるよう interface にしている。
type UDPSender interface {
	SendTo(addr netip.AddrPort, packet []byte) error
}

// Room はロビーとゲームの状態を持つ。状態を読み書きするのは Run の goroutine だけ。
// Handle* と Step はテストから直接呼べるように公開している。
type Room struct {
	config      RoomConfig
	logger      *slog.Logger
	udp         UDPSender
	players     []*Player
	connPlayers map[uint64]*Player
	phase       int
	startTime   time.Time
	startTick   uint32
	// このゲームでボスが受けた累計ダメージと、クライアントが LoadComplete で伝えたボスの最大HP（0 は不明）。
	bossTotalDamage int
	bossMaxHealth   int
	// このゲームでの村の HP。
	villageHealth int
	// 結果の段階で、ボスを倒して終わったか。endReason は終わった理由（gnet.End*）。
	isCleared bool
	endReason int
	// 敵を動かす担当の playerId（-1 は不在）と、担当から届いた最新の敵の状態。
	enemyOwnerID int
	enemies      []gnet.EnemySample

	// Discovery の goroutine から読むための値。
	announcedPlayerCount atomic.Int32
	announcedPhase       atomic.Int32
}

func NewRoom(config RoomConfig, udp UDPSender, logger *slog.Logger, now time.Time) *Room {
	if config.MaxPlayers <= 0 || config.MaxPlayers > MaxPlayerSlots {
		config.MaxPlayers = MaxPlayerSlots
	}
	if config.TickRate <= 0 {
		config.TickRate = 30
	}
	if config.ReviveDuration <= 0 {
		config.ReviveDuration = defaultReviveDuration
	}
	if config.ReviveRadius <= 0 {
		config.ReviveRadius = defaultReviveRadius
	}
	if config.VillageMaxHealth <= 0 {
		config.VillageMaxHealth = defaultVillageMaxHealth
	}
	if config.VillageDamage <= 0 {
		config.VillageDamage = defaultVillageDamage
	}
	if config.VillageDamageInterval <= 0 {
		config.VillageDamageInterval = defaultVillageDamageInterval
	}
	return &Room{
		config:       config,
		logger:       logger.With("component", "room"),
		udp:          udp,
		players:      make([]*Player, config.MaxPlayers),
		connPlayers:  make(map[uint64]*Player),
		phase:        gnet.PhaseLobby,
		startTime:    now,
		enemyOwnerID: -1,
	}
}

// Run はゲームループ。ctx が終わるまで TCP・UDP の受信と tick を1本の goroutine で処理する。
func (r *Room) Run(ctx context.Context, tcpEvents <-chan gnet.TCPEvent, udpPackets <-chan gnet.UDPPacket) {
	ticker := time.NewTicker(time.Second / time.Duration(r.config.TickRate))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-tcpEvents:
			if event.Frame == nil {
				r.HandleClosed(event.Conn, time.Now())
			} else {
				r.HandleFrame(event.Conn, *event.Frame, time.Now())
			}
		case packet := <-udpPackets:
			r.HandleUDP(packet, time.Now())
		case now := <-ticker.C:
			r.Step(now)
		}
	}
}

// Announce は ServerAnnounce に載せる人数と段階を返す。どの goroutine から呼んでもよい。
func (r *Room) Announce() (playerCount, phase int) {
	return int(r.announcedPlayerCount.Load()), int(r.announcedPhase.Load())
}

// TickAt は now 時点のサーバーtick。フレームの遅れで tick がずれないよう経過時間から求める。
func (r *Room) TickAt(now time.Time) uint32 {
	return uint32(now.Sub(r.startTime) * time.Duration(r.config.TickRate) / time.Second)
}

func (r *Room) HandleFrame(conn Conn, frame gnet.Frame, now time.Time) {
	player := r.connPlayers[conn.ID()]
	if player != nil {
		player.LastReceivedAt = now
	}
	switch frame.MsgType {
	case gnet.MsgPing:
		var ping gnet.Ping
		if !r.decode(frame, &ping) {
			return
		}
		ping.IsPong = true
		ping.ServerTick = r.TickAt(now)
		sendJSON(conn, gnet.MsgPing, ping)
	case gnet.MsgJoinRequest:
		var request gnet.JoinRequest
		if player != nil || !r.decode(frame, &request) {
			return
		}
		r.handleJoin(conn, request, now)
	case gnet.MsgReconnect:
		var request gnet.Reconnect
		if player != nil || !r.decode(frame, &request) {
			return
		}
		r.handleReconnect(conn, request, now)
	case gnet.MsgSetReady:
		var request gnet.SetReady
		if player == nil || r.phase != gnet.PhaseLobby || !r.decode(frame, &request) {
			return
		}
		player.IsReady = request.IsReady
		r.logger.Info("ready changed", "player", player.ID, "isReady", player.IsReady)
		r.broadcastLobby()
		r.tryBeginLoading()
	case gnet.MsgLoadComplete:
		var loadComplete gnet.LoadComplete
		if player == nil || r.phase != gnet.PhaseLoading || !r.decode(frame, &loadComplete) {
			return
		}
		player.IsLoaded = true
		// 全員同じステージなので同じ値が届くはずだが、食い違ったら大きい方を使う。
		if loadComplete.BossMaxHealth > r.bossMaxHealth {
			r.bossMaxHealth = loadComplete.BossMaxHealth
		}
		if loadComplete.CharacterID >= 0 && loadComplete.CharacterID < MaxPlayerSlots {
			player.CharacterID = loadComplete.CharacterID
		} else {
			player.CharacterID = -1
		}
		r.logger.Info("load complete", "player", player.ID, "bossMaxHealth", loadComplete.BossMaxHealth, "character", player.CharacterID)
		// 選んだキャラを全員に知らせる（開始前に見た目を合わせられるよう、GameStart より先に送る）。
		r.broadcastLobby()
		r.tryStartGame(now)
	case gnet.MsgFire:
		var fire gnet.Fire
		if player == nil || r.phase != gnet.PhaseInGame || !r.decode(frame, &fire) {
			return
		}
		notify := gnet.FireNotify{PlayerID: player.ID, Origin: fire.Origin, Velocity: fire.Velocity}
		for _, other := range r.players {
			if other != nil && other != player && other.IsConnected() {
				sendJSON(other.Conn, gnet.MsgFireNotify, notify)
			}
		}
	case gnet.MsgBossDamage:
		var damage gnet.BossDamage
		if player == nil || r.phase != gnet.PhaseInGame || !r.decode(frame, &damage) {
			return
		}
		if damage.Amount <= 0 || damage.Amount > maxBossDamagePerReport {
			r.logger.Warn("invalid boss damage", "player", player.ID, "amount", damage.Amount)
			return
		}
		r.bossTotalDamage += damage.Amount
		r.logger.Info("boss damaged", "player", player.ID, "amount", damage.Amount, "total", r.bossTotalDamage)
		r.broadcastTCP(gnet.MsgBossState, gnet.BossState{TotalDamage: r.bossTotalDamage})
		r.checkGameEnd()
	case gnet.MsgReviveStart:
		var request gnet.ReviveStart
		if player == nil || r.phase != gnet.PhaseInGame || !r.decode(frame, &request) {
			return
		}
		r.handleReviveStart(player, request, now)
	case gnet.MsgReviveStop:
		if player == nil {
			return
		}
		r.handleReviveStop(player)
	case gnet.MsgEnemyAction:
		var action gnet.EnemyAction
		if player == nil || r.phase != gnet.PhaseInGame || !r.decode(frame, &action) {
			return
		}
		r.handleEnemyAction(player, action)
	case gnet.MsgLeave:
		if player == nil {
			return
		}
		r.logger.Info("player left", "player", player.ID)
		delete(r.connPlayers, conn.ID())
		r.players[player.ID] = nil
		conn.Close()
		r.afterMembershipChanged(now)
	default:
		r.logger.Warn("unknown tcp message", "msgType", fmt.Sprintf("0x%04X", frame.MsgType), "conn", conn.ID())
	}
	r.updateAnnounce()
}

func (r *Room) handleJoin(conn Conn, request gnet.JoinRequest, now time.Time) {
	reason := 0
	slot := r.findFreeSlot()
	switch {
	case request.Version != int(gnet.ProtocolVersion):
		reason = gnet.RejectVersion
	case r.phase != gnet.PhaseLobby:
		reason = gnet.RejectInGame
	case slot < 0:
		reason = gnet.RejectFull
	}
	if reason != 0 {
		// 切断はクライアント側に任せる（先に閉じると JoinRejected が届かないことがある）。
		r.logger.Info("join rejected", "conn", conn.ID(), "reason", reason, "version", request.Version)
		sendJSON(conn, gnet.MsgJoinRejected, gnet.JoinRejected{Reason: reason})
		return
	}
	player := &Player{
		ID:             slot,
		Name:           sanitizeName(request.PlayerName, slot),
		Token:          r.newToken(),
		Conn:           conn,
		LastReceivedAt: now,
		CharacterID:    -1,
	}
	r.players[slot] = player
	r.connPlayers[conn.ID()] = player
	r.logger.Info("player joined", "player", slot, "name", player.Name, "conn", conn.ID())
	sendJSON(conn, gnet.MsgJoinAccepted, gnet.JoinAccepted{
		PlayerID:   slot,
		UDPToken:   player.Token,
		TickRate:   r.config.TickRate,
		ServerTick: r.TickAt(now),
	})
	r.broadcastLobby()
}

func (r *Room) handleReconnect(conn Conn, request gnet.Reconnect, now time.Time) {
	if request.Version != int(gnet.ProtocolVersion) {
		sendJSON(conn, gnet.MsgJoinRejected, gnet.JoinRejected{Reason: gnet.RejectVersion})
		return
	}
	var player *Player
	if request.PlayerID >= 0 && request.PlayerID < len(r.players) {
		player = r.players[request.PlayerID]
	}
	if player == nil || player.Token != request.UDPToken {
		r.logger.Info("reconnect rejected", "conn", conn.ID(), "player", request.PlayerID)
		sendJSON(conn, gnet.MsgJoinRejected, gnet.JoinRejected{Reason: gnet.RejectNotFound})
		return
	}
	// サーバーがまだ切断に気付いていない場合は、古い接続を捨てて置き換える。
	if player.Conn != nil {
		delete(r.connPlayers, player.Conn.ID())
		player.Conn.Close()
		player.markDisconnected(now)
	}
	player.Conn = conn
	player.LastReceivedAt = now
	r.connPlayers[conn.ID()] = player
	r.logger.Info("player reconnected", "player", player.ID, "conn", conn.ID())
	sendJSON(conn, gnet.MsgJoinAccepted, gnet.JoinAccepted{
		PlayerID:   player.ID,
		UDPToken:   player.Token,
		TickRate:   r.config.TickRate,
		ServerTick: r.TickAt(now),
	})
	r.broadcastLobby()
	switch r.phase {
	case gnet.PhaseLoading:
		sendJSON(conn, gnet.MsgLoadStage, gnet.LoadStage{StageName: r.config.StageName})
	case gnet.PhaseInGame:
		sendJSON(conn, gnet.MsgGameStart, gnet.GameStart{StartTick: r.startTick})
		sendJSON(conn, gnet.MsgBossState, gnet.BossState{TotalDamage: r.bossTotalDamage})
		sendJSON(conn, gnet.MsgVillageState, r.villageState())
	case gnet.PhaseResult:
		sendJSON(conn, gnet.MsgGameEnd, gnet.GameEnd{IsCleared: r.isCleared, Reason: r.endReason})
	}
}

func (r *Room) HandleClosed(conn Conn, now time.Time) {
	player := r.connPlayers[conn.ID()]
	if player == nil {
		return
	}
	delete(r.connPlayers, conn.ID())
	if player.Conn != nil && player.Conn.ID() == conn.ID() {
		r.logger.Info("player disconnected", "player", player.ID)
		player.markDisconnected(now)
		r.afterMembershipChanged(now)
	}
	r.updateAnnounce()
}

func (r *Room) HandleUDP(packet gnet.UDPPacket, now time.Time) {
	header := packet.Header
	if int(header.PlayerID) >= len(r.players) {
		return
	}
	player := r.players[header.PlayerID]
	if player == nil || !player.IsConnected() || player.Token != header.Token {
		return
	}
	if player.HasUDPSeq && !gnet.IsSeqNewer(header.Seq, player.LastUDPSeq) {
		return
	}
	player.HasUDPSeq = true
	player.LastUDPSeq = header.Seq
	if player.UDPAddr != packet.Addr {
		r.logger.Info("udp bound", "player", player.ID, "addr", packet.Addr.String())
		player.UDPAddr = packet.Addr
	}
	switch header.MsgType {
	case gnet.MsgUDPBind:
		r.sendUDP(player, gnet.MsgUDPBindAck, nil)
	case gnet.MsgPlayerState:
		var state gnet.PlayerState
		if err := json.Unmarshal(packet.Payload, &state); err != nil {
			r.logger.Debug("bad player state", "player", player.ID, "err", err)
			return
		}
		player.State = state
		player.HasState = true
		// 蘇生された本人が起き上がったら、次の蘇生を受け付ける。
		if !player.IsDead() {
			player.IsRevivePending = false
		}
	case gnet.MsgEnemyState:
		r.handleEnemyState(player, packet.Payload)
	}
}

// Step は tick ごとの処理。タイムアウトの判定とスナップショットの送信を行う。
func (r *Room) Step(now time.Time) {
	isChanged := false
	for index, player := range r.players {
		if player == nil {
			continue
		}
		if player.IsConnected() && now.Sub(player.LastReceivedAt) > r.config.PingTimeout {
			r.logger.Info("ping timeout", "player", player.ID)
			delete(r.connPlayers, player.Conn.ID())
			player.Conn.Close()
			player.markDisconnected(now)
			isChanged = true
		} else if !player.IsConnected() && now.Sub(player.DisconnectedAt) > r.config.ReconnectWindow {
			r.logger.Info("reconnect window expired", "player", player.ID)
			r.players[index] = nil
			isChanged = true
		}
	}
	if isChanged {
		r.afterMembershipChanged(now)
	}
	if r.phase == gnet.PhaseInGame {
		r.updateRevives(now)
		r.updateEnemyOwner()
		r.updateVillage(now)
		tick := r.TickAt(now)
		r.sendSnapshot(tick)
		r.sendEnemySnapshot(tick)
		r.checkGameEnd()
	}
	r.updateAnnounce()
}

// 参加者が増減したあと、段階を進めたり戻したりする。
func (r *Room) afterMembershipChanged(now time.Time) {
	if r.phase != gnet.PhaseLobby && r.countPlayers(false) == 0 {
		r.logger.Info("room is empty; back to lobby")
		r.phase = gnet.PhaseLobby
		r.bossTotalDamage = 0
		r.bossMaxHealth = 0
		r.clearAllRevives()
		r.clearEnemies()
	}
	r.broadcastLobby()
	r.tryBeginLoading()
	r.tryStartGame(now)
}

// 接続中の全員が Ready になったら LoadStage を送る。
func (r *Room) tryBeginLoading() {
	if r.phase != gnet.PhaseLobby || r.countPlayers(true) == 0 {
		return
	}
	for _, player := range r.players {
		if player != nil && player.IsConnected() && !player.IsReady {
			return
		}
	}
	r.phase = gnet.PhaseLoading
	r.bossMaxHealth = 0
	r.logger.Info("all ready; loading", "stage", r.config.StageName)
	for _, player := range r.players {
		if player != nil && player.IsConnected() {
			player.IsLoaded = false
			sendJSON(player.Conn, gnet.MsgLoadStage, gnet.LoadStage{StageName: r.config.StageName})
		}
	}
	r.broadcastLobby()
}

// 接続中の全員の LoadComplete が揃ったら GameStart を送る。
func (r *Room) tryStartGame(now time.Time) {
	if r.phase != gnet.PhaseLoading || r.countPlayers(true) == 0 {
		return
	}
	for _, player := range r.players {
		if player != nil && player.IsConnected() && !player.IsLoaded {
			return
		}
	}
	r.phase = gnet.PhaseInGame
	r.startTick = r.TickAt(now) + uint32(r.config.StartDelayTicks)
	r.bossTotalDamage = 0
	r.villageHealth = r.config.VillageMaxHealth
	// 前のゲームで倒れていた状態が残っていると、開始直後にゲームオーバーになるので捨てる。
	for _, player := range r.players {
		if player != nil {
			player.HasState = false
		}
	}
	r.clearAllRevives()
	r.clearEnemies()
	r.logger.Info("game start", "startTick", r.startTick)
	r.broadcastTCP(gnet.MsgGameStart, gnet.GameStart{StartTick: r.startTick})
	r.broadcastTCP(gnet.MsgBossState, gnet.BossState{TotalDamage: r.bossTotalDamage})
	r.broadcastTCP(gnet.MsgVillageState, r.villageState())
	r.broadcastLobby()
}

func (r *Room) sendSnapshot(tick uint32) {
	snapshot := gnet.WorldSnapshot{ServerTick: tick, Players: make([]gnet.SnapshotPlayer, 0, len(r.players))}
	for _, player := range r.players {
		if player != nil && player.IsConnected() && player.HasState {
			snapshot.Players = append(snapshot.Players, gnet.SnapshotPlayer{PlayerID: player.ID, PlayerState: player.State})
		}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		r.logger.Error("marshal snapshot", "err", err)
		return
	}
	for _, player := range r.players {
		if player != nil && player.IsConnected() && player.UDPAddr.IsValid() {
			r.sendUDP(player, gnet.MsgWorldSnapshot, payload)
		}
	}
}

func (r *Room) sendUDP(player *Player, msgType uint8, payload []byte) {
	player.SendSeq++
	packet, err := gnet.EncodePacket(gnet.UDPHeader{
		MsgType:  msgType,
		PlayerID: gnet.ServerPlayerID,
		Seq:      player.SendSeq,
		Token:    player.Token,
	}, payload)
	if err != nil {
		r.logger.Error("encode udp packet", "msgType", msgType, "err", err)
		return
	}
	if err := r.udp.SendTo(player.UDPAddr, packet); err != nil {
		r.logger.Debug("udp send failed", "player", player.ID, "err", err)
	}
}

func (r *Room) broadcastLobby() {
	state := gnet.LobbyState{Phase: r.phase, Players: make([]gnet.LobbyPlayer, 0, len(r.players))}
	for _, player := range r.players {
		if player != nil {
			state.Players = append(state.Players, gnet.LobbyPlayer{
				PlayerID:    player.ID,
				PlayerName:  player.Name,
				IsReady:     player.IsReady,
				IsConnected: player.IsConnected(),
				CharacterID: player.CharacterID,
				IsLoaded:    player.IsLoaded,
			})
		}
	}
	r.broadcastTCP(gnet.MsgLobbyState, state)
}

func (r *Room) broadcastTCP(msgType uint16, message any) {
	for _, player := range r.players {
		if player != nil && player.IsConnected() {
			sendJSON(player.Conn, msgType, message)
		}
	}
}

func (r *Room) decode(frame gnet.Frame, target any) bool {
	if err := json.Unmarshal(frame.Payload, target); err != nil {
		r.logger.Warn("bad payload", "msgType", fmt.Sprintf("0x%04X", frame.MsgType), "err", err)
		return false
	}
	return true
}

func (r *Room) findFreeSlot() int {
	for index, player := range r.players {
		if player == nil {
			return index
		}
	}
	return -1
}

func (r *Room) countPlayers(isConnectedOnly bool) int {
	count := 0
	for _, player := range r.players {
		if player != nil && (!isConnectedOnly || player.IsConnected()) {
			count++
		}
	}
	return count
}

func (r *Room) updateAnnounce() {
	r.announcedPlayerCount.Store(int32(r.countPlayers(false)))
	r.announcedPhase.Store(int32(r.phase))
}

// newToken は他の枠と重ならない 0 以外の乱数を返す。
func (r *Room) newToken() uint32 {
	var buffer [4]byte
	for {
		if _, err := rand.Read(buffer[:]); err != nil {
			panic(err)
		}
		token := binary.LittleEndian.Uint32(buffer[:])
		if token == 0 {
			continue
		}
		isUsed := false
		for _, player := range r.players {
			isUsed = isUsed || (player != nil && player.Token == token)
		}
		if !isUsed {
			return token
		}
	}
}

func sanitizeName(name string, slot int) string {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) {
		name = ""
	}
	if utf8.RuneCountInString(name) > maxPlayerNameLength {
		name = string([]rune(name)[:maxPlayerNameLength])
	}
	if name == "" {
		name = fmt.Sprintf("Player%d", slot+1)
	}
	return name
}

func sendJSON(conn Conn, msgType uint16, message any) {
	payload, err := json.Marshal(message)
	if err != nil {
		return
	}
	conn.Send(msgType, payload)
}
