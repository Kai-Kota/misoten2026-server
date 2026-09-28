package net

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// FrameLengthSize は length フィールドのバイト数。
	FrameLengthSize = 4
	// FrameTypeSize は msgType フィールドのバイト数。
	FrameTypeSize = 2
	// MaxFrameLength は length（msgType + payload）の上限。
	MaxFrameLength = 64 * 1024
	// MaxPayloadSize は1フレームに載せられる payload の上限。
	MaxPayloadSize = MaxFrameLength - FrameTypeSize
)

var (
	ErrFrameTooLarge = errors.New("frame too large")
	ErrFrameTooSmall = errors.New("frame too small")
)

// Frame は TCP 上の1メッセージ。
// 形式: uint32 length | uint16 msgType | payload（リトルエンディアン）。
type Frame struct {
	MsgType uint16
	Payload []byte
}

// EncodeFrame は Frame を送信用のバイト列にする。
func EncodeFrame(msgType uint16, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("%w: payload %d bytes", ErrFrameTooLarge, len(payload))
	}
	length := FrameTypeSize + len(payload)
	buffer := make([]byte, FrameLengthSize+length)
	binary.LittleEndian.PutUint32(buffer[0:4], uint32(length))
	binary.LittleEndian.PutUint16(buffer[4:6], msgType)
	copy(buffer[6:], payload)
	return buffer, nil
}

// ReadFrame は r から1フレームを読む。途中までしか届いていない場合は届くまで待つ。
func ReadFrame(r io.Reader) (Frame, error) {
	var header [FrameLengthSize + FrameTypeSize]byte
	if _, err := io.ReadFull(r, header[:FrameLengthSize]); err != nil {
		return Frame{}, err
	}
	length := binary.LittleEndian.Uint32(header[0:4])
	if length < FrameTypeSize {
		return Frame{}, fmt.Errorf("%w: length %d", ErrFrameTooSmall, length)
	}
	if length > MaxFrameLength {
		return Frame{}, fmt.Errorf("%w: length %d", ErrFrameTooLarge, length)
	}
	if _, err := io.ReadFull(r, header[FrameLengthSize:]); err != nil {
		return Frame{}, unexpectedEOF(err)
	}
	payload := make([]byte, length-FrameTypeSize)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, unexpectedEOF(err)
	}
	return Frame{MsgType: binary.LittleEndian.Uint16(header[4:6]), Payload: payload}, nil
}

// フレームの途中で切れた場合は、正常な切断と区別できるようにする。
func unexpectedEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
