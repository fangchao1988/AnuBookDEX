package anubis

// evm.go: Anubis Chain EVM 客户端 + 交易签名/广播/回执解析。
// 引入 go-ethereum 后提供真实 eth_sendRawTransaction → eth_getTransactionReceipt。

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

// EVMClient 封装 Anubis Chain EVM 交互的最小客户端。
type EVMClient struct {
	Client      *ethclient.Client
	ChainID     *big.Int
	PrivateKey  *ecdsa.PrivateKey // operator 结算私钥（secp256k1）
	From        ethcommon.Address   // operator EOA，msg.sender
	GasTipCap   *big.Int         // EIP-1559 小费上限
	GasFeeCap   *big.Int
	ReceiptWait time.Duration    // 回执轮询超时
}

// NewEVMClient 从 chain.anubis.* 配置构造 EVM 客户端。
// rpc-endpoint/HTTP 已实测可用（WS 不可用）；chain-id 缺省则链上 eth_chainId 回填。
func NewEVMClient(rpcEndpoint string) (*EVMClient, error) {
	if strings.TrimSpace(rpcEndpoint) == "" {
		return nil, errors.New("chain.anubis.rpc-endpoint is empty")
	}
	client, err := ethclient.Dial(rpcEndpoint)
	if err != nil {
		return nil, fmt.Errorf("dial anubis rpc: %w", err)
	}

	// chainID：配置优先，否则 eth_chainId 回填
	cid := big.NewInt(int64(config.GetInt("chain.anubis.chain-id", 0)))
	if cid.Sign() == 0 {
		if id, err := client.NetworkID(context.Background()); err == nil {
			cid = id
		} else {
			return nil, fmt.Errorf("resolve chain-id: %w", err)
		}
	}

	// operator 私钥（secp256k1）：metamask key 优先，回落通用 key
	privHex := operatorPrivateKey()
	if privHex == "" {
		return nil, errors.New("anubis operator private key empty (export ANUBIS_PRIVATE_KEY_METAMASK or ANUBIS_PRIVATE_KEY)")
	}
	priv, err := crypto.HexToECDSA(strings.TrimPrefix(privHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse operator private key: %w", err)
	}

	c := &EVMClient{
		Client:      client,
		ChainID:     cid,
		PrivateKey:  priv,
		From:        crypto.PubkeyToAddress(priv.PublicKey),
		GasTipCap:   big.NewInt(config.GetInt64("chain.anubis.gas-tip-cap-gwei", 2) * 1_000_000_000),
		GasFeeCap:   big.NewInt(config.GetInt64("chain.anubis.gas-fee-cap-gwei", 50) * 1_000_000_000),
		ReceiptWait: config.GetDuration("chain.anubis.receipt-timeout-s", 120) * time.Second,
	}
	common.Info("anubis evm client ready",
		"chain-id", cid.String(), "operator", c.From.Hex(), "rpc", rpcEndpoint)
	return c, nil
}

// SuggestGas 估算 gas，失败回退到静态上限。
func (c *EVMClient) SuggestGas(ctx context.Context, to ethcommon.Address, data []byte) uint64 {
	gas, err := c.Client.EstimateGas(ctx, ethereum.CallMsg{From: c.From, To: &to, Data: data})
	if err != nil {
		common.Warn("anubis evm: gas estimate failed; using static cap", "err", err)
		return 5_000_000
	}
	return gas * 12 / 10 // 留 20% 余裕
}

// SendTransaction 对 EIP-1559 交易签名、广播并等回执。
func (c *EVMClient) SendTransaction(ctx context.Context, to ethcommon.Address, data []byte, value *big.Int) (*types.Receipt, *types.Transaction, error) {
	if value == nil {
		value = big.NewInt(0)
	}
	nonce, err := c.Client.PendingNonceAt(ctx, c.From)
	if err != nil {
		return nil, nil, fmt.Errorf("pending nonce: %w", err)
	}
	gas := c.SuggestGas(ctx, to, data)

	signedTx, err := types.SignNewTx(c.PrivateKey, types.LatestSignerForChainID(c.ChainID), &types.DynamicFeeTx{
		To:        &to,
		Nonce:     nonce,
		Gas:       gas,
		GasFeeCap: c.GasFeeCap,
		GasTipCap: c.GasTipCap,
		Value:     value,
		Data:      data,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("sign tx: %w", err)
	}
	if err := c.Client.SendTransaction(ctx, signedTx); err != nil {
		return nil, nil, fmt.Errorf("broadcast: %w", err)
	}
	common.Info("anubis evm: tx broadcast", "hash", signedTx.Hash().Hex(), "nonce", nonce, "gas", gas)

	return c.waitForReceipt(ctx, signedTx)
}

// waitForReceipt 轮询 eth_getTransactionReceipt。
func (c *EVMClient) waitForReceipt(ctx context.Context, tx *types.Transaction) (*types.Receipt, *types.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, c.ReceiptWait)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	lastErr := error(nil)
	for {
		select {
		case <-ctx.Done():
			return nil, tx, fmt.Errorf("receipt timeout: %w", lastErr)
		case <-ticker.C:
			rec, err := c.Client.TransactionReceipt(ctx, tx.Hash())
			if err == nil && rec != nil {
				common.Info("anubis evm: txmined", "hash", tx.Hash().Hex(),
					"status", rec.Status, "block", rec.BlockNumber)
				return rec, tx, nil
			}
			lastErr = err
		}
	}
}

// CallContract 只读调用（eth_call），返回原始 returndata。
func (c *EVMClient) CallContract(ctx context.Context, to ethcommon.Address, data []byte) ([]byte, error) {
	return c.Client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
}

// GetLogs HTTP-polling eth_getLogs（WS 不可用，镜像 subscriber 轮询）。
func (c *EVMClient) GetLogs(ctx context.Context, fromBlock, toBlock uint64, address ethcommon.Address, topics [][]ethcommon.Hash) ([]types.Log, error) {
	q := ethereum.FilterQuery{FromBlock: big.NewInt(int64(fromBlock)), ToBlock: big.NewInt(int64(toBlock)), Addresses: []ethcommon.Address{address}}
	if len(topics) > 0 && len(topics[0]) > 0 {
		q.Topics = topics
	}
	return c.Client.FilterLogs(ctx, q)
}

// BlockNumber 当前链上高度。
func (c *EVMClient) BlockNumber(ctx context.Context) (uint64, error) {
	return c.Client.BlockNumber(ctx)
}
