// Package runner provides shared startup logic for both centralized and DEX modes.
package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/core/market"
	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/scheduler"
	"github.com/AnuBookDEX/engine/internal/infra/statistics"
	"github.com/spf13/cast"
)

// StartHTTPServer starts the health-check HTTP server.
func StartHTTPServer(port int) {
	go func() {
		http.HandleFunc("/health", healthHandler)
		err := http.ListenAndServe(":"+strconv.Itoa(port), nil)
		if err != nil {
			common.Fatal("http listen error:", err)
		}
	}()
}

func healthHandler(w http.ResponseWriter, req *http.Request) {
	io.WriteString(w, "AnuBookDEX engine running")
}

// Recover wraps a goroutine with panic recovery.
func Recover() {
	if e := recover(); e != nil {
		common.Error("server exit:", e, string(debug.Stack()))
		os.Exit(common.ErrnoSystemError)
	}
}

// OrderHandler abstracts how a processed order's result is dispatched.
type OrderHandler func(mrJSON []byte, mr *match.MatchResult)

// StartMatcher runs the main matching loop for a single symbol.
// It abstracts infrastructure-specific callbacks (onSnapshot, onOrder) so both
// centralized and DEX entry points can share the ticker/depth/matching logic.
func StartMatcher(
	book *match.OrderBook,
	orderSeqChan <-chan *match.Order,
	onSnapshot func(cloneBook *match.OrderBook),
	onSnapSignal func(),
	onOrder OrderHandler,
	onReport func(),
) {
	defer Recover()

	snapshotTicker := scheduler.NewTickerSnapshot()
	orderBookReportTicker := scheduler.NewTickerOrderbookReport()
	minDepthTicker := time.NewTicker(cast.ToDuration(config.GetInt64("market.min-depth-update-interval-ms", 100)) * time.Millisecond)
	stackedTicker := time.NewTicker(cast.ToDuration(config.GetInt64("market.min-stacked-depth-update-interval-ms", 1000)) * time.Millisecond)
	reportTicker := time.NewTicker(time.Second * 10)

	workMark := false
	defaultDepthTimeMs := common.TimestampNowMs() + config.GetInt64("market.default-update-interval-ms", 1000)

	for {
		select {
		case <-snapshotTicker.C:
			cloneBook := book.Clone()
			onSnapshot(cloneBook)
			onSnapSignal()

		case <-orderBookReportTicker.C:
			book.Report()

		case order := <-orderSeqChan:
			common.Debug("received order", order.OrderId, "|symbol", book.Symbol, "|", order.CreateAt)
			mrAB := book.GenMatchResult(order)

			bytesJson, err := json.Marshal(mrAB)
			if err != nil {
				common.Fatal("match encode to json err", err, mrAB)
			}

			statistics.SetMatchTag(order.SeqId)
			onOrder(bytesJson, &mrAB.Mr)

			common.Debug(string(bytesJson))
			statistics.IncrMatchNum()
			workMark = true

		case <-reportTicker.C:
			onReport()
			common.Info(fmt.Sprintf("%s matcher status --- puller.channel.length[%d] currentId[%d]",
				book.Symbol, len(orderSeqChan), book.FromId))

		case <-minDepthTicker.C:
			nowMs := common.TimestampNowMs()
			if workMark {
				market.BuildAndReportDepth(book)
				defaultDepthTimeMs = nowMs + config.GetInt64("market.default-update-interval-ms", 1000)
				workMark = false
			}
			if defaultDepthTimeMs < nowMs {
				market.BuildAndReportDepth(book)
				defaultDepthTimeMs = nowMs + config.GetInt64("market.default-update-interval-ms", 1000)
			}

		case <-stackedTicker.C:
			market.BuildAndReportDepthPercent10(book)
		}
	}
}
