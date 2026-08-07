// Package runner 提供集中式和 DEX 双模式共享的启动逻辑
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

	"github.com/AnuBookDEX/engine/internal/core/market"
	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/infra/scheduler"
	"github.com/AnuBookDEX/engine/internal/infra/statistics"
	"github.com/spf13/cast"
)

// StartHTTPServer 启动健康检查 HTTP 服务
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
	io.WriteString(w, "AnuBookDEX engine running\n")
}

// Recover 包装 goroutine，捕获 panic 并优雅退出
func Recover() {
	if e := recover(); e != nil {
		common.Error("server exit:", e, string(debug.Stack()))
		os.Exit(common.ErrnoSystemError)
	}
}

// OrderHandler 抽象撮合结果的派发方式，供集中式和 DEX 模式各自实现
type OrderHandler func(mrJSON []byte, mr *match.MatchResult)

// StartMatcher 为单个交易对运行主撮合循环
// 通过回调（onSnapshot、onOrder）抽象基础设施差异，使集中式和 DEX 入口
// 可以共享相同的 ticker/depth/matching 逻辑
func StartMatcher(
	book *match.OrderBook,
	orderSeqChan <-chan *match.Order,
	onSnapshot func(cloneBook *match.OrderBook),
	onSnapSignal func(),
	onOrder OrderHandler,
	onReport func(),
) {
	defer Recover()

	// 初始化定时器
	snapshotTicker := scheduler.NewTickerSnapshot()                                                                                           // 订单簿快照定时器
	orderBookReportTicker := scheduler.NewTickerOrderbookReport()                                                                             // 订单簿状态上报定时器
	minDepthTicker := time.NewTicker(cast.ToDuration(config.GetInt64("market.min-depth-update-interval-ms", 100)) * time.Millisecond)         // 最小深度更新间隔
	stackedTicker := time.NewTicker(cast.ToDuration(config.GetInt64("market.min-stacked-depth-update-interval-ms", 1000)) * time.Millisecond) // 聚合深度更新间隔
	reportTicker := time.NewTicker(time.Second * 10)                                                                                          // 撮合状态上报间隔

	workMark := false                                                                                          // 撮合活动标记：有新成交时才刷新深度
	defaultDepthTimeMs := common.TimestampNowMs() + config.GetInt64("market.default-update-interval-ms", 1000) // 深度定时刷新的下次触发时间

	for {
		select {
		case <-snapshotTicker.C:
			// 定期生成订单簿快照，用于故障恢复
			cloneBook := book.Clone()
			onSnapshot(cloneBook)
			onSnapSignal()

		case <-orderBookReportTicker.C:
			// 定期输出订单簿统计信息到日志
			book.Report()

		case order := <-orderSeqChan:
			// 收到新订单，执行撮合
			common.Debug("received order", order.OrderId, "|symbol", book.Symbol, "|", order.CreateAt)
			mrAB := book.GenMatchResult(order)

			bytesJson, err := json.Marshal(mrAB)
			if err != nil {
				common.Fatal("match encode to json err", err, mrAB)
			}

			statistics.SetMatchTag(order.SeqId)
			onOrder(bytesJson, &mrAB.Mr) // 派发撮合结果（持久化/行情/结算）

			common.Debug(string(bytesJson))
			statistics.IncrMatchNum()
			workMark = true // 标记有撮合活动，触发深度更新

		case <-reportTicker.C:
			// 定期上报撮合进度
			onReport()
			common.Info(fmt.Sprintf("%s matcher status --- puller.channel.length[%d] currentId[%d]",
				book.Symbol, len(orderSeqChan), book.FromId))

		case <-minDepthTicker.C:
			// 深度更新：有撮合活动时立即更新，否则按默认间隔定时更新
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
			// 定期生成聚合深度（按比例合并档位）
			market.BuildAndReportDepthPercent10(book)
		}
	}
}
