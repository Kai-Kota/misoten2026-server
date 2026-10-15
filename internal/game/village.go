package game

import (
	"time"

	gnet "misoten2026/server/internal/net"
)

// 村の耐久度の既定値。ボスの攻撃の代わりに、ゲーム開始から一定間隔で減らす。
const (
	defaultVillageMaxHealth      = 100
	defaultVillageDamage         = 1
	defaultVillageDamageInterval = time.Second
)

// updateVillage は startTick からの経過時間で村の HP を決め、変わったら全員に VillageState を送る。
// 経過時間から求めるので、tick が遅れても減る量はずれない。
func (r *Room) updateVillage(now time.Time) {
	elapsed := now.Sub(r.startTime) - time.Duration(r.startTick)*time.Second/time.Duration(r.config.TickRate)
	if elapsed < 0 {
		return
	}
	hitCount := int(elapsed / r.config.VillageDamageInterval)
	health := max(0, r.config.VillageMaxHealth-hitCount*r.config.VillageDamage)
	if health == r.villageHealth {
		return
	}
	r.villageHealth = health
	r.broadcastTCP(gnet.MsgVillageState, r.villageState())
}

func (r *Room) villageState() gnet.VillageState {
	return gnet.VillageState{Health: r.villageHealth, MaxHealth: r.config.VillageMaxHealth}
}
