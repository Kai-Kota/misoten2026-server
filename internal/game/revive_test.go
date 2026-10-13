package game

import (
	"testing"
	"time"

	gnet "misoten2026/server/internal/net"
)

// reviveFixture は2人でゲームを始め、B を倒れた状態にする。A は B のすぐ隣にいる。
type reviveFixture struct {
	*roomFixture
	first, second *fakeConn
	a, b          gnet.JoinAccepted
	seq           uint16
}

func newReviveFixture(t *testing.T) *reviveFixture {
	f := &reviveFixture{roomFixture: newFixture(t)}
	f.first, f.a, f.second, f.b = startGame(f.roomFixture)
	f.setState(f.a, gnet.Vector3{X: 1}, false)
	f.setState(f.b, gnet.Vector3{}, true)
	return f
}

func (f *reviveFixture) setState(accepted gnet.JoinAccepted, pos gnet.Vector3, isDead bool) {
	f.seq++
	state := gnet.PlayerState{Pos: pos}
	if isDead {
		state.Flags = gnet.FlagDead
	}
	f.room.HandleUDP(f.udpPacket(accepted, gnet.MsgPlayerState, f.seq, state), f.now)
}

func TestReviveCompletesAfterDuration(t *testing.T) {
	f := newReviveFixture(t)
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	f.advance(defaultReviveDuration - time.Second/30)
	if countSent(f.second, gnet.MsgRevived) != 0 {
		t.Fatal("revived before the duration")
	}
	f.advance(time.Second / 30)
	for _, conn := range []*fakeConn{f.first, f.second} {
		var revived gnet.Revived
		if !conn.last(t, gnet.MsgRevived, &revived) || revived.ReviverID != f.a.PlayerID || revived.TargetID != f.b.PlayerID {
			t.Fatalf("revived = %+v", revived)
		}
	}
	if f.room.players[0].IsReviving {
		t.Fatal("reviver still reviving")
	}

	// 本人が起き上がるまでは重ねて蘇生しない。
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	if f.room.players[0].IsReviving {
		t.Fatal("revive started while pending")
	}
	f.setState(f.b, gnet.Vector3{}, false)
	if f.room.players[1].IsRevivePending {
		t.Fatal("pending was not cleared after standing up")
	}
}

func TestReviveIgnoredWhenTooFarOrTargetAlive(t *testing.T) {
	f := newReviveFixture(t)
	f.setState(f.a, gnet.Vector3{X: defaultReviveRadius + 0.5}, false)
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	if f.room.players[0].IsReviving {
		t.Fatal("revive started from too far")
	}

	f.setState(f.a, gnet.Vector3{X: 1}, false)
	f.setState(f.b, gnet.Vector3{}, false)
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	if f.room.players[0].IsReviving {
		t.Fatal("revive started on a living player")
	}

	// 自分自身や存在しない枠は対象にならない。
	f.setState(f.b, gnet.Vector3{}, true)
	f.send(f.second, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: 7})
	if f.room.players[0].IsReviving || f.room.players[1].IsReviving {
		t.Fatal("invalid revive started")
	}
}

func TestReviveCancelledByStopOrMovingAway(t *testing.T) {
	f := newReviveFixture(t)
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	f.advance(time.Second)
	f.send(f.first, gnet.MsgReviveStop, gnet.ReviveStop{})
	f.advance(defaultReviveDuration)
	if countSent(f.second, gnet.MsgRevived) != 0 || f.room.players[0].IsReviving {
		t.Fatal("revive continued after stop")
	}

	// 押し直すと最初から数え直す。途中で離れたら中断する。
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	f.advance(time.Second)
	f.setState(f.a, gnet.Vector3{X: 10}, false)
	f.advance(time.Second / 30)
	f.advance(defaultReviveDuration)
	if countSent(f.second, gnet.MsgRevived) != 0 || f.room.players[0].IsReviving {
		t.Fatal("revive continued after moving away")
	}
}

func TestReviveCancelledWhenTargetLeaves(t *testing.T) {
	f := newReviveFixture(t)
	f.send(f.first, gnet.MsgReviveStart, gnet.ReviveStart{TargetID: f.b.PlayerID})
	f.room.HandleClosed(f.second, f.now)
	f.advance(defaultReviveDuration)
	if countSent(f.first, gnet.MsgRevived) != 0 || f.room.players[0].IsReviving {
		t.Fatal("revive continued after the target disconnected")
	}
}
