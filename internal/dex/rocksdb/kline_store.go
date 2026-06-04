package rocksdb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/l2quote"
)

// KlineStore 基于本地文件的 K线存储，实现 interfaces.KlineStore
// MVP 阶段使用 JSON 文件存储，后续替换为 Pebble/badger 嵌入式 KV
type KlineStore struct {
	mu      sync.RWMutex
	dataDir string
	// 内存缓存：key = "symbol:klineType", value = 最新 Kline
	cache map[string]*l2quote.QuoteKline
}

// NewKlineStore 创建 K线存储
func NewKlineStore(dataDir string) (*KlineStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create kline data dir: %w", err)
	}
	store := &KlineStore{
		dataDir: dataDir,
		cache:   make(map[string]*l2quote.QuoteKline),
	}
	common.Info("rocksdb: kline store initialized at", dataDir)
	return store, nil
}

func (s *KlineStore) cacheKey(symbol, klineType string) string {
	return symbol + ":" + klineType
}

func (s *KlineStore) filePath(symbol, klineType string) string {
	return filepath.Join(s.dataDir, fmt.Sprintf("kline_%s_%s.json", symbol, klineType))
}

// Save 保存 K线
func (s *KlineStore) Save(symbol string, klineType string, k *l2quote.QuoteKline) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.cacheKey(symbol, klineType)
	s.cache[key] = k

	data, err := json.Marshal(k)
	if err != nil {
		return fmt.Errorf("marshal kline: %w", err)
	}

	path := s.filePath(symbol, klineType)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write kline file: %w", err)
	}
	return nil
}

// LoadLatest 加载最新 K线
func (s *KlineStore) LoadLatest(symbol string, klineType string) (*l2quote.QuoteKline, error) {
	s.mu.RLock()
	key := s.cacheKey(symbol, klineType)
	if cached, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	path := s.filePath(symbol, klineType)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read kline file: %w", err)
	}

	var k l2quote.QuoteKline
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("unmarshal kline: %w", err)
	}

	s.mu.Lock()
	s.cache[key] = &k
	s.mu.Unlock()

	return &k, nil
}

// Close 关闭存储（MVP 阶段为 no-op，后续 Pebble/badger 实现需要）
func (s *KlineStore) Close() error {
	return nil
}

// LoadRange 加载范围 K线 (MVP 返回空，后续实现)
func (s *KlineStore) LoadRange(symbol string, klineType string, from, to int64) ([]*l2quote.QuoteKline, error) {
	// TODO: 基于时间范围扫描文件/Pebble iterator
	return nil, nil
}

