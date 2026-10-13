// Command server は4人協力TPSのゲームサーバー。
// ホストPCで起動し、ホスト自身を含む全クライアントが TCP/UDP で接続する。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"misoten2026/server/internal/game"
	gnet "misoten2026/server/internal/net"
)

type Config struct {
	ServerName         string  `json:"serverName"`
	TCPPort            int     `json:"tcpPort"`
	UDPPort            int     `json:"udpPort"`
	DiscoveryPort      int     `json:"discoveryPort"`
	AnnounceIntervalMs int     `json:"announceIntervalMs"`
	TickRate           int     `json:"tickRate"`
	MaxPlayers         int     `json:"maxPlayers"`
	PingTimeoutMs      int     `json:"pingTimeoutMs"`
	ReconnectWindowSec int     `json:"reconnectWindowSec"`
	StartDelayTicks    int     `json:"startDelayTicks"`
	StageName          string  `json:"stageName"`
	ReviveTimeMs       int     `json:"reviveTimeMs"`
	ReviveRadius       float32 `json:"reviveRadius"`
	LogDir             string  `json:"logDir"`
	LogLevel           string  `json:"logLevel"`
}

func defaultConfig() Config {
	return Config{
		ServerName:         "misoten2026",
		TCPPort:            7777,
		UDPPort:            7778,
		DiscoveryPort:      7779,
		AnnounceIntervalMs: 1000,
		TickRate:           30,
		MaxPlayers:         4,
		PingTimeoutMs:      3000,
		ReconnectWindowSec: 30,
		StartDelayTicks:    15,
		StageName:          "Kai_NetworkTestScene",
		ReviveTimeMs:       3000,
		ReviveRadius:       3,
		LogDir:             "logs",
		LogLevel:           "info",
	}
}

func main() {
	configPath := flag.String("config", "", "config.json のパス（省略時は実行ファイルの隣、次にカレント）")
	parentPID := flag.Int("parentPid", 0, "このプロセスが終了したらサーバーも終了する（Unity のホストから起動する場合に使う）")
	flag.Parse()

	if err := run(*configPath, *parentPID); err != nil {
		fmt.Fprintln(os.Stderr, "server:", err)
		os.Exit(1)
	}
}

func run(configPath string, parentPID int) error {
	configPath = resolveConfigPath(configPath)
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	logger, closeLog, err := newLogger(config, filepath.Dir(configPath))
	if err != nil {
		return err
	}
	defer closeLog()
	logger.Info("server starting", "config", configPath, "tcp", config.TCPPort, "udp", config.UDPPort,
		"discovery", config.DiscoveryPort, "tickRate", config.TickRate)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if parentPID > 0 {
		ctx = watchParent(ctx, parentPID, logger)
	}

	tcpServer, err := gnet.ListenTCP(config.TCPPort, logger)
	if err != nil {
		return fmt.Errorf("listen tcp: %w", err)
	}
	udpServer, err := gnet.ListenUDP(config.UDPPort, logger)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}

	room := game.NewRoom(game.RoomConfig{
		TickRate:        config.TickRate,
		MaxPlayers:      config.MaxPlayers,
		PingTimeout:     time.Duration(config.PingTimeoutMs) * time.Millisecond,
		ReconnectWindow: time.Duration(config.ReconnectWindowSec) * time.Second,
		StartDelayTicks: config.StartDelayTicks,
		StageName:       config.StageName,
		ReviveDuration:  time.Duration(config.ReviveTimeMs) * time.Millisecond,
		ReviveRadius:    config.ReviveRadius,
	}, udpServer, logger, time.Now())

	tcpEvents := make(chan gnet.TCPEvent, 256)
	udpPackets := make(chan gnet.UDPPacket, 256)
	go tcpServer.Serve(ctx, tcpEvents)
	go udpServer.Serve(ctx, udpPackets)
	go gnet.RunDiscovery(ctx, config.DiscoveryPort, time.Duration(config.AnnounceIntervalMs)*time.Millisecond,
		func() gnet.ServerAnnounce {
			playerCount, phase := room.Announce()
			return gnet.ServerAnnounce{
				ServerName:  config.ServerName,
				Version:     int(gnet.ProtocolVersion),
				TCPPort:     config.TCPPort,
				UDPPort:     config.UDPPort,
				PlayerCount: playerCount,
				MaxPlayers:  config.MaxPlayers,
				Phase:       phase,
			}
		}, logger)

	room.Run(ctx, tcpEvents, udpPackets)
	logger.Info("server stopped")
	return nil
}

func resolveConfigPath(path string) string {
	if path != "" {
		return path
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "config.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "config.json"
}

// loadConfig はファイルにない項目を既定値のままにする。
func loadConfig(path string) (Config, error) {
	config := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("parse config %s: %w", path, err)
	}
	return config, nil
}

// newLogger は標準出力と logs/ 以下のファイルの両方に出すロガーを作る。
func newLogger(config Config, baseDir string) (*slog.Logger, func(), error) {
	logDir := config.LogDir
	if !filepath.IsAbs(logDir) {
		logDir = filepath.Join(baseDir, logDir)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create log dir: %w", err)
	}
	fileName := "server_" + time.Now().Format("20060102_150405") + ".log"
	file, err := os.OpenFile(filepath.Join(logDir, fileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToUpper(config.LogLevel))); err != nil {
		level = slog.LevelInfo
	}
	handler := slog.NewTextHandler(io.MultiWriter(os.Stdout, file), &slog.HandlerOptions{Level: level})
	return slog.New(handler), func() { _ = file.Close() }, nil
}

// watchParent は親プロセス（ホストの Unity）が落ちたときにサーバーを止める。
func watchParent(ctx context.Context, pid int, logger *slog.Logger) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	process, err := os.FindProcess(pid)
	if err != nil {
		logger.Warn("parent process not found; stopping", "pid", pid, "err", err)
		cancel()
		return ctx
	}
	go func() {
		_, _ = process.Wait()
		logger.Info("parent process exited", "pid", pid)
		cancel()
	}()
	return ctx
}
