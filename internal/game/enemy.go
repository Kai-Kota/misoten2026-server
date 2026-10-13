package game

import (
	"encoding/json"

	gnet "misoten2026/server/internal/net"
)

// 敵の同期。敵の AI は Unity の NavMesh に頼るのでサーバーでは動かさず、
// 接続中で一番小さい playerId のクライアント（担当）に任せる。
// サーバーは担当から届いた状態と行動を全員へ配り、担当以外から届いたものは捨てる。

// MaxEnemies は1回の EnemyState で受け付ける敵の数の上限（EnemySnapshot が1パケットに収まる数）。
const MaxEnemies = 6

// currentEnemyOwner は敵を動かす担当の playerId。誰も接続していなければ -1。
func (r *Room) currentEnemyOwner() int {
	for _, player := range r.players {
		if player != nil && player.IsConnected() {
			return player.ID
		}
	}
	return -1
}

func (r *Room) handleEnemyState(player *Player, payload []byte) {
	if r.phase != gnet.PhaseInGame || player.ID != r.currentEnemyOwner() {
		return
	}
	var state gnet.EnemyState
	if err := json.Unmarshal(payload, &state); err != nil {
		r.logger.Debug("bad enemy state", "player", player.ID, "err", err)
		return
	}
	if len(state.Enemies) > MaxEnemies {
		r.logger.Warn("too many enemies", "player", player.ID, "count", len(state.Enemies))
		return
	}
	r.enemies = state.Enemies
}

func (r *Room) handleEnemyAction(player *Player, action gnet.EnemyAction) {
	if player.ID != r.currentEnemyOwner() {
		return
	}
	for _, other := range r.players {
		if other != nil && other != player && other.IsConnected() {
			sendJSON(other.Conn, gnet.MsgEnemyAction, action)
		}
	}
}

// 担当が変わったら記録する。新しい担当のクライアントは、今の敵の位置から AI を動かし始める。
func (r *Room) updateEnemyOwner() {
	owner := r.currentEnemyOwner()
	if owner == r.enemyOwnerID {
		return
	}
	r.logger.Info("enemy owner changed", "from", r.enemyOwnerID, "to", owner)
	r.enemyOwnerID = owner
}

func (r *Room) sendEnemySnapshot(tick uint32) {
	snapshot := gnet.EnemySnapshot{ServerTick: tick, EnemyOwnerID: r.enemyOwnerID, Enemies: r.enemies}
	if snapshot.Enemies == nil {
		snapshot.Enemies = []gnet.EnemySample{}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		r.logger.Error("marshal enemy snapshot", "err", err)
		return
	}
	for _, player := range r.players {
		if player != nil && player.IsConnected() && player.UDPAddr.IsValid() {
			r.sendUDP(player, gnet.MsgEnemySnapshot, payload)
		}
	}
}

func (r *Room) clearEnemies() {
	r.enemies = nil
}
