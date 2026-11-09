# 通信プロトコル

クライアント（Unity）とサーバーの間でやり取りするメッセージの仕様。

| ポート | 種類 | 用途 |
| --- | --- | --- |
| 7777 | TCP (NoDelay) | 入室・ロビー・開始・終了など落とせないイベント |
| 7778 | UDP | PlayerState・EnemyState（C→S、30Hz）と WorldSnapshot・EnemySnapshot（S→C、30Hz） |
| 7779 | UDP ブロードキャスト | ServerAnnounce（1秒ごと） |

- TCP フレーム: `uint32 length | uint16 msgType | payload`（リトルエンディアン、length は msgType + payload、上限64KB）
- UDP ヘッダ（12バイト、リトルエンディアン）: `uint16 magic(0xB055) | uint8 version(8) | uint8 msgType | uint8 playerId(サーバー発は0xFF) | uint8 reserved | uint16 seq | uint32 token`
  - 1パケット1200バイト以下、再送なし、seq が古いものは捨てる
  - サーバー発のパケットの token は受け取るプレイヤーの udpToken
- payload は UTF-8 の JSON。キー名は `internal/net/messages.go` と クライアント側（Unity）の定義 で一致させる。

## TCP

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
| 0x0204 | GameEnd | S→C | `{isCleared, reason}` reason: 1=ボス撃破 2=全員ダウン 3=村の HP が 0。再接続時にも送る |
| 0x0301 | Fire | C→S | `{origin, velocity}` 自分の弾を撃ったとき |
| 0x0302 | FireNotify | S→C | `{playerId, origin, velocity}` 撃った本人以外へ中継 |
| 0x0303 | BossDamage | C→S | `{amount}` 自分の攻撃でボスに与えたダメージ |
| 0x0304 | BossState | S→C | `{totalDamage}` このゲームの累計ダメージ。開始時・変化時・再接続時に送る |
| 0x0305 | VillageState | S→C | `{health, maxHealth}` 村の HP。サーバーが減らす。開始時・変化時・再接続時に送る |
| 0x0401 | ReviveStart | C→S | `{targetId}` 倒れている味方へのインタラクトを押し始めた |
| 0x0402 | ReviveStop | C→S | `{}` インタラクトを離した |
| 0x0403 | Revived | S→C | `{reviverId, targetId}` 蘇生が成立した。全員に送り、targetId 本人のクライアントが HP を戻す |
| 0x0501 | EnemyAction | 双方 | `{enemyId, actionType, pos, yaw, targetIds}` 担当のクライアントで敵が攻撃などを始めた。サーバーは担当以外の全員へ中継する。担当以外から届いたものは捨てる。targetIds は竜巻・爆発が狙った相手の playerId（番号のない相手は -1） |
| 0x0601 | WeaponState | C→S | `{startingWeapon, pickupId}` 自分の武器が変わった。pickupId は合体に使ったフィールド武器の番号（シーン内の位置の順、未合体は -1）。ロード中から受け付ける |
| 0x0602 | WeaponNotify | S→C | `{playerId, startingWeapon, pickupId}` 本人以外へ中継。再接続時は全員分を送り直す |
| 0x0603 | StationReserve | C→S | `{pickupId}` 合体地点で合体を始めた。先に頼んだ1人だけが予約できる。1人が予約できるのは1か所 |
| 0x0604 | StationRelease | C→S | `{pickupId}` 合体をやめた。自分の予約だけを外す（使用済みは戻らない） |
| 0x0605 | StationState | S→C | `{pickupId, state, playerId}` state: 0=未使用 1=予約中 2=使用済み。変化時は全員へ、予約を断ったときは頼んだ人にだけ今の状態を送る。合体（WeaponState の pickupId）で使用済み、切断で予約を外す。再接続時は予約中・使用済みを送り直す |

## UDP

| msgType | 名前 | 向き | payload |
| --- | --- | --- | --- |
| 0x01 | UdpBind | C→S | なし。UdpBindAck が届くまで200ms間隔で再送 |
| 0x02 | UdpBindAck | S→C | なし |
| 0x10 | PlayerState | C→S | `{tick, pos:{x,y,z}, yaw, pitch, vel:{x,y,z}, animState, flags, health, maxHealth, wireTip:{x,y,z}}` |
| 0x11 | EnemyState | C→S | `{enemies:[{enemyId, tick, pos, yaw, vel, actionType}]}` 担当のクライアントだけが送る。担当以外から届いたものは捨てる。最大6体 |
| 0x20 | WorldSnapshot | S→C | `{serverTick, players:[{playerId, tick, pos, yaw, pitch, vel, animState, flags, health, maxHealth, wireTip}]}` |
| 0x21 | EnemySnapshot | S→C | `{serverTick, enemyOwnerId, enemies:[...EnemyState と同じ]}` WorldSnapshot に入れると1パケットに収まらないので分けている。enemyOwnerId が自分なら敵の AI を動かす（-1 は不在） |
| 0x30 | ServerAnnounce | S→全体 | `{serverName, version, tcpPort, udpPort, playerCount, maxPlayers, phase}` |

- `tick` はクライアントが推定したサーバーtick（送信時点）。他プレイヤーの補間はこの tick を時間軸にする。
- `yaw`・`pitch` は度。pitch は Unity と同じく下向きが正。
- `animState`: 0=Idle 1=Move 2=Dead 3=Attack 4=Wire 5=Interact 6=Revive、`flags`: bit0=接地 bit1=死亡
- `wireTip` はワイヤーの先端の位置。`animState` が Wire のときだけ意味を持つ
- HP は各自のクライアントが正（自分の Status を送る）。ボスの HP は、各クライアントが自分の攻撃で減った分を BossDamage で報告し、届いた累計ダメージまで減らす（減らす方向にだけ合わせる）
- 他プレイヤーの弾は表示専用（ダメージ0）。ダメージは撃った本人のクライアントで判定する
- 蘇生はサーバーが判定する。ReviveStart を受けたら、蘇生する側が生きていて、相手の `flags` が死亡で、2人の `pos` の距離が `reviveRadius`（既定3m）以内なら開始する。tick ごとに同じ条件を確かめ、外れたら中断する。`reviveTimeMs`（既定3000）たったら Revived を送る。本人から生きている状態が届くまでは、同じ相手を重ねて蘇生しない
- 村の HP はサーバーが持つ。startTick から `villageDamageIntervalMs`（既定1000）ごとに `villageDamage`（既定1）ずつ、`villageMaxHealth`（既定100）から減らす。経過時間から求めるので tick が遅れてもずれない
- `disableVillage` が true なら村の HP は減らず、村が理由のゲームオーバーにもならない（α の config.json は true）

## 流れ

1. TCP 接続 → JoinRequest → JoinAccepted（または JoinRejected）
2. UdpBind を Ack まで再送（サーバーはこのパケットの送信元を UDP の宛先にする）
3. 接続中の全員が SetReady(true) → LoadStage → 全員の LoadComplete → GameStart(startTick = 現在tick + startDelayTicks)
4. ゲーム中は PlayerState と WorldSnapshot・EnemySnapshot を30Hzでやり取りする。敵の担当は EnemyState も送る
5. TCP で3秒何も届かなければ切断扱い。枠は30秒保持され、Reconnect で戻れる。Leave で即座に枠を空ける

