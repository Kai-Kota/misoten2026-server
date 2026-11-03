package game

import (
	"testing"

	gnet "misoten2026/server/internal/net"
)

func TestWeaponStateIsRelayedToOthersOnly(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	f.send(first, gnet.MsgWeaponState, gnet.WeaponState{StartingWeapon: 3, PickupID: 2})

	var notify gnet.WeaponNotify
	if !second.last(t, gnet.MsgWeaponNotify, &notify) {
		t.Fatal("WeaponNotify was not relayed")
	}
	if notify.PlayerID != 0 || notify.StartingWeapon != 3 || notify.PickupID != 2 {
		t.Fatalf("notify = %+v", notify)
	}
	if countSent(first, gnet.MsgWeaponNotify) != 0 {
		t.Fatal("WeaponNotify was echoed back to the sender")
	}
}

func TestWeaponStateIsAcceptedWhileLoading(t *testing.T) {
	f := newFixture(t)
	first, _ := f.join("A")
	second, _ := f.join("B")
	for _, conn := range []*fakeConn{first, second} {
		f.send(conn, gnet.MsgSetReady, gnet.SetReady{IsReady: true})
	}
	// 開始武器はゲームのシーンに着いた時点（LoadComplete より前）に送られる。
	f.send(first, gnet.MsgWeaponState, gnet.WeaponState{StartingWeapon: 1, PickupID: -1})
	if countSent(second, gnet.MsgWeaponNotify) != 1 {
		t.Fatal("WeaponNotify was not relayed while loading")
	}
}

func TestWeaponStateIgnoredInLobbyOrOutOfRange(t *testing.T) {
	f := newFixture(t)
	first, _ := f.join("A")
	second, _ := f.join("B")
	f.send(first, gnet.MsgWeaponState, gnet.WeaponState{StartingWeapon: 1, PickupID: -1})
	if countSent(second, gnet.MsgWeaponNotify) != 0 {
		t.Fatal("WeaponNotify relayed in lobby")
	}

	f = newFixture(t)
	first, _, second, _ = startGame(f)
	for _, state := range []gnet.WeaponState{{StartingWeapon: -1, PickupID: -1}, {StartingWeapon: 5, PickupID: -1}, {StartingWeapon: 0, PickupID: -2}, {StartingWeapon: 0, PickupID: 64}} {
		f.send(first, gnet.MsgWeaponState, state)
	}
	if countSent(second, gnet.MsgWeaponNotify) != 0 {
		t.Fatal("invalid WeaponState was relayed")
	}
}

func TestWeaponsResentOnReconnectAndClearedOnNextLoad(t *testing.T) {
	f := newFixture(t)
	first, a, second, _ := startGame(f)
	f.send(first, gnet.MsgWeaponState, gnet.WeaponState{StartingWeapon: 2, PickupID: -1})
	f.send(second, gnet.MsgWeaponState, gnet.WeaponState{StartingWeapon: 4, PickupID: 1})

	f.room.HandleClosed(first, f.now)
	reconnected := &fakeConn{id: 100}
	f.send(reconnected, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 0, UDPToken: a.UDPToken})
	var notify gnet.WeaponNotify
	if !reconnected.last(t, gnet.MsgWeaponNotify, &notify) || notify.PlayerID != 1 || notify.StartingWeapon != 4 || notify.PickupID != 1 {
		t.Fatalf("weapon of the other player was not resent: %+v", notify)
	}
	if countSent(reconnected, gnet.MsgWeaponNotify) != 1 {
		t.Fatal("own weapon was sent back on reconnect")
	}

	f.room.clearWeapons()
	for _, player := range f.room.players {
		if player != nil && player.HasWeapon {
			t.Fatalf("weapon of player %d was not cleared", player.ID)
		}
	}
}
