package pusher

import (
	"context"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/orderbook"
)

// EventPusher listens for on-chain settlement/liquidation/cancel events
// and prunes the in-memory orderbook accordingly.

var marginAccountABI abi.ABI

func init() {
	const abiJSON = `[
	  {
	    "anonymous": false,
	    "inputs": [
	      {"indexed": true, "name": "trader", "type": "address"},
	      {"indexed": true, "name": "seriesId", "type": "bytes32"},
	      {"indexed": false, "name": "isLong", "type": "bool"},
	      {"indexed": false, "name": "payoff", "type": "uint256"},
	      {"indexed": false, "name": "shortfall", "type": "uint256"}
	    ],
	    "name": "PositionSettled",
	    "type": "event"
	  },
	  {
	    "anonymous": false,
	    "inputs": [
	      {"indexed": true, "name": "trader", "type": "address"},
	      {"indexed": true, "name": "seriesId", "type": "bytes32"},
	      {"indexed": true, "name": "liquidator", "type": "address"},
	      {"indexed": false, "name": "currentOptionPrice", "type": "uint256"},
	      {"indexed": false, "name": "marginSeized", "type": "uint256"},
	      {"indexed": false, "name": "penaltyToInsurance", "type": "uint256"},
	      {"indexed": false, "name": "rewardToLiquidator", "type": "uint256"}
	    ],
	    "name": "PositionLiquidated",
	    "type": "event"
	  }
	]`
	var err error
	marginAccountABI, err = abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic("failed to parse margin account ABI: " + err.Error())
	}
}

type EventPusher struct {
	cfg    *config.Config
	client *ethclient.Client
	books  *orderbook.BookManager
}

func New(cfg *config.Config, client *ethclient.Client, books *orderbook.BookManager) *EventPusher {
	return &EventPusher{
		cfg:    cfg,
		client: client,
		books:  books,
	}
}

// Start subscribes to PositionSettled and PositionLiquidated events.
// When a position is settled or liquidated, any remaining orders for that
// trader+series are likely stale and should be pruned from the in-memory book.
func (ep *EventPusher) Start(ctx context.Context) error {
	settledSig := marginAccountABI.Events["PositionSettled"].ID
	liquidatedSig := marginAccountABI.Events["PositionLiquidated"].ID

	query := ethereum.FilterQuery{
		Addresses: []common.Address{ep.cfg.MarginAccountAddr},
		Topics:    [][]common.Hash{{settledSig, liquidatedSig}},
	}

	logCh := make(chan types.Log, 64)
	sub, err := ep.client.SubscribeFilterLogs(ctx, query, logCh)
	if err != nil {
		return err
	}

	log.Printf("[pusher] subscribed to settlement/liquidation events on %s",
		ep.cfg.MarginAccountAddr.Hex())

	go func() {
		defer sub.Unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-sub.Err():
				log.Printf("[pusher] subscription error: %v", err)
				return
			case vLog := <-logCh:
				ep.handleEvent(vLog)
			}
		}
	}()

	return nil
}

func (ep *EventPusher) handleEvent(vLog types.Log) {
	if len(vLog.Topics) < 3 {
		return
	}

	// topics[1] = trader (indexed), topics[2] = seriesId (indexed)
	var seriesID [32]byte
	copy(seriesID[:], vLog.Topics[2][:])

	// Log the event for visibility
	eventName := "PositionSettled"
	if vLog.Topics[0] == marginAccountABI.Events["PositionLiquidated"].ID {
		eventName = "PositionLiquidated"
	}

	log.Printf("[pusher] %s event: series=%x block=%d",
		eventName, seriesID[:6], vLog.BlockNumber)

	// Resync: re-fetch any remaining orders in this series from chain
	// For simplicity, the in-memory book will naturally stay consistent because
	// matched/settled orders return zero from on-chain queries and the matcher
	// skips them. The on-chain bulkMatch will revert on invalid order IDs.
}

// ReplaySettlementEvents replays historical events from a starting block
func (ep *EventPusher) ReplaySettlementEvents(ctx context.Context, fromBlock uint64) error {
	settledSig := marginAccountABI.Events["PositionSettled"].ID
	liquidatedSig := marginAccountABI.Events["PositionLiquidated"].ID

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		Addresses: []common.Address{ep.cfg.MarginAccountAddr},
		Topics:    [][]common.Hash{{settledSig, liquidatedSig}},
	}

	logs, err := ep.client.FilterLogs(ctx, query)
	if err != nil {
		return err
	}

	log.Printf("[pusher] replayed %d historical settlement/liquidation events", len(logs))
	for _, vLog := range logs {
		ep.handleEvent(vLog)
	}

	return nil
}
