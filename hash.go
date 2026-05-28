package toggleflow

import (
	"crypto/sha256"
	"encoding/binary"
)

// bucket returns a stable number in [0, 99] for a user+flag pair.
// Identical to backend/internal/eval/hash.go so rollout percentages are consistent.
func bucket(flagKey, userKey string) int {
	h := sha256.Sum256([]byte(flagKey + "." + userKey))
	n := binary.BigEndian.Uint32(h[:4])
	return int(n % 100)
}
