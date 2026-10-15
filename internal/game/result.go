package game

import gnet "misoten2026/server/internal/net"

// checkGameEnd はゲーム中に勝敗が決まったかを調べ、決まったら結果の段階へ進めて全員に GameEnd を送る。
// ボスへの累計ダメージが最大HPに届いたらクリア、接続中の全員が倒れていたらゲームオーバー。
func (r *Room) checkGameEnd() {
	if r.phase != gnet.PhaseInGame {
		return
	}
	switch {
	case r.bossMaxHealth > 0 && r.bossTotalDamage >= r.bossMaxHealth:
		r.endGame(true)
	case r.isEveryoneDead():
		r.endGame(false)
	}
}

// isEveryoneDead は接続中のプレイヤーが1人以上いて、全員が倒れているときに true。
// 蘇生が成立して起き上がりを待っている人は、生きているものとして数える。
func (r *Room) isEveryoneDead() bool {
	count := 0
	for _, player := range r.players {
		if player == nil || !player.IsConnected() {
			continue
		}
		if !player.IsDead() || player.IsRevivePending {
			return false
		}
		count++
	}
	return count > 0
}

func (r *Room) endGame(isCleared bool) {
	r.phase = gnet.PhaseResult
	r.isCleared = isCleared
	r.clearAllRevives()
	r.logger.Info("game end", "isCleared", isCleared, "bossTotalDamage", r.bossTotalDamage, "bossMaxHealth", r.bossMaxHealth)
	r.broadcastTCP(gnet.MsgGameEnd, gnet.GameEnd{IsCleared: isCleared})
	r.broadcastLobby()
}
