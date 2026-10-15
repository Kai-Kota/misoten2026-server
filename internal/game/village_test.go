package game

import (
	"testing"
	"time"

	gnet "misoten2026/server/internal/net"
)

// startDelay は newFixture の StartDelayTicks（15 tick = 0.5秒）。
const startDelay = time.Second / 2

func villageState(t *testing.T, conn *fakeConn) gnet.VillageState {
	t.Helper()
	var state gnet.VillageState
	if !conn.last(t, gnet.MsgVillageState, &state) {
		t.Fatal("VillageState was not sent")
	}
	return state
}

func newVillageFixture(t *testing.T, maxHealth, damage int, interval time.Duration) *roomFixture {
	f := newFixture(t)
	f.room.config.VillageMaxHealth = maxHealth
	f.room.config.VillageDamage = damage
	f.room.config.VillageDamageInterval = interval
	return f
}

func TestVillageStartsFullAndWaitsForStartTick(t *testing.T) {
	f := newVillageFixture(t, 30, 10, time.Second)
	first, _, _, _ := startGame(f)
	if state := villageState(t, first); state.Health != 30 || state.MaxHealth != 30 {
		t.Fatalf("state = %+v, want full", state)
	}

	// startTick までは減らさない。
	f.advance(startDelay + time.Second - time.Second/30)
	if f.room.villageHealth != 30 {
		t.Fatalf("health = %d before the first interval", f.room.villageHealth)
	}
	sent := countSent(first, gnet.MsgVillageState)

	f.advance(time.Second / 30)
	if state := villageState(t, first); state.Health != 20 {
		t.Fatalf("health = %d, want 20", state.Health)
	}
	// 変わらない tick では送らない。
	f.advance(time.Second / 30)
	if countSent(first, gnet.MsgVillageState) != sent+1 {
		t.Fatal("VillageState was sent without a change")
	}
}

func TestVillageDestroyedIsGameOver(t *testing.T) {
	f := newVillageFixture(t, 20, 10, time.Second)
	first, _, second, _ := startGameWithBoss(f, 100)
	f.advance(startDelay + time.Second)
	if f.room.phase != gnet.PhaseInGame {
		t.Fatal("game ended while the village has health")
	}

	// tick が遅れて2回分まとめて進んでも、経過時間どおりに減る。
	f.advance(time.Second)
	if f.room.phase != gnet.PhaseResult {
		t.Fatalf("phase = %d, want result", f.room.phase)
	}
	for _, conn := range []*fakeConn{first, second} {
		end := gameEnd(t, conn)
		if end.IsCleared || end.Reason != gnet.EndVillageDestroyed {
			t.Fatalf("end = %+v", end)
		}
		if villageState(t, conn).Health != 0 {
			t.Fatal("village health was not 0")
		}
	}

	// 結果の段階では減らさず、GameEnd も重ねて送らない。
	sent := countSent(first, gnet.MsgVillageState)
	f.advance(5 * time.Second)
	if countSent(first, gnet.MsgVillageState) != sent || countSent(first, gnet.MsgGameEnd) != 1 {
		t.Fatal("village kept changing after the result")
	}
}

func TestBossDefeatIsReportedWithReason(t *testing.T) {
	f := newFixture(t)
	first, _, _, _ := startGameWithBoss(f, 100)
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: 100})
	if end := gameEnd(t, first); !end.IsCleared || end.Reason != gnet.EndBossDefeated {
		t.Fatalf("end = %+v", end)
	}
}

func TestVillageResentOnReconnectAndResetOnNextGame(t *testing.T) {
	f := newVillageFixture(t, 30, 10, time.Second)
	first, a, second, _ := startGame(f)
	f.advance(startDelay + time.Second)

	f.room.HandleClosed(first, f.now)
	reconnected := &fakeConn{id: 100}
	f.send(reconnected, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: a.UDPToken})
	if state := villageState(t, reconnected); state.Health != 20 || state.MaxHealth != 30 {
		t.Fatalf("state = %+v on reconnect", state)
	}

	// 全員が抜けてから次のゲームを始めると、満タンから始まる。
	f.send(reconnected, gnet.MsgLeave, gnet.Leave{})
	f.send(second, gnet.MsgLeave, gnet.Leave{})
	next, _, _, _ := startGame(f)
	if state := villageState(t, next); state.Health != 30 {
		t.Fatalf("health = %d on the next game", state.Health)
	}
}
