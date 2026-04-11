package indexer

import (
	"math/big"
	"testing"
)

func TestParseHexUint64(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
		err   bool
	}{
		{"0x0", 0, false},
		{"0x1", 1, false},
		{"0xa", 10, false},
		{"0xff", 255, false},
		{"0x10932c", 1086252, false},
		{"", 0, true},
		{"xyz", 0, true},
	}

	for _, tt := range tests {
		got, err := parseHexUint64(tt.input)
		if tt.err && err == nil {
			t.Errorf("parseHexUint64(%q) expected error", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("parseHexUint64(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("parseHexUint64(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestDecodeUint256(t *testing.T) {
	// 64 hex chars = 32 bytes = one word
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000064" + // word 0: 100
		"00000000000000000000000000000000000000000000000000000000000000c8" // word 1: 200

	w0 := decodeUint256(data, 0)
	if w0.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("word 0: got %s, want 100", w0)
	}

	w1 := decodeUint256(data, 1)
	if w1.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("word 1: got %s, want 200", w1)
	}

	// Short data should not panic
	short := decodeUint256("0x00", 0)
	if short.Sign() != 0 {
		t.Errorf("short data: got %s, want 0", short)
	}

	// Out of range word
	oob := decodeUint256(data, 5)
	if oob.Sign() != 0 {
		t.Errorf("out of range: got %s, want 0", oob)
	}
}

func TestTopicAddr(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{"0x000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7", "0xdac17f958d2ee523a2206206994597c13d831ec7"},
		{"0xdac17f958d2ee523a2206206994597c13d831ec7", "0xdac17f958d2ee523a2206206994597c13d831ec7"},
		{"short", "short"}, // doesn't panic on short input
		{"", ""},
	}

	for _, tt := range tests {
		got := topicAddr(tt.topic)
		if got != tt.want {
			t.Errorf("topicAddr(%q) = %q, want %q", tt.topic, got, tt.want)
		}
	}
}

func TestKnownTopics(t *testing.T) {
	topics := knownTopics()
	if len(topics) < 16 {
		t.Errorf("expected at least 16 known topics, got %d", len(topics))
	}

	// All should start with 0x and be 66 chars
	for _, topic := range topics {
		if len(topic) != 66 {
			t.Errorf("topic %s has length %d, expected 66", topic[:10], len(topic))
		}
		if topic[:2] != "0x" {
			t.Errorf("topic %s missing 0x prefix", topic[:10])
		}
	}
}
