package game

import (
	"time"

	gnet "misoten2026/server/internal/net"
)

// 蘇生の判定。物理は使わず、各クライアントが送ってくる位置と死亡フラグだけで決める。
//   - ReviveStart: 相手が倒れていて、距離が ReviveRadius 以内なら蘇生を始める
//   - 押している間: tick ごとに距離と状態を確かめ、外れたら中断する
//   - ReviveDuration たったら Revived を全員に送る。本人のクライアントが HP を戻す

const (
	defaultReviveDuration = 3 * time.Second
	// インタラクトの範囲（2m）に、通信の遅れで位置がずれる分の余裕を足した値。
	defaultReviveRadius = 3.0
)

func (r *Room) handleReviveStart(player *Player, request gnet.ReviveStart, now time.Time) {
	if player.IsReviving && player.ReviveTargetID == request.TargetID {
		return
	}
	target := r.playerAt(request.TargetID)
	if !r.canRevive(player, target) {
		r.logger.Debug("revive start ignored", "player", player.ID, "target", request.TargetID)
		return
	}
	player.IsReviving = true
	player.ReviveTargetID = target.ID
	player.ReviveStartedAt = now
	r.logger.Info("revive started", "player", player.ID, "target", target.ID)
}

func (r *Room) handleReviveStop(player *Player) {
	if !player.IsReviving {
		return
	}
	r.logger.Info("revive stopped", "player", player.ID, "target", player.ReviveTargetID)
	player.clearRevive()
}

// updateRevives は tick ごとに進行中の蘇生を確かめ、時間がたったものを成立させる。
func (r *Room) updateRevives(now time.Time) {
	for _, player := range r.players {
		if player == nil || !player.IsReviving {
			continue
		}
		target := r.playerAt(player.ReviveTargetID)
		if !r.canRevive(player, target) {
			r.logger.Info("revive cancelled", "player", player.ID, "target", player.ReviveTargetID)
			player.clearRevive()
			continue
		}
		if now.Sub(player.ReviveStartedAt) < r.config.ReviveDuration {
			continue
		}
		r.logger.Info("revived", "player", player.ID, "target", target.ID)
		target.IsRevivePending = true
		r.broadcastTCP(gnet.MsgRevived, gnet.Revived{ReviverID: player.ID, TargetID: target.ID})
		// 同じ相手を蘇生していた他のプレイヤーも終わらせる。
		for _, other := range r.players {
			if other != nil && other.IsReviving && other.ReviveTargetID == target.ID {
				other.clearRevive()
			}
		}
	}
}

// 蘇生できるのは、ゲーム中に、生きているプレイヤーが、近くで倒れている別のプレイヤーに対してだけ。
// 蘇生が成立して本人の起き上がりを待っている間は、重ねて蘇生しない。
func (r *Room) canRevive(player, target *Player) bool {
	if r.phase != gnet.PhaseInGame || target == nil || target == player {
		return false
	}
	if !player.IsConnected() || !player.HasState || player.IsDead() {
		return false
	}
	if !target.IsConnected() || !target.IsDead() || target.IsRevivePending {
		return false
	}
	return distanceSquared(player.State.Pos, target.State.Pos) <= r.config.ReviveRadius*r.config.ReviveRadius
}

func (r *Room) clearAllRevives() {
	for _, player := range r.players {
		if player != nil {
			player.clearRevive()
			player.IsRevivePending = false
		}
	}
}

func (r *Room) playerAt(id int) *Player {
	if id < 0 || id >= len(r.players) {
		return nil
	}
	return r.players[id]
}

func distanceSquared(a, b gnet.Vector3) float32 {
	x, y, z := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return x*x + y*y + z*z
}
