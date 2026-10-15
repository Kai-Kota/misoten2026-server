package game

import (
	"testing"
	"time"

	gnet "misoten2026/server/internal/net"
)

// startGameWithBoss は2人入室させ、ボスの最大HPを伝えてゲームを開始した状態にする。
func startGameWithBoss(f *roomFixture, bossMaxHealth int) (*fakeConn, gnet.JoinAccepted, *fakeConn, gnet.JoinAccepted) {
	f.t.Helper()
	first, a := f.join("A")
	second, b := f.join("B")
	for _, conn := range []*fakeConn{first, second} {
		f.send(conn, gnet.MsgSetReady, gnet.SetReady{IsReady: true})
	}
	for _, conn := range []*fakeConn{first, second} {
		f.send(conn, gnet.MsgLoadComplete, gnet.LoadComplete{BossMaxHealth: bossMaxHealth})
	}
	if f.room.phase != gnet.PhaseInGame {
		f.t.Fatal("game did not start")
	}
	return first, a, second, b
}

func gameEnd(t *testing.T, conn *fakeConn) gnet.GameEnd {
	t.Helper()
	var end gnet.GameEnd
	if !conn.last(t, gnet.MsgGameEnd, &end) {
		t.Fatal("GameEnd was not sent")
	}
	return end
}

func TestBossDefeatedClearsGame(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGameWithBoss(f, 100)
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: 60})
	if f.room.phase != gnet.PhaseInGame || countSent(first, gnet.MsgGameEnd) != 0 {
		t.Fatal("game ended before the boss was defeated")
	}

	f.send(second, gnet.MsgBossDamage, gnet.BossDamage{Amount: 40})
	if f.room.phase != gnet.PhaseResult {
		t.Fatalf("phase = %d, want result", f.room.phase)
	}
	for _, conn := range []*fakeConn{first, second} {
		if !gameEnd(t, conn).IsCleared {
			t.Fatal("isCleared = false")
		}
	}

	// 結果の段階ではダメージを数えず、GameEnd も重ねて送らない。
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: 10})
	f.advance(time.Second)
	if f.room.bossTotalDamage != 100 || countSent(first, gnet.MsgGameEnd) != 1 {
		t.Fatalf("total = %d, GameEnd sent %d times", f.room.bossTotalDamage, countSent(first, gnet.MsgGameEnd))
	}
}

func TestBossWithoutMaxHealthNeverClears(t *testing.T) {
	f := newFixture(t)
	first, _, _, _ := startGame(f)
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: 5000})
	if f.room.phase != gnet.PhaseInGame {
		t.Fatal("game ended without knowing the boss max health")
	}
}

func TestEveryoneDeadIsGameOver(t *testing.T) {
	f := &reviveFixture{roomFixture: newFixture(t)}
	f.first, f.a, f.second, f.b = startGameWithBoss(f.roomFixture, 100)
	f.setState(f.a, gnet.Vector3{}, true)
	f.advance(time.Second / 30)
	if f.room.phase != gnet.PhaseInGame {
		t.Fatal("game ended while B is alive")
	}

	f.setState(f.b, gnet.Vector3{}, true)
	f.advance(time.Second / 30)
	if f.room.phase != gnet.PhaseResult {
		t.Fatalf("phase = %d, want result", f.room.phase)
	}
	if gameEnd(t, f.second).IsCleared {
		t.Fatal("isCleared = true")
	}
}

func TestDisconnectedPlayerIsIgnoredForGameOver(t *testing.T) {
	f := &reviveFixture{roomFixture: newFixture(t)}
	f.first, f.a, f.second, f.b = startGameWithBoss(f.roomFixture, 100)
	f.setState(f.b, gnet.Vector3{}, false)
	f.room.HandleClosed(f.second, f.now)
	f.setState(f.a, gnet.Vector3{}, true)
	f.advance(time.Second / 30)
	if f.room.phase != gnet.PhaseResult || gameEnd(t, f.first).IsCleared {
		t.Fatal("game did not end when every connected player was dead")
	}
}

func TestRevivePendingCountsAsAlive(t *testing.T) {
	f := newReviveFixture(t)
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	f.advance(defaultReviveDuration)
	if countSent(f.second, gnet.MsgRevived) == 0 {
		t.Fatal("revive did not complete")
	}
	// 蘇生した A が倒れても、B は起き上がる途中なので終わらない。
	f.setState(f.a, gnet.Vector3{X: 1}, true)
	// Ping のタイムアウトで切断されないよう、両方から受信したことにしておく。
	f.send(f.first, gnet.MsgPing, gnet.Ping{})
	f.send(f.second, gnet.MsgPing, gnet.Ping{})
	f.advance(time.Second / 30)
	if f.room.phase != gnet.PhaseInGame {
		t.Fatal("game ended while B was being revived")
	}
	// 起き上がり待ちが外れれば、全員が倒れているので終わる。
	f.room.players[1].IsRevivePending = false
	f.advance(time.Second / 30)
	if f.room.phase != gnet.PhaseResult {
		t.Fatal("game did not end after the pending revive was cleared")
	}
}

func TestResultResentOnReconnectAndResetWhenEmpty(t *testing.T) {
	f := newFixture(t)
	first, a, second, _ := startGameWithBoss(f, 50)
	f.send(first, gnet.MsgBossDamage, gnet.BossDamage{Amount: 50})

	f.room.HandleClosed(first, f.now)
	reconnected := &fakeConn{id: 100}
	f.send(reconnected, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: a.UDPToken})
	if !gameEnd(t, reconnected).IsCleared {
		t.Fatal("GameEnd was not resent on reconnect")
	}

	// 結果の段階では新しく入室できない。
	late := &fakeConn{id: 200}
	f.send(late, gnet.MsgJoinRequest, gnet.JoinRequest{Version: int(gnet.ProtocolVersion), PlayerName: "C"})
	if rejectReason(t, late) != gnet.RejectInGame {
		t.Fatal("join was not rejected during result")
	}

	f.send(reconnected, gnet.MsgLeave, gnet.Leave{})
	f.send(second, gnet.MsgLeave, gnet.Leave{})
	if f.room.phase != gnet.PhaseLobby || f.room.bossTotalDamage != 0 || f.room.bossMaxHealth != 0 {
		t.Fatalf("phase = %d, total = %d, max = %d", f.room.phase, f.room.bossTotalDamage, f.room.bossMaxHealth)
	}
}
