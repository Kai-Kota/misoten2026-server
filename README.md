# Game Server

4人協力TPSのゲームサーバー（Go）。展示会のローカルネットワークで、ホストPCが起動し、ホスト自身を含む全クライアントが接続する。

## ビルドと起動

```powershell
# Server/bin/ に misoten_server.exe と config.json を出力
./build.ps1

# Unity のビルドの隣に置く（ゲームを -host で起動したときに使われる）
./build.ps1 -OutDir <Unityのビルド先>/Server

# 単体で起動（config.json は実行ファイルの隣 → カレントの順に探す）
./bin/misoten_server.exe
./bin/misoten_server.exe -config path/to/config.json
```

- ログは標準出力と `logs/server_YYYYMMDD_HHMMSS.log`（config.json のあるフォルダ基準）に出る。
- `-parentPid <pid>` を付けると、そのプロセスの終了時にサーバーも止まる。Unity の `-host` 起動が使う。
- 初回起動時に Windows ファイアウォールの許可を求められたら、プライベートネットワークを許可する。

```powershell
go test ./...
```

## 構成

| パス | 内容 |
| --- | --- |
| `cmd/server` | 起動、config.json、ログ |
| `internal/net` | TCPフレーム、UDPヘッダ、メッセージ定義、TCP/UDPサーバー、ServerAnnounce |
| `internal/game` | ゲームループ（Room）とプレイヤー枠 |

ゲーム状態に触るのは `Room.Run` の goroutine 1本だけ。TCP の読み取り（接続ごと）と UDP の受信は別 goroutine で、channel でゲームループへ渡す。TCP の書き込みも接続ごとの goroutine で行う。

## プロトコル

| ポート | 種類 | 用途 |
| --- | --- | --- |
| 7777 | TCP (NoDelay) | 入室・ロビー・開始・終了など落とせないイベント |
| 7778 | UDP | PlayerState（C→S、30Hz）と WorldSnapshot（S→C、30Hz） |
| 7779 | UDP ブロードキャスト | ServerAnnounce（1秒ごと） |

- TCP フレーム: `uint32 length | uint16 msgType | payload`（リトルエンディアン、length は msgType + payload、上限64KB）
- UDP ヘッダ（12バイト、リトルエンディアン）: `uint16 magic(0xB055) | uint8 version(1) | uint8 msgType | uint8 playerId(サーバー発は0xFF) | uint8 reserved | uint16 seq | uint32 token`
  - 1パケット1200バイト以下、再送なし、seq が古いものは捨てる
  - サーバー発のパケットの token は受け取るプレイヤーの udpToken
- payload は UTF-8 の JSON。キー名は `internal/net/messages.go` と Unity の `NetworkMessages.cs` で一致させる。

### TCP

| msgType | 名前 | 向き | payload |
| --- | --- | --- | --- |
| 0x0101 | JoinRequest | C→S | `{version, playerName}` |
| 0x0102 | JoinAccepted | S→C | `{playerId(0-3), udpToken, tickRate, serverTick}` |
| 0x0103 | JoinRejected | S→C | `{reason}` 1=FULL 2=IN_GAME 3=VERSION 4=NOT_FOUND(再接続先なし) |
| 0x0105 | SetReady | C→S | `{isReady}` |
| 0x0106 | LobbyState | S→C | `{phase(0=Lobby 1=Loading 2=InGame), players:[{playerId, playerName, isReady, isConnected}]}` |
| 0x0107 | Leave | C→S | `{}` |
| 0x0108 | Ping/Pong | 双方 | `{clientTimeMs, serverTick, isPong}` クライアントが1秒ごとに送り、サーバーは isPong=true で返す |
| 0x0109 | Reconnect | C→S | `{version, playerId, udpToken}` 成功時は JoinAccepted、段階に応じて LoadStage / GameStart も届く |
| 0x0201 | LoadStage | S→C | `{stageName}` |
| 0x0202 | LoadComplete | C→S | `{}` |
| 0x0203 | GameStart | S→C | `{startTick}` |

### UDP

| msgType | 名前 | 向き | payload |
| --- | --- | --- | --- |
| 0x01 | UdpBind | C→S | なし。UdpBindAck が届くまで200ms間隔で再送 |
| 0x02 | UdpBindAck | S→C | なし |
| 0x10 | PlayerState | C→S | `{tick, pos:{x,y,z}, yaw, pitch, vel:{x,y,z}, animState, flags}` |
| 0x20 | WorldSnapshot | S→C | `{serverTick, players:[{playerId, tick, pos, yaw, pitch, vel, animState, flags}]}` |
| 0x30 | ServerAnnounce | S→全体 | `{serverName, version, tcpPort, udpPort, playerCount, maxPlayers, phase}` |

- `tick` はクライアントが推定したサーバーtick（送信時点）。他プレイヤーの補間はこの tick を時間軸にする。
- `yaw`・`pitch` は度。pitch は Unity と同じく下向きが正。
- `animState`: 0=Idle 1=Move 2=Dead、`flags`: bit0=接地 bit1=死亡

### 流れ

1. TCP 接続 → JoinRequest → JoinAccepted（または JoinRejected）
2. UdpBind を Ack まで再送（サーバーはこのパケットの送信元を UDP の宛先にする）
3. 接続中の全員が SetReady(true) → LoadStage → 全員の LoadComplete → GameStart(startTick = 現在tick + startDelayTicks)
4. ゲーム中は PlayerState と WorldSnapshot を30Hzでやり取りする
5. TCP で3秒何も届かなければ切断扱い。枠は30秒保持され、Reconnect で戻れる。Leave で即座に枠を空ける
