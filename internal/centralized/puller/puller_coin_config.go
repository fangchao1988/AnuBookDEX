package puller

import (
	"database/sql"
	"fmt"
	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func InitCoinConfig(symbol string) {
	db := &DbInfo{
		symbol: symbol,
	}
	db.initConfDbInfo(symbol)
	db.InitSymbolConf()
}

func (d *DbInfo) initConfDbInfo(symbol string) {
	d.DataSourceName = fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8",
		config.GetString("aibit-db.user", ""),
		config.GetString("aibit-db.password", ""),
		config.GetString("aibit-db.endpoint", ""),
		config.GetString("aibit-db.db", ""),
	)
	var err error
	d.DB, err = sql.Open("mysql", d.DataSourceName)
	if err != nil {
		common.Fatal("open db error", err)
	}
	d.DB.SetMaxOpenConns(config.GetInt("aibit-db.conn-num", 10))
	d.DB.SetMaxIdleConns(config.GetInt("aibit-db.conn-num", 10))
	d.DB.SetConnMaxLifetime(8 * time.Hour) // set use forever

	d.Prepare = getPrepare(symbol)
	d.dbStmt, err = d.DB.Prepare(d.Prepare)
	if err != nil {
		common.Fatal("prepare sql error:", err)
	}
}

// 初始化币对深度
func (d *DbInfo) InitSymbolConf() {
	var scale int
	d.DB.QueryRow(fmt.Sprintf("SELECT  price_scale FROM aibit_coin_pair_config WHERE symbol = '%s' ", d.symbol)).Scan(&scale)
	if scale <= 0 {
		panic("error price scale " + d.symbol)
	}
	common.SetSymbolDepth(d.symbol, scale)

}
