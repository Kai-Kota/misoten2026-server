package game

import (
	"testing"

	gnet "misoten2026/server/internal/net"
)

func lastStation(t *testing.T, conn *fakeConn) gnet.StationState {
	t.Helper()
	var state gnet.StationState
	if !conn.last(t, gnet.MsgStationState, &state) {
		t.Fatal("StationState was not sent")
	}
	return state
}

func TestStationReserveIsSharedWithEveryone(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 2})
	for _, conn := range []*fakeConn{first, second} {
		if state := lastStation(t, conn); state.PickupID != 2 || state.State != gnet.StationReserved || state.PlayerID != 0 {
			t.Fatalf("state = %+v", state)
		}
	}
}

func TestStationReservedByOtherIsDenied(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 2})
	sentToFirst := countSent(first, gnet.MsgStationState)
	f.send(second, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 2})
	// 断った相手にだけ、今の持ち主を返す。
	if state := lastStation(t, second); state.State != gnet.StationReserved || state.PlayerID != 0 {
		t.Fatalf("denied state = %+v", state)
	}
	if countSent(first, gnet.MsgStationState) != sentToFirst {
		t.Fatal("the denial was broadcast")
	}
}

func TestStationReleaseFreesOnlyOwnReservation(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 1})
	f.send(second, gnet.MsgStationRelease, gnet.StationRequest{PickupID: 1})
	if f.room.stations[1].PlayerID != 0 {
		t.Fatal("another player's reservation was released")
	}
	f.send(first, gnet.MsgStationRelease, gnet.StationRequest{PickupID: 1})
	if state := lastStation(t, second); state.PickupID != 1 || state.State != gnet.StationFree || state.PlayerID != -1 {
		t.Fatalf("state = %+v", state)
	}
	f.send(second, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 1})
	if state := lastStation(t, first); state.State != gnet.StationReserved || state.PlayerID != 1 {
		t.Fatalf("freed station could not be reserved: %+v", state)
	}
}

func TestReservingAnotherStationReleasesThePrevious(t *testing.T) {
	f := newFixture(t)
	first, _, _, _ := startGame(f)
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 1})
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 3})
	if _, exists := f.room.stations[1]; exists {
		t.Fatal("the previous reservation was kept")
	}
	if f.room.stations[3].State != gnet.StationReserved {
		t.Fatal("the new station was not reserved")
	}
}

func TestCombinedStationBecomesUsedAndCannotBeReserved(t *testing.T) {
	f := newFixture(t)
	first, _, second, _ := startGame(f)
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 4})
	f.send(first, gnet.MsgWeaponState, gnet.WeaponState{StartingWeapon: 0, PickupID: 4})
	if state := lastStation(t, second); state.PickupID != 4 || state.State != gnet.StationUsed || state.PlayerID != 0 {
		t.Fatalf("state = %+v", state)
	}
	// 使用済みは本人が予約をやめても戻らない。
	f.send(first, gnet.MsgStationRelease, gnet.StationRequest{PickupID: 4})
	f.send(second, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 4})
	if state := lastStation(t, second); state.State != gnet.StationUsed || state.PlayerID != 0 {
		t.Fatalf("used station was reserved: %+v", state)
	}
}

func TestStationReservationReleasedOnDisconnectAndResentOnReconnect(t *testing.T) {
	f := newFixture(t)
	first, _, second, b := startGame(f)
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 1})
	f.send(second, gnet.MsgWeaponState, gnet.WeaponState{StartingWeapon: 0, PickupID: 2})

	f.room.HandleClosed(first, f.now)
	if _, exists := f.room.stations[1]; exists {
		t.Fatal("the reservation of the disconnected player was kept")
	}

	f.room.HandleClosed(second, f.now)
	reconnected := &fakeConn{id: 100}
	f.send(reconnected, gnet.MsgReconnect, gnet.Reconnect{Version: int(gnet.ProtocolVersion), PlayerID: 1, UDPToken: b.UDPToken})
	if state := lastStation(t, reconnected); state.PickupID != 2 || state.State != gnet.StationUsed || state.PlayerID != 1 {
		t.Fatalf("used station was not resent: %+v", state)
	}
}

func TestStationRequestsIgnoredOutsideGameOrOutOfRange(t *testing.T) {
	f := newFixture(t)
	first, _ := f.join("A")
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 1})
	if countSent(first, gnet.MsgStationState) != 0 {
		t.Fatal("reserve accepted in lobby")
	}

	f = newFixture(t)
	first, _, _, _ = startGame(f)
	for _, pickupID := range []int{-1, maxPickupID + 1} {
		f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: pickupID})
	}
	if countSent(first, gnet.MsgStationState) != 0 {
		t.Fatal("out-of-range reserve accepted")
	}

	f.room.clearStations()
	f.send(first, gnet.MsgStationReserve, gnet.StationRequest{PickupID: 0})
	f.room.clearStations()
	if len(f.room.stations) != 0 {
		t.Fatal("stations were not cleared")
	}
}
