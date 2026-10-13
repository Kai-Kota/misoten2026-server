package net

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// UDPMagic はこのゲームのパケットであることを示す値。
	UDPMagic uint16 = 0xB055
	// ProtocolVersion は UDP ヘッダと JoinRequest で使うプロトコルのバージョン。
	ProtocolVersion uint8 = 3
	// UDPHeaderSize は UDP ヘッダのバイト数。
	UDPHeaderSize = 12
	// MaxUDPPacketSize は1パケットの上限（ヘッダ込み）。
	MaxUDPPacketSize = 1200
	// MaxUDPPayloadSize は1パケットに載せられる payload の上限。
	MaxUDPPayloadSize = MaxUDPPacketSize - UDPHeaderSize
	// ServerPlayerID はサーバー発のパケットに入れる playerId。
	ServerPlayerID uint8 = 0xFF
)

var (
	ErrPacketTooSmall = errors.New("packet too small")
	ErrPacketTooLarge = errors.New("packet too large")
	ErrBadMagic       = errors.New("bad magic")
	ErrBadVersion     = errors.New("bad version")
)

// UDPHeader は UDP パケット先頭の12バイト（リトルエンディアン）。
// uint16 magic | uint8 version | uint8 msgType | uint8 playerId | uint8 reserved | uint16 seq | uint32 token
type UDPHeader struct {
	MsgType  uint8
	PlayerID uint8
	Seq      uint16
	Token    uint32
}

// EncodePacket はヘッダと payload から送信用のパケットを作る。
func EncodePacket(header UDPHeader, payload []byte) ([]byte, error) {
	if len(payload) > MaxUDPPayloadSize {
		return nil, fmt.Errorf("%w: payload %d bytes", ErrPacketTooLarge, len(payload))
	}
	packet := make([]byte, UDPHeaderSize+len(payload))
	binary.LittleEndian.PutUint16(packet[0:2], UDPMagic)
	packet[2] = ProtocolVersion
	packet[3] = header.MsgType
	packet[4] = header.PlayerID
	packet[5] = 0
	binary.LittleEndian.PutUint16(packet[6:8], header.Seq)
	binary.LittleEndian.PutUint32(packet[8:12], header.Token)
	copy(packet[UDPHeaderSize:], payload)
	return packet, nil
}

// DecodePacket はパケットを検証し、ヘッダと payload に分ける。
// payload は packet の一部を指すので、packet を再利用する場合は呼び出し側でコピーする。
func DecodePacket(packet []byte) (UDPHeader, []byte, error) {
	if len(packet) < UDPHeaderSize {
		return UDPHeader{}, nil, fmt.Errorf("%w: %d bytes", ErrPacketTooSmall, len(packet))
	}
	if len(packet) > MaxUDPPacketSize {
		return UDPHeader{}, nil, fmt.Errorf("%w: %d bytes", ErrPacketTooLarge, len(packet))
	}
	if magic := binary.LittleEndian.Uint16(packet[0:2]); magic != UDPMagic {
		return UDPHeader{}, nil, fmt.Errorf("%w: 0x%04X", ErrBadMagic, magic)
	}
	if packet[2] != ProtocolVersion {
		return UDPHeader{}, nil, fmt.Errorf("%w: %d", ErrBadVersion, packet[2])
	}
	header := UDPHeader{
		MsgType:  packet[3],
		PlayerID: packet[4],
		Seq:      binary.LittleEndian.Uint16(packet[6:8]),
		Token:    binary.LittleEndian.Uint32(packet[8:12]),
	}
	return header, packet[UDPHeaderSize:], nil
}

// IsSeqNewer は seq が last より新しいかを、uint16 の一周を考慮して判定する。
func IsSeqNewer(seq, last uint16) bool {
	return int16(seq-last) > 0
}
