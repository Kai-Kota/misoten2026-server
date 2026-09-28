package net

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodePacketLayout(t *testing.T) {
	packet, err := EncodePacket(UDPHeader{MsgType: 0x10, PlayerID: 2, Seq: 0x1234, Token: 0xDEADBEEF}, []byte{0x7B, 0x7D})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x55, 0xB0, 0x01, 0x10, 0x02, 0x00, 0x34, 0x12, 0xEF, 0xBE, 0xAD, 0xDE, 0x7B, 0x7D}
	if !bytes.Equal(packet, want) {
		t.Fatalf("packet = % X, want % X", packet, want)
	}
}

func TestPacketRoundTrip(t *testing.T) {
	header := UDPHeader{MsgType: 0x20, PlayerID: ServerPlayerID, Seq: 65535, Token: 42}
	payload := []byte(`{"serverTick":10,"players":[]}`)
	packet, err := EncodePacket(header, payload)
	if err != nil {
		t.Fatal(err)
	}
	decodedHeader, decodedPayload, err := DecodePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if decodedHeader != header || !bytes.Equal(decodedPayload, payload) {
		t.Fatalf("decoded %+v %q", decodedHeader, decodedPayload)
	}
}

func TestDecodePacketErrors(t *testing.T) {
	valid, _ := EncodePacket(UDPHeader{MsgType: 1}, nil)
	badMagic := append([]byte(nil), valid...)
	badMagic[0] = 0
	badVersion := append([]byte(nil), valid...)
	badVersion[2] = ProtocolVersion + 1
	tests := []struct {
		name   string
		packet []byte
		want   error
	}{
		{"empty", nil, ErrPacketTooSmall},
		{"short", valid[:UDPHeaderSize-1], ErrPacketTooSmall},
		{"too large", make([]byte, MaxUDPPacketSize+1), ErrPacketTooLarge},
		{"bad magic", badMagic, ErrBadMagic},
		{"bad version", badVersion, ErrBadVersion},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := DecodePacket(test.packet); !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodePacketHeaderOnly(t *testing.T) {
	packet, _ := EncodePacket(UDPHeader{MsgType: 0x02, PlayerID: ServerPlayerID, Seq: 1, Token: 9}, nil)
	_, payload, err := DecodePacket(packet)
	if err != nil || len(payload) != 0 {
		t.Fatalf("payload %d bytes, err %v", len(payload), err)
	}
}

func TestEncodePacketSizeLimit(t *testing.T) {
	if _, err := EncodePacket(UDPHeader{}, make([]byte, MaxUDPPayloadSize)); err != nil {
		t.Fatalf("max payload rejected: %v", err)
	}
	if _, err := EncodePacket(UDPHeader{}, make([]byte, MaxUDPPayloadSize+1)); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("err = %v, want ErrPacketTooLarge", err)
	}
}

func TestIsSeqNewer(t *testing.T) {
	tests := []struct {
		seq, last uint16
		want      bool
	}{
		{1, 0, true},
		{0, 0, false},
		{0, 1, false},
		{0, 65535, true},
		{5, 65530, true},
		{65530, 5, false},
		{32767, 0, true},
		{32768, 0, false},
	}
	for _, test := range tests {
		if got := IsSeqNewer(test.seq, test.last); got != test.want {
			t.Errorf("IsSeqNewer(%d, %d) = %v, want %v", test.seq, test.last, got, test.want)
		}
	}
}
