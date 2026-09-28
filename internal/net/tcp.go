package net

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	sendQueueSize   = 64
	writeTimeout    = 2 * time.Second
	readIdleTimeout = 10 * time.Second
)

// TCPEvent は受信 goroutine からゲームループへ渡すイベント。
// Frame が nil のときは切断を表す。
type TCPEvent struct {
	Conn  *TCPConn
	Frame *Frame
}

// TCPConn は1接続。読み取りと書き込みはそれぞれ専用の goroutine で行う。
type TCPConn struct {
	id        uint64
	conn      *net.TCPConn
	sendQueue chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	logger    *slog.Logger
}

var nextConnID atomic.Uint64

func (c *TCPConn) ID() uint64 { return c.id }

func (c *TCPConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// Send はフレームを書き込みキューに積む。キューが詰まっている相手は切断する。
// どの goroutine から呼んでもよい。
func (c *TCPConn) Send(msgType uint16, payload []byte) bool {
	encoded, err := EncodeFrame(msgType, payload)
	if err != nil {
		c.logger.Error("encode frame", "msgType", msgType, "err", err)
		return false
	}
	select {
	case <-c.closed:
		return false
	default:
	}
	select {
	case c.sendQueue <- encoded:
		return true
	case <-c.closed:
		return false
	default:
		c.logger.Warn("send queue full; closing")
		c.Close()
		return false
	}
}

// Close は接続を閉じる。何度呼んでもよい。
func (c *TCPConn) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
}

func (c *TCPConn) readLoop(ctx context.Context, events chan<- TCPEvent) {
	defer func() {
		c.Close()
		select {
		case events <- TCPEvent{Conn: c}:
		case <-ctx.Done():
		}
	}()
	for {
		// Ping は1秒ごとに届くので、長く無音の接続（入室しないままの接続など）は閉じる。
		_ = c.conn.SetReadDeadline(time.Now().Add(readIdleTimeout))
		frame, err := ReadFrame(c.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				c.logger.Info("read ended", "err", err)
			}
			return
		}
		select {
		case events <- TCPEvent{Conn: c, Frame: &frame}:
		case <-c.closed:
			return
		}
	}
}

func (c *TCPConn) writeLoop() {
	for {
		select {
		case encoded := <-c.sendQueue:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := c.conn.Write(encoded); err != nil {
				c.logger.Info("write failed", "err", err)
				c.Close()
				return
			}
		case <-c.closed:
			return
		}
	}
}

// TCPServer は接続を受け付け、受信したフレームを events に流す。
type TCPServer struct {
	listener *net.TCPListener
	logger   *slog.Logger
}

func ListenTCP(port int, logger *slog.Logger) (*TCPServer, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: port})
	if err != nil {
		return nil, err
	}
	return &TCPServer{listener: listener, logger: logger.With("component", "tcp")}, nil
}

// Serve は ctx が終わるまで接続を受け付ける。
func (s *TCPServer) Serve(ctx context.Context, events chan<- TCPEvent) {
	go func() {
		<-ctx.Done()
		_ = s.listener.Close()
	}()
	for {
		conn, err := s.listener.AcceptTCP()
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Error("accept", "err", err)
			}
			return
		}
		// 小さなイベントを遅延なく届けるため Nagle を無効にする。
		_ = conn.SetNoDelay(true)
		c := &TCPConn{
			id:        nextConnID.Add(1),
			conn:      conn,
			sendQueue: make(chan []byte, sendQueueSize),
			closed:    make(chan struct{}),
		}
		c.logger = s.logger.With("conn", c.id, "remote", conn.RemoteAddr().String())
		c.logger.Info("accepted")
		go c.readLoop(ctx, events)
		go c.writeLoop()
		go func() {
			select {
			case <-ctx.Done():
				c.Close()
			case <-c.closed:
			}
		}()
	}
}
