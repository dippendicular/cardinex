package sketch

const (
	fnv64Offset = 14695981039346656037
	fnv64Prime  = 1099511628211
)

// Hash64WithSeed is a deterministic FNV-1a variant used for double hashing.
func Hash64WithSeed(b []byte, seed uint64) uint64 {
	h := fnv64Offset ^ seed
	for _, c := range b {
		h ^= uint64(c)
		h *= fnv64Prime
	}
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

func DoubleHash(b []byte) (uint64, uint64) {
	h1 := Hash64WithSeed(b, 0xcbf29ce484222325)
	h2 := Hash64WithSeed(b, 0x9e3779b97f4a7c15)
	return h1, h2
}
