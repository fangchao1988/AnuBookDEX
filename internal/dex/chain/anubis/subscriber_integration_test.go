//go:build integration

package anubis

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// 这些测试连真实 Anubis RPC，默认不跑（build tag: integration）
// 运行：go test -tags integration -run TestSubscriber -v ./internal/dex/chain/ -count=1

func newLiveSubscriber(t *testing.T) *Subscriber {
	return &Subscriber{
		rpcEndpoint:  "https://rpc.anubispace.org",
		contractAddr: "0x5037c538B744C7fC3b56D8359Cce01895b5819d8",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		orderTopic:   "0x942d46dddbdc36d1ed575e5093656f2952053568a7867ea0aaf449ace306f03c",
		ctx:          context.Background(),
	}
}

func TestSubscriberGetBlockNumberLive(t *testing.T) {
	s := newLiveSubscriber(t)
	block, err := s.getBlockNumber()
	if err != nil {
		t.Skip("rpc not reachable:", err)
	}
	t.Logf("latest block = %d", block)
	if block == 0 {
		t.Fatal("expected non-zero block number")
	}
}

func TestSubscriberGetLogsLive(t *testing.T) {
	s := newLiveSubscriber(t)
	block, err := s.getBlockNumber()
	if err != nil {
		t.Skip("rpc not reachable:", err)
	}
	// 查最近 100 个区块的 OrderSubmitted 事件
	from := uint64(1)
	if block > 100 {
		from = block - 100
	}
	logs, err := s.getLogs(from, block)
	if err != nil {
		t.Skip("getLogs failed (endpoint may reject range):", err)
	}
	t.Logf("fetched %d logs, block [%d, %d]", len(logs), from, block)
	for i, lg := range logs {
		if i >= 3 {
			break
		}
		t.Logf("  log[%d]: topics=%d data_len=%d block=%s", i, len(lg.Topics), len(lg.Data)/2, lg.BlockNumber)
	}
}
