package game

import (
	gnet "misoten2026/server/internal/net"
)

// 開始武器の種類の数と、フィールド武器の番号の上限。範囲外の WeaponState は捨てる。
const (
	weaponTypeCount = 5
	maxPickupID     = 63
)

// handleWeaponState は武器が変わったプレイヤーの状態を覚え、本人以外へ中継する。
// 開始武器はゲームのシーンに着いた時点で決まるので、ロード中から受け付ける。
func (r *Room) handleWeaponState(player *Player, state gnet.WeaponState) {
	if state.StartingWeapon < 0 || state.StartingWeapon >= weaponTypeCount || state.PickupID < -1 || state.PickupID > maxPickupID {
		r.logger.Warn("invalid weapon state", "player", player.ID, "startingWeapon", state.StartingWeapon, "pickupId", state.PickupID)
		return
	}
	player.Weapon = state
	player.HasWeapon = true
	r.logger.Info("weapon changed", "player", player.ID, "startingWeapon", state.StartingWeapon, "pickupId", state.PickupID)
	notify := weaponNotify(player)
	for _, other := range r.players {
		if other != nil && other != player && other.IsConnected() {
			sendJSON(other.Conn, gnet.MsgWeaponNotify, notify)
		}
	}
}

// sendWeapons は本人以外の全員の武器を conn へ送る。再接続した人が途中の変更を取りこぼさないようにする。
func (r *Room) sendWeapons(conn Conn, self *Player) {
	for _, other := range r.players {
		if other != nil && other != self && other.HasWeapon {
			sendJSON(conn, gnet.MsgWeaponNotify, weaponNotify(other))
		}
	}
}

// clearWeapons は新しいゲームのロードを始めるときに、前のゲームの武器を忘れる。
func (r *Room) clearWeapons() {
	for _, player := range r.players {
		if player != nil {
			player.Weapon = gnet.WeaponState{}
			player.HasWeapon = false
		}
	}
}

func weaponNotify(player *Player) gnet.WeaponNotify {
	return gnet.WeaponNotify{PlayerID: player.ID, StartingWeapon: player.Weapon.StartingWeapon, PickupID: player.Weapon.PickupID}
}
