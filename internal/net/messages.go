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
)

// UDP のメッセージ種別（uint8）。
const (
	MsgUDPBind        uint8 = 0x01
	MsgUDPBindAck     uint8 = 0x02
	MsgPlayerState    uint8 = 0x10
	MsgWorldSnapshot  uint8 = 0x20
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
