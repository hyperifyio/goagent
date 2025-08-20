package wasmrun

import (
	"errors"
)

// ErrOOBMemory is returned when attempting to read outside the bounds
// of the guest's linear memory. It standardizes to code "OOB_MEMORY".
var ErrOOBMemory = errors.New("OOB_MEMORY")

// readLinearMemory returns a copy of length bytes starting at ptr from the
// provided linear memory slice. It performs strict bounds checks and returns
// ErrOOBMemory on any out-of-bounds or overflow condition. A length of zero
// returns an empty slice without error only when ptr is within [0,len] (ptr==len allowed).
func readLinearMemory(linearMemory []byte, ptr uint32, length uint32) ([]byte, error) {
	memLen := uint32(len(linearMemory))

	// Zero-length reads are valid only if ptr is not beyond the end.
	if length == 0 {
		if ptr > memLen { // strictly beyond end is invalid
			return nil, ErrOOBMemory
		}
		return make([]byte, 0), nil
	}

	// For non-zero length, ptr must be within [0, len-1]
	if ptr >= memLen {
		return nil, ErrOOBMemory
	}

	// Detect overflow in ptr + length
	end := ptr + length
	if end < ptr { // overflow
		return nil, ErrOOBMemory
	}

	// Ensure end does not exceed memory length (end index is exclusive).
	if end > memLen {
		return nil, ErrOOBMemory
	}

	// Safe to slice; copy to avoid exposing backing array.
	out := make([]byte, length)
	copy(out, linearMemory[ptr:end])
	return out, nil
}
