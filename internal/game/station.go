package game

import (
	gnet "misoten2026/server/internal/net"
)

// stationState は合体地点1か所の状態。地点を使えるのは1人だけで、
// 合体のアニメーション中は予約（StationReserved）、合体したら使用済み（StationUsed）になる。
type stationState struct {
	State    int
	PlayerID int
}

// handleStationReserve は先に頼んだ人にだけ地点を予約させ、全員へ知らせる。
// 既に他の人が予約・使用していたら、頼んだ人にだけ今の状態を返す（クライアントはそれを見て合体をやめる）。
func (r *Room) handleStationReserve(player *Player, request gnet.StationRequest) {
	if !isValidPickupID(request.PickupID) {
		r.logger.Warn("invalid station reserve", "player", player.ID, "pickupId", request.PickupID)
		return
	}
	current, exists := r.stations[request.PickupID]
	if exists && (current.State == gnet.StationUsed || current.PlayerID != player.ID) {
		r.logger.Info("station reserve denied", "player", player.ID, "pickupId", request.PickupID, "state", current.State, "owner", current.PlayerID)
		sendJSON(player.Conn, gnet.MsgStationState, stationNotify(request.PickupID, current))
		return
	}
	// 1人が予約できるのは1か所だけ。別の地点へ移ったら前の予約を外す。
	for pickupID, other := range r.stations {
		if pickupID != request.PickupID && other.State == gnet.StationReserved && other.PlayerID == player.ID {
			r.setStation(pickupID, stationState{State: gnet.StationFree, PlayerID: -1})
		}
	}
	r.logger.Info("station reserved", "player", player.ID, "pickupId", request.PickupID)
	r.setStation(request.PickupID, stationState{State: gnet.StationReserved, PlayerID: player.ID})
}

// handleStationRelease は合体をやめた人の予約を外す。使用済みの地点や他の人の予約は変えない。
func (r *Room) handleStationRelease(player *Player, request gnet.StationRequest) {
	current, exists := r.stations[request.PickupID]
	if !exists || current.State != gnet.StationReserved || current.PlayerID != player.ID {
		return
	}
	r.logger.Info("station released", "player", player.ID, "pickupId", request.PickupID)
	r.setStation(request.PickupID, stationState{State: gnet.StationFree, PlayerID: -1})
}

// markStationUsed は合体した（WeaponState に pickupId が入った）地点を使用済みにする。
func (r *Room) markStationUsed(player *Player, pickupID int) {
	if !isValidPickupID(pickupID) {
		return
	}
	current, exists := r.stations[pickupID]
	if exists && current.State == gnet.StationUsed {
		if current.PlayerID != player.ID {
			r.logger.Warn("station used twice", "pickupId", pickupID, "first", current.PlayerID, "second", player.ID)
		}
		return
	}
	r.setStation(pickupID, stationState{State: gnet.StationUsed, PlayerID: player.ID})
}

// releaseStationsOf は切断した人の予約を外す。使用済みはそのまま残す。
func (r *Room) releaseStationsOf(playerID int) {
	for pickupID, current := range r.stations {
		if current.State == gnet.StationReserved && current.PlayerID == playerID {
			r.setStation(pickupID, stationState{State: gnet.StationFree, PlayerID: -1})
		}
	}
}

// sendStations は予約中・使用済みの地点をすべて conn へ送る。再接続した人が途中の変更を取りこぼさないようにする。
func (r *Room) sendStations(conn Conn) {
	for pickupID, current := range r.stations {
		sendJSON(conn, gnet.MsgStationState, stationNotify(pickupID, current))
	}
}

// clearStations は新しいゲームのロードを始めるときに、前のゲームの地点の状態を忘れる。
func (r *Room) clearStations() {
	r.stations = make(map[int]stationState)
}

func (r *Room) setStation(pickupID int, state stationState) {
	if r.stations == nil {
		r.stations = make(map[int]stationState)
	}
	if state.State == gnet.StationFree {
		delete(r.stations, pickupID)
	} else {
		r.stations[pickupID] = state
	}
	r.broadcastTCP(gnet.MsgStationState, stationNotify(pickupID, state))
}

func stationNotify(pickupID int, state stationState) gnet.StationState {
	return gnet.StationState{PickupID: pickupID, State: state.State, PlayerID: state.PlayerID}
}

func isValidPickupID(pickupID int) bool {
	return pickupID >= 0 && pickupID <= maxPickupID
}
