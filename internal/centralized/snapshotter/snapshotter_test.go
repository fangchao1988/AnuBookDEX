package snapshotter

import (
	"encoding/gob"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/shopspring/decimal"
	"github.com/spf13/viper"
	"log"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/core/match"
	"math/rand"
	"os"
	"testing"
)

func ExampleMinGap() {
	if MinGap() <= 0 {
		log.Println(MinGap())
	}
}

func BenchmarkDumpSnapshot(b *testing.B) {
	decimal.MarshalJSONWithoutQuotes = true
	book := match.InitOrderBook(26, "btceth")
	addRandomOrder(book, 800)
	gob.Register(match.Order{})
	viper.Set("aws.s3.enable", false)
	name := BuildSnapshotPath(book, 200)
	log.Println(name)
	b.StartTimer()
	dump(book, name)
	b.StopTimer()
}

func addRandomOrder(book *match.OrderBook, num int) {
	var t int64
	t = 0
	for num > 0 {
		num--
		order1 := &match.Order{
			SeqId:          t,
			OrderId:        rand.Int63(),
			State:          match.PartialFilled,
			Price:          decimal.NewFromFloat(rand.Float64()),
			UnfilledAmount: decimal.New(rand.Int63(), 0),
			CircuitRate:    decimal.NewFromFloat(rand.Float64()),
			CreateAt:       rand.Int63(),
		}
		book.Cache()[t] = order1
		t++
		book.BuySet.Add(order1)
		order2 := &match.Order{
			SeqId:          t,
			OrderId:        rand.Int63(),
			State:          match.PartialFilled,
			Price:          decimal.NewFromFloat(rand.Float64()),
			UnfilledAmount: decimal.New(rand.Int63(), 0),
			CircuitRate:    decimal.NewFromFloat(rand.Float64()),
			CreateAt:       rand.Int63(),
		}
		book.SellSet.Add(order2)
		book.Cache()[t] = order2
		t++

	}
}

func ExampleGetSnapshotIds() {
	GetSnapshotIds("btcusdt")
}

func ExampleS3() {
	//common.LogInit(common.LogLevel)
	common.LoadConfigViper()
	file, err := os.Create("test.filestt")
	if err != nil {
		log.Println("file create :", err)
	}
	_, err = file.Write([]byte("yuchangxu use s3 test"))
	if err != nil {
		log.Println("write:", err)
	}
	file.Close()
	if err != nil {
		log.Println("sync :", err)
	}
	file, err = os.Open("test.filestt")

	_, err = upLoader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(viper.GetString("aws.s3.bucket")),
		Key:    aws.String(file.Name()),
		Body:   file,
	})
	if err != nil {
		log.Println("s3 upload file failed", err)
	}
}
