package wasmrun

import (
	"bytes"
	"testing"
)

func TestReadLinearMemory_Valid(t *testing.T) {
	mem := []byte("hello world")
	b, err := readLinearMemory(mem, 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(b, []byte("hello")) {
		t.Fatalf("want hello, got %q", string(b))
	}
	b, err = readLinearMemory(mem, 6, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(b, []byte("world")) {
		t.Fatalf("want world, got %q", string(b))
	}
}

func TestReadLinearMemory_ZeroLength(t *testing.T) {
	mem := []byte("abc")
	b, err := readLinearMemory(mem, 1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b) != 0 {
		t.Fatalf("expected empty slice, got %d", len(b))
	}
}

func TestReadLinearMemory_OutOfBounds(t *testing.T) {
	mem := []byte("abc")
	cases := [][2]uint32{
		{3, 1}, // ptr == len
		{4, 0}, // ptr > len
		{2, 2}, // end == 4 > len 3
		{0, 4}, // length > len
	}
	for i, c := range cases {
		if _, err := readLinearMemory(mem, c[0], c[1]); err == nil {
			t.Fatalf("case %d: expected OOB error", i)
		}
	}
}

func TestReadLinearMemory_Overflow(t *testing.T) {
	mem := make([]byte, 8)
	// Choose ptr and length that overflow uint32 when summed
	ptr := uint32(0xFFFFFFF0)
	length := uint32(0x30)
	if _, err := readLinearMemory(mem, ptr, length); err == nil {
		t.Fatalf("expected overflow to be treated as OOB")
	}
}
