package game

import (
	"encoding/json"
	"testing"
	"time"

	gnet "misoten2026/server/internal/net"
)

// lastEnemySnapshot は最後に送った EnemySnapshot を返す。
func lastEnemySnapshot(t *testing.T, f *roomFixture) gnet.EnemySnapshot {
	t.Helper()
	for index := len(f.udp.sent) - 1; index >= 0; index-- {
		if f.udp.sent[index].header.MsgType == gnet.MsgEnemySnapshot {
			var snapshot gnet.EnemySnapshot
			if err := json.Unmarshal(f.udp.sent[index].payload, &snapshot); err != nil {
				t.Fatal(err)
			}
			return snapshot
		}
	}
	t.Fatal("EnemySnapshot was not sent")
	return gnet.EnemySnapshot{}
}

func enemyState(x float32) gnet.EnemyState {
	return gnet.EnemyState{Enemies: []gnet.EnemySample{{EnemyID: 0, Tick: 10, Pos: gnet.Vector3{X: x}}}}
}

func TestEnemyStateIsAcceptedOnlyFromOwner(t *testing.T) {
	f := newFixture(t)
	_, a, _, b := startGame(f)

	// 担当は一番小さい playerId（A）。B から届いた状態は捨てる。
	f.room.HandleUDP(f.udpPacket(b, gnet.MsgEnemyState, 1, enemyState(-5)), f.now)
	f.advance(time.Second / 30)
	snapshot := lastEnemySnapshot(t, f)
	if snapshot.EnemyOwnerID != a.PlayerID || len(snapshot.Enemies) != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	f.room.HandleUDP(f.udpPacket(a, gnet.MsgEnemyState, 1, enemyState(3)), f.now)
	f.advance(time.Second / 30)
	snapshot = lastEnemySnapshot(t, f)
	if len(snapshot.Enemies) != 1 || snapshot.Enemies[0].Pos.X != 3 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestEnemyActionIsRelayedFromOwnerOnly(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	action := gnet.EnemyAction{EnemyID: 0, ActionType: 3, Pos: gnet.Vector3{X: 1}, Yaw: 90}

	f.send(first, gnet.MsgEnemyAction, action)
	if countSent(second, gnet.MsgEnemyAction) != 1 || countSent(first, gnet.MsgEnemyAction) != 0 {
		t.Fatal("owner's action was not relayed to the others only")
	}
	f.send(second, gnet.MsgEnemyAction, action)
	if countSent(first, gnet.MsgEnemyAction) != 0 {
		t.Fatal("non-owner's action was relayed")
	}
}

func TestEnemyOwnerMovesWhenOwnerDisconnects(t *testing.T) {
	f := newFixture(t)
	first, _, _, b := startGame(f)
	f.room.HandleUDP(f.udpPacket(b, gnet.MsgUDPBind, 1, nil), f.now)

	f.room.HandleClosed(first, f.now)
	f.advance(time.Second / 30)
	if snapshot := lastEnemySnapshot(t, f); snapshot.EnemyOwnerID != b.PlayerID {
		t.Fatalf("owner = %d, want %d", snapshot.EnemyOwnerID, b.PlayerID)
	}
	f.room.HandleUDP(f.udpPacket(b, gnet.MsgEnemyState, 2, enemyState(7)), f.now)
	f.advance(time.Second / 30)
	if snapshot := lastEnemySnapshot(t, f); len(snapshot.Enemies) != 1 || snapshot.Enemies[0].Pos.X != 7 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestEnemyStateIsClearedOnGameStart(t *testing.T) {
	f := newFixture(t)
	first, a, second, _ := startGame(f)
	f.room.HandleUDP(f.udpPacket(a, gnet.MsgEnemyState, 1, enemyState(3)), f.now)

	// 全員抜けてロビーに戻り、もう一度始めたら前のゲームの敵の状態は残さない。
	f.send(first, gnet.MsgLeave, gnet.Leave{})
	f.send(second, gnet.MsgLeave, gnet.Leave{})
	startGame(f)
	if f.room.enemies != nil {
		t.Fatalf("enemies = %+v", f.room.enemies)
	}
}

func TestEnemySnapshotFitsInOnePacket(t *testing.T) {
	snapshot := gnet.EnemySnapshot{ServerTick: 4294967295, EnemyOwnerID: MaxPlayerSlots - 1}
	for index := 0; index < MaxEnemies; index++ {
		snapshot.Enemies = append(snapshot.Enemies, gnet.EnemySample{
			EnemyID:    index,
			Tick:       4294967295,
			Pos:        gnet.Vector3{X: -123.456789, Y: -123.456789, Z: -123.456789},
			Yaw:        -359.999999,
			Vel:        gnet.Vector3{X: -123.456789, Y: -123.456789, Z: -123.456789},
			ActionType: 255,
		})
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > gnet.MaxUDPPayloadSize {
		t.Fatalf("enemy snapshot payload %d bytes exceeds %d", len(payload), gnet.MaxUDPPayloadSize)
	}
}
