package rocksdb

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/match"
)

// SnapshotStore 基于本地文件的订单簿快照存储，实现 interfaces.SnapshotStore
// MVP 阶段使用 gob 编码文件，兼容现有快照格式，后续替换为 Pebble/badger
type SnapshotStore struct {
	mu      sync.RWMutex
	dataDir string
	// 内存缓存最新快照
	cache map[string]*match.OrderBook
}

// NewSnapshotStore 创建快照存储
func NewSnapshotStore(dataDir string) (*SnapshotStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create snapshot data dir: %w", err)
	}
	store := &SnapshotStore{
		dataDir: dataDir,
		cache:   make(map[string]*match.OrderBook),
	}
	common.Info("rocksdb: snapshot store initialized at", dataDir)
	return store, nil
}

func (s *SnapshotStore) snapshotDir(symbol string) string {
	return filepath.Join(s.dataDir, "snapshot", symbol)
}

// Save 保存订单簿快照
// 使用 gob 编码，每次保存生成新文件（按 FromId 命名）
func (s *SnapshotStore) Save(symbol string, book *match.OrderBook) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[symbol] = book

	dir := s.snapshotDir(symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(book); err != nil {
		return fmt.Errorf("encode order book: %w", err)
	}

	filename := filepath.Join(dir, fmt.Sprintf("%d.book", book.FromId))
	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write snapshot file: %w", err)
	}

	common.Debug(fmt.Sprintf("rocksdb: snapshot saved symbol=%s fromId=%d", symbol, book.FromId))
	return nil
}

// LoadLatest 加载最新订单簿快照
func (s *SnapshotStore) LoadLatest(symbol string) (*match.OrderBook, error) {
	s.mu.RLock()
	if cached, ok := s.cache[symbol]; ok {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	dir := s.snapshotDir(symbol)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snapshot dir: %w", err)
	}

	if len(entries) == 0 {
		return nil, nil
	}

	// 找到最新的快照文件（按 FromId 排序）
	var latestFile string
	var latestId int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".book") {
			continue
		}
		idStr := strings.TrimSuffix(entry.Name(), ".book")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		if id > latestId {
			latestId = id
			latestFile = entry.Name()
		}
	}

	if latestFile == "" {
		return nil, nil
	}

	data, err := os.ReadFile(filepath.Join(dir, latestFile))
	if err != nil {
		return nil, fmt.Errorf("read snapshot file: %w", err)
	}

	book := match.NewOrderBook()
	dec := gob.NewDecoder(bytes.NewBuffer(data))
	if err := dec.Decode(book); err != nil {
		return nil, fmt.Errorf("decode order book: %w", err)
	}

	s.mu.Lock()
	s.cache[symbol] = book
	s.mu.Unlock()

	common.Info(fmt.Sprintf("rocksdb: snapshot loaded symbol=%s fromId=%d", symbol, book.FromId))
	return book, nil
}

// Close 关闭存储（MVP 阶段为 no-op，后续 Pebble/badger 实现需要）
func (s *SnapshotStore) Close() error {
	return nil
}

// PruneOld 清理旧快照，保留最近 keepN 个
func (s *SnapshotStore) PruneOld(symbol string, keepN int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.snapshotDir(symbol)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type snapshotEntry struct {
		name string
		id   int64
	}
	var snapshots []snapshotEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".book") {
			continue
		}
		idStr := strings.TrimSuffix(entry.Name(), ".book")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		snapshots = append(snapshots, snapshotEntry{name: entry.Name(), id: id})
	}

	if len(snapshots) <= keepN {
		return nil
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].id > snapshots[j].id // 降序，最新的在前
	})

	// 删除超出保留数量的旧快照
	for _, snap := range snapshots[keepN:] {
		path := filepath.Join(dir, snap.name)
		if err := os.Remove(path); err != nil {
			common.Warn("rocksdb: failed to prune snapshot:", path, err)
		}
	}
	common.Info(fmt.Sprintf("rocksdb: pruned %d old snapshots for %s", len(snapshots)-keepN, symbol))
	return nil
}

