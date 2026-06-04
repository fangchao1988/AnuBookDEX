package main

import (
	"encoding/json"
	"fmt"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"github.com/AnuBookDEX/engine/internal/core/l2quote"
	"github.com/AnuBookDEX/engine/internal/core/market"
	"github.com/AnuBookDEX/engine/internal/core/match"
	"github.com/AnuBookDEX/engine/internal/centralized/persistence"
	"github.com/AnuBookDEX/engine/internal/centralized/puller"
	"github.com/AnuBookDEX/engine/internal/centralized/rabbitmq"
	"github.com/AnuBookDEX/engine/internal/centralized/redis"
	"github.com/AnuBookDEX/engine/internal/dex/runner"
	"github.com/AnuBookDEX/engine/internal/centralized/snapshotter"
	"github.com/AnuBookDEX/engine/internal/infra/statistics"
	"github.com/AnuBookDEX/engine/internal/centralized/validate"
	"github.com/shopspring/decimal"
	"time"
)

var (
	GitTag    = "2021.7.31.init-release"
	BuildTime = "2021-7-31T00:00:00+0800"
)

func init() {
	decimal.DivisionPrecision = 37
	common.Info("=============== starting server ===============")
}

func main() {
	if err := common.LoadConfigViper(); err != nil {
		panic("config load error: " + err.Error())
	}

	runner.StartHTTPServer(config.GetInt("http.port", 9000))
	common.ZapInit()
	defer runner.Recover()

	statistics.Init()
	snapshotter.Init()
	redis.InitClient()
	rabbitmq.Init()
	match.Init()
	marketInit()

	startExchange()
	validate.ValidateOrderbook()
	common.WriteStartOk()
	common.Info("exchange started")

	for {
		time.Sleep(100 * time.Second)
	}
}

func marketInit() {
	for id, symbol := range config.GetStringSlice("symbols", []string{}) {
		common.Trace("initializing symbol[", id, "]:", symbol)
		exchangeName := fmt.Sprintf("%s.%s", config.GetString("app.name", "market"),
			config.GetString("rabbitmq.exchange.quotation", "l2quote"))
		market.MarketThreadInit(exchangeName, symbol)
	}
}

func startExchange() {
	for _, symbol := range config.GetStringSlice("symbols", []string{}) {
		perch := make(chan []byte, 10000)
		persistence.Init(symbol, perch)

		ch := make(chan *match.Order, 5000)
		mrCh := make(chan []byte, 5000)
		exchangeName := config.GetString("app.profile", "market") + "." + config.GetString("rabbitmq.exchange.quotation", "l2quote")
		l2 := l2quote.NewL2quote(symbol, redis.KlineClient, mrCh, config.GetString("l2quote.snapshot.path", "./sp/"),
			exchangeName, config.GetInt64("l2quote.mq-send-interval-ms", 500),
			config.GetInt64("l2quote.kline-forward-limit", 1440),
			config.GetInt("batch_result", 90),
			config.GetInt64("l2quote.snapshot.n-history", 10),
			config.GetInt("l2quote.make-new-kline-at-sec", 2))
		l2.Init()
		go l2.Run()
		common.Info(fmt.Sprintf("init %s l2quote finish", symbol))

		lastId, ctype := snapshotter.GetLastSnapshotId(symbol)
		var book *match.OrderBook
		if lastId > 0 {
			var err error
			book, err = snapshotter.Load(symbol, ctype, lastId)
			if err != nil {
				common.Fatal("load snapshotter failed lastId:", lastId, " symbol:", symbol)
			}
		} else {
			book = match.InitOrderBook(0, symbol)
		}

		puller.Init(ch, symbol, lastId+1)
		puller.InitCoinConfig(symbol)

		match.OrderBookMap[symbol] = book

		publishChan := match.PublishResultChan(book.Symbol)
		snapshotChan := snapshotter.DumpSnapshotChan(book.Symbol)

		go runner.StartMatcher(book, ch,
			func(cloneBook *match.OrderBook) {
				snapshotChan <- cloneBook
			},
			func() {
				mrCh <- []byte("snapshot!")
			},
			func(mrJSON []byte, mr *match.MatchResult) {
				mrBytesJson, err := json.Marshal(mr)
				if err != nil {
					common.Fatal("match encode to json err", err, mr)
				}
				perch <- mrBytesJson
				publishChan <- mrBytesJson
				mrCh <- mrJSON
			},
			func() {},
		)
	}
}
