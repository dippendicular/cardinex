package sketch

// Sketch is the common contract implemented by all probabilistic structures.
// Implementations mutate in place and must reject incompatible merges.
type Sketch interface {
	Add(item []byte)
	Query(item []byte) interface{}
	Merge(other Sketch) error
	Serialize() []byte
	Deserialize([]byte) error
}
