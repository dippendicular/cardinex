package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
)

// Meta is persisted next to each snapshot.
type Meta struct {
	Name    string   `json:"name"`
	Kind    uint16   `json:"kind"`
	Batches []string `json:"batches,omitempty"` // recent batch IDs, for idempotency across restarts
}

// Record is one persisted sketch: its last snapshot plus the log of writes
// applied after it.
type Record struct {
	ID       string
	Meta     Meta
	Snapshot []byte
	Log      [][]byte
}

// Store persists snapshots and the append-only batch log.
type Store interface {
	Load(ctx context.Context) ([]Record, error)
	// Save writes the snapshot and drops the first trimLog log entries, atomically.
	Save(ctx context.Context, id string, meta Meta, snapshot []byte, trimLog int64) error
	Append(ctx context.Context, id string, entry []byte) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore keeps everything in process. It is for tests and for running
// without Redis; nothing survives a restart.
type MemoryStore struct {
	mu   sync.Mutex
	recs map[string]*Record
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{recs: map[string]*Record{}} }

func (m *MemoryStore) Load(context.Context) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.recs))
	for _, r := range m.recs {
		out = append(out, Record{ID: r.ID, Meta: r.Meta, Snapshot: r.Snapshot, Log: append([][]byte(nil), r.Log...)})
	}
	return out, nil
}

func (m *MemoryStore) Save(_ context.Context, id string, meta Meta, snap []byte, trim int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.recs[id]
	if r == nil {
		r = &Record{ID: id}
		m.recs[id] = r
	}
	r.Meta, r.Snapshot = meta, append([]byte(nil), snap...)
	if trim > int64(len(r.Log)) {
		trim = int64(len(r.Log))
	}
	r.Log = r.Log[trim:]
	return nil
}

func (m *MemoryStore) Append(_ context.Context, id string, e []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.recs[id]
	if r == nil {
		return errors.New("store: append before first save")
	}
	r.Log = append(r.Log, append([]byte(nil), e...))
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, id)
	return nil
}

// RedisStore keys: cardinex:meta:<id> (JSON), cardinex:snap:<id> (CDNX
// bytes), cardinex:log:<id> (list of JSON write entries).
type RedisStore struct{ rdb *redis.Client }

func NewRedisStore(addr string) *RedisStore {
	return &RedisStore{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

const (
	metaPrefix = "cardinex:meta:"
	snapPrefix = "cardinex:snap:"
	logPrefix  = "cardinex:log:"
)

// Load iterates with SCAN, never KEYS, so it does not block Redis.
func (s *RedisStore) Load(ctx context.Context) ([]Record, error) {
	var out []Record
	iter := s.rdb.Scan(ctx, 0, metaPrefix+"*", 512).Iterator()
	for iter.Next(ctx) {
		id := strings.TrimPrefix(iter.Val(), metaPrefix)
		mb, err := s.rdb.Get(ctx, metaPrefix+id).Bytes()
		if err != nil {
			continue
		}
		var meta Meta
		if json.Unmarshal(mb, &meta) != nil {
			continue
		}
		snap, err := s.rdb.Get(ctx, snapPrefix+id).Bytes()
		if err != nil {
			continue
		}
		logs, err := s.rdb.LRange(ctx, logPrefix+id, 0, -1).Result()
		if err != nil {
			return nil, err
		}
		rec := Record{ID: id, Meta: meta, Snapshot: snap}
		for _, l := range logs {
			rec.Log = append(rec.Log, []byte(l))
		}
		out = append(out, rec)
	}
	return out, iter.Err()
}

func (s *RedisStore) Save(ctx context.Context, id string, meta Meta, snap []byte, trim int64) error {
	mb, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, metaPrefix+id, mb, 0)
		p.Set(ctx, snapPrefix+id, snap, 0)
		if trim > 0 {
			p.LTrim(ctx, logPrefix+id, trim, -1)
		}
		return nil
	})
	return err
}

func (s *RedisStore) Append(ctx context.Context, id string, e []byte) error {
	return s.rdb.RPush(ctx, logPrefix+id, e).Err()
}

func (s *RedisStore) Delete(ctx context.Context, id string) error {
	return s.rdb.Del(ctx, metaPrefix+id, snapPrefix+id, logPrefix+id).Err()
}
