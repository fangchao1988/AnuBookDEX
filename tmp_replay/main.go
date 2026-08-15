package main

import (
	"fmt"

	"github.com/AnuBookDEX/engine/internal/dex/chain/aleo"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

func main() {
	rpc := aleo.NewRESTClient(config.GetString("chain.aleo.rpc-endpoint", "https://api.explorer.provable.com/v1"))
	pid := config.GetString("chain.aleo.program-id", "anubook_dex_p5.aleo")
	for _, tx := range []string{"at150urvmygm4x8z", "at163248kewnpjmp"} {
		p, err := aleo.ExtractAndDecryptOrder(rpc, tx, pid)
		if err != nil {
			fmt.Println(tx, "EXTRACT ERR:", err)
			continue
		}
		fmt.Printf("%s: order_id=%d side=%v price=%s amount=%s\n", tx, p.Order.OrderId, p.Order.BuyOrSell, p.Order.Price, p.Order.UnfilledAmount)
		fmt.Printf("  OrderCT=%s...\n", p.OrderCT[:40])
		fmt.Printf("  OpFund=%s...\n", p.OpFund[:40])
		fmt.Printf("  Creds=%s...\n", p.Creds[:40])
	}
}
