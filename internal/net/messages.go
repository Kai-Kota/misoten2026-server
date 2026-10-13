package net

// TCP のメッセージ種別（uint16）。
const (
	MsgJoinRequest  uint16 = 0x0101
	MsgJoinAccepted uint16 = 0x0102
	MsgJoinRejected uint16 = 0x0103
	MsgSetReady     uint16 = 0x0105
	MsgLobbyState   uint16 = 0x0106
	MsgLeave        uint16 = 0x0107
	MsgPing         uint16 = 0x0108
	MsgReconnect    uint16 = 0x0109
	MsgLoadStage    uint16 = 0x0201
	MsgLoadComplete uint16 = 0x0202
	MsgGameStart    uint16 = 0x0203
	MsgFire         uint16 = 0x0301
	MsgFireNotify   uint16 = 0x0302
	MsgBossDamage   uint16 = 0x0303
	MsgBossState    uint16 = 0x0304
	MsgReviveStart  uint16 = 0x0401
	MsgReviveStop   uint16 = 0x0402
	MsgRevived      uint16 = 0x0403
	MsgEnemyAction  uint16 = 0x0501
)

// UDP のメッセージ種別（uint8）。
const (
	MsgUDPBind        uint8 = 0x01
	MsgUDPBindAck     uint8 = 0x02
	MsgPlayerState    uint8 = 0x10
	MsgEnemyState     uint8 = 0x11
	MsgWorldSnapshot  uint8 = 0x20
	MsgEnemySnapshot  uint8 = 0x21
	MsgServerAnnounce uint8 = 0x30
)

// JoinRejected の理由。
const (
	RejectFull     = 1
	RejectInGame   = 2
	RejectVersion  = 3
	RejectNotFound = 4 // Reconnect で戻れる枠がない（期限切れ・token不一致など）
)

// ロビーの段階。
const (
	PhaseLobby   = 0
	PhaseLoading = 1
	PhaseInGame  = 2
)

// PlayerState.flags のビット。
const (
	FlagGrounded = 1 << 0
	FlagDead     = 1 << 1
)

// 以下は payload の JSON。フィールド名は Unity 側の JsonUtility と一致させる。

type JoinRequest struct {
	Version    int    `json:"version"`
	PlayerName string `json:"playerName"`
}

type JoinAccepted struct {
	PlayerID   int    `json:"playerId"`
	UDPToken   uint32 `json:"udpToken"`
	TickRate   int    `json:"tickRate"`
	ServerTick uint32 `json:"serverTick"`
}

type JoinRejected struct {
	Reason int `json:"reason"`
}

type SetReady struct {
	IsReady bool `json:"isReady"`
}

type LobbyPlayer struct {
	PlayerID    int    `json:"playerId"`
	PlayerName  string `json:"playerName"`
	IsReady     bool   `json:"isReady"`
	IsConnected bool   `json:"isConnected"`
}

type LobbyState struct {
	Phase   int           `json:"phase"`
	Players []LobbyPlayer `json:"players"`
}

type Leave struct{}

// Ping はクライアントが1秒ごとに送り、サーバーは isPong=true にして返す。
type Ping struct {
	ClientTimeMs int64  `json:"clientTimeMs"`
	ServerTick   uint32 `json:"serverTick"`
	IsPong       bool   `json:"isPong"`
}

type Reconnect struct {
	Version  int    `json:"version"`
	PlayerID int    `json:"playerId"`
	UDPToken uint32 `json:"udpToken"`
}

type LoadStage struct {
	StageName string `json:"stageName"`
}

type LoadComplete struct{}

type GameStart struct {
	StartTick uint32 `json:"startTick"`
}

type Vector3 struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}

// PlayerState はクライアントが30Hzで送る自分の状態。tick はクライアントが推定したサーバーtick。
type PlayerState struct {
	Tick      uint32  `json:"tick"`
	Pos       Vector3 `json:"pos"`
	Yaw       float32 `json:"yaw"`
	Pitch     float32 `json:"pitch"`
	Vel       Vector3 `json:"vel"`
	AnimState int     `json:"animState"`
	Flags     int     `json:"flags"`
	Health    int     `json:"health"`
	MaxHealth int     `json:"maxHealth"`
	// WireTip はワイヤーの先端の位置。animState が Wire のときだけ意味を持つ。
	WireTip Vector3 `json:"wireTip"`
}

// Fire は自分の弾を撃ったときに送る。サーバーは撃った本人以外へ FireNotify として中継する。
type Fire struct {
	Origin   Vector3 `json:"origin"`
	Velocity Vector3 `json:"velocity"`
}

type FireNotify struct {
	PlayerID int     `json:"playerId"`
	Origin   Vector3 `json:"origin"`
	Velocity Vector3 `json:"velocity"`
}

// BossDamage は自分の攻撃でボスに与えたダメージ量。
type BossDamage struct {
	Amount int `json:"amount"`
}

// BossState はこのゲームでボスが受けた累計ダメージ。各クライアントは「最大HP - 累計」までHPを減らす。
type BossState struct {
	TotalDamage int `json:"totalDamage"`
}

// ReviveStart は倒れている味方の蘇生を始めたときに送る（インタラクトを押している間）。
// 成否はサーバーが距離と経過時間で決める。離したら ReviveStop を送る。
type ReviveStart struct {
	TargetID int `json:"targetId"`
}

type ReviveStop struct{}

// Revived は蘇生が成立したことを全員に知らせる。蘇生された本人のクライアントが自分の HP を戻す。
type Revived struct {
	ReviverID int `json:"reviverId"`
	TargetID  int `json:"targetId"`
}

// EnemyState は敵を動かす担当のクライアントが毎 tick 送る、全部の敵の状態。
// 担当はサーバーが決め（EnemySnapshot.enemyOwnerId）、担当以外から届いたものは捨てる。
type EnemyState struct {
	Enemies []EnemySample `json:"enemies"`
}

// EnemySample は敵1体の状態。tick は担当のクライアントが推定したサーバーtick。
type EnemySample struct {
	EnemyID    int     `json:"enemyId"`
	Tick       uint32  `json:"tick"`
	Pos        Vector3 `json:"pos"`
	Yaw        float32 `json:"yaw"`
	Vel        Vector3 `json:"vel"`
	ActionType int     `json:"actionType"`
}

// EnemySnapshot はサーバーが毎 tick 全員へ送る。WorldSnapshot に入れると1パケットに収まらないため分けている。
// enemyOwnerId が自分なら、そのクライアントが敵の AI を動かす。誰もいなければ -1。
type EnemySnapshot struct {
	ServerTick   uint32        `json:"serverTick"`
	EnemyOwnerID int           `json:"enemyOwnerId"`
	Enemies      []EnemySample `json:"enemies"`
}

// EnemyAction は担当のクライアントで敵が攻撃などの行動を始めたときに送る。
// サーバーは担当以外の全員へ中継し、受け取ったクライアントは同じ位置・向きから同じ行動を再生する。
type EnemyAction struct {
	EnemyID    int     `json:"enemyId"`
	ActionType int     `json:"actionType"`
	Pos        Vector3 `json:"pos"`
	Yaw        float32 `json:"yaw"`
}

type SnapshotPlayer struct {
	PlayerID int `json:"playerId"`
	PlayerState
}

type WorldSnapshot struct {
	ServerTick uint32           `json:"serverTick"`
	Players    []SnapshotPlayer `json:"players"`
}

type ServerAnnounce struct {
	ServerName  string `json:"serverName"`
	Version     int    `json:"version"`
	TCPPort     int    `json:"tcpPort"`
	UDPPort     int    `json:"udpPort"`
	PlayerCount int    `json:"playerCount"`
	MaxPlayers  int    `json:"maxPlayers"`
	Phase       int    `json:"phase"`
}
