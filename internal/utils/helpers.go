package utils

import (
	"context"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/ethclient"
)

// LatestBlock returns the current block number
func LatestBlock(ctx context.Context, client *ethclient.Client) (uint64, error) {
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return 0, err
	}
	return header.Number.Uint64(), nil
}

// SafeStartBlock computes a reasonable start block for historical replay
// (e.g., ~24h ago assuming ~250ms blocks on Arbitrum)
func SafeStartBlock(ctx context.Context, client *ethclient.Client, lookbackBlocks uint64) uint64 {
	latest, err := LatestBlock(ctx, client)
	if err != nil {
		log.Printf("[utils] failed to get latest block: %v, using 0", err)
		return 0
	}
	if latest > lookbackBlocks {
		return latest - lookbackBlocks
	}
	return 0
}

// Wei18 converts a human-readable value to 18-decimal wei
func Wei18(val float64) *big.Int {
	f := new(big.Float).SetFloat64(val)
	f.Mul(f, new(big.Float).SetFloat64(1e18))
	result, _ := f.Int(nil)
	return result
}
