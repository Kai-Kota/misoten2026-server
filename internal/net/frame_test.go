package net

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

func TestEncodeFrameLayout(t *testing.T) {
	encoded, err := EncodeFrame(0x0102, []byte{0xAA, 0xBB, 0xCC})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x05, 0x00, 0x00, 0x00, 0x02, 0x01, 0xAA, 0xBB, 0xCC}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded = % X, want % X", encoded, want)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	payloads := [][]byte{{}, []byte(`{"version":1,"playerName":"Kai"}`), bytes.Repeat([]byte{7}, MaxPayloadSize)}
	var stream bytes.Buffer
	for index, payload := range payloads {
		encoded, err := EncodeFrame(uint16(0x0100+index), payload)
		if err != nil {
			t.Fatal(err)
		}
		stream.Write(encoded)
	}
	for index, payload := range payloads {
		frame, err := ReadFrame(&stream)
		if err != nil {
			t.Fatalf("frame %d: %v", index, err)
		}
		if frame.MsgType != uint16(0x0100+index) || !bytes.Equal(frame.Payload, payload) {
			t.Fatalf("frame %d mismatch: type 0x%04X, %d bytes", index, frame.MsgType, len(frame.Payload))
		}
	}
	if _, err := ReadFrame(&stream); !errors.Is(err, io.EOF) {
		t.Fatalf("after last frame err = %v, want EOF", err)
	}
}

func TestReadFrameFromOneByteReads(t *testing.T) {
	encoded, _ := EncodeFrame(0x0108, []byte(`{"isPong":true}`))
	frame, err := ReadFrame(iotest.OneByteReader(bytes.NewReader(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if frame.MsgType != 0x0108 || string(frame.Payload) != `{"isPong":true}` {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestReadFrameRejectsBadLength(t *testing.T) {
	tests := []struct {
		name   string
		length uint32
		want   error
	}{
		{"zero", 0, ErrFrameTooSmall},
		{"type only partial", 1, ErrFrameTooSmall},
		{"over limit", MaxFrameLength + 1, ErrFrameTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buffer [8]byte
			binary.LittleEndian.PutUint32(buffer[:4], test.length)
			if _, err := ReadFrame(bytes.NewReader(buffer[:])); !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReadFrameAcceptsLimit(t *testing.T) {
	encoded, err := EncodeFrame(1, make([]byte, MaxPayloadSize))
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(encoded); got != MaxFrameLength {
		t.Fatalf("length = %d, want %d", got, MaxFrameLength)
	}
	if _, err := ReadFrame(bytes.NewReader(encoded)); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeFrameRejectsOversizedPayload(t *testing.T) {
	if _, err := EncodeFrame(1, make([]byte, MaxPayloadSize+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameTruncated(t *testing.T) {
	encoded, _ := EncodeFrame(1, []byte("payload"))
	for cut := 1; cut < len(encoded); cut++ {
		_, err := ReadFrame(bytes.NewReader(encoded[:cut]))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("cut at %d: err = %v, want ErrUnexpectedEOF", cut, err)
		}
	}
}
