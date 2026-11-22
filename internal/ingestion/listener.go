package ingestion

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/models"
	"github.com/thenoobicat/featherengine/internal/orderbook"
)

// Listener subscribes to on-chain events and populates the in-memory orderbook.
type Listener struct {
	cfg     *config.Config
	client  *ethclient.Client
	books   *orderbook.BookManager
	traders *TraderTracker

	// Track known series on-chain for liquidation scanning
	knownSeries   map[[32]byte]models.Series
	knownSeriesMu sync.RWMutex
}

// TraderTracker keeps track of traders with open positions across all series (for liquidation scanning).
type TraderTracker struct {
	mu      sync.RWMutex
	traders map[common.Address]map[[32]byte]struct{} // trader → set of seriesIDs
}

func NewTraderTracker() *TraderTracker {
	return &TraderTracker{
		traders: make(map[common.Address]map[[32]byte]struct{}),
	}
}

func (tt *TraderTracker) Track(trader common.Address, seriesID [32]byte) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if tt.traders[trader] == nil {
		tt.traders[trader] = make(map[[32]byte]struct{})
	}
	tt.traders[trader][seriesID] = struct{}{}
}

func (tt *TraderTracker) AllTraders() []common.Address {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	addrs := make([]common.Address, 0, len(tt.traders))
	for addr := range tt.traders {
		addrs = append(addrs, addr)
	}
	return addrs
}

func (tt *TraderTracker) SeriesFor(trader common.Address) [][32]byte {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	set := tt.traders[trader]
	ids := make([][32]byte, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids
}

func NewListener(cfg *config.Config, client *ethclient.Client, books *orderbook.BookManager, traders *TraderTracker) *Listener {
	return &Listener{
		cfg:         cfg,
		client:      client,
		books:       books,
		traders:     traders,
		knownSeries: make(map[[32]byte]models.Series),
	}
}

func (l *Listener) KnownSeries() map[[32]byte]models.Series {
	l.knownSeriesMu.RLock()
	defer l.knownSeriesMu.RUnlock()
	cp := make(map[[32]byte]models.Series, len(l.knownSeries))
	for k, v := range l.knownSeries {
		cp[k] = v
	}
	return cp
}

// Start subscribes to LimitOrderCreated events and processes them in real-time.
// Includes automatic reconnection with exponential backoff if the WebSocket subscription drops.
func (l *Listener) Start(ctx context.Context) error {
	go l.subscribeWithReconnect(ctx)
	return nil
}

// subscribeWithReconnect manages the WebSocket subscription lifecycle with exponential backoff.
func (l *Listener) subscribeWithReconnect(ctx context.Context) {
	const (
		initialBackoff = 1 * time.Second
		maxBackoff     = 60 * time.Second
		backoffFactor  = 2
	)

	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := l.subscribe(ctx)
		if err == nil {
			// subscription ran and ended cleanly (context cancelled)
			return
		}

		log.Printf("[ingestion] subscription ended: %v — reconnecting in %s", err, backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff with cap
		backoff *= time.Duration(backoffFactor)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// subscribe creates a single subscription session. Returns nil on clean context cancellation,
// or an error if the subscription dropped unexpectedly.
func (l *Listener) subscribe(ctx context.Context) error {
	orderCreatedSig := OrderbookABI.Events["LimitOrderCreated"].ID
	orderCancelledSig := OrderbookABI.Events["LimitOrderCancelled"].ID

	query := ethereum.FilterQuery{
		Addresses: []common.Address{l.cfg.OrderbookAddr},
		Topics:    [][]common.Hash{{orderCreatedSig, orderCancelledSig}},
	}

	logCh := make(chan types.Log, 256)
	sub, err := l.client.SubscribeFilterLogs(ctx, query, logCh)
	if err != nil {
		return fmt.Errorf("subscribe order events: %w", err)
	}

	log.Printf("[ingestion] subscribed to LimitOrderCreated and LimitOrderCancelled on %s", l.cfg.OrderbookAddr.Hex())

	defer sub.Unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-sub.Err():
			return fmt.Errorf("subscription error: %w", err)
		case vLog := <-logCh:
			if len(vLog.Topics) == 0 {
				continue
			}
			eventSig := vLog.Topics[0]
			if eventSig == orderCreatedSig {
				l.handleOrderCreated(ctx, vLog)
			} else if eventSig == orderCancelledSig {
				l.handleOrderCancelled(ctx, vLog)
			}
		}
	}
}

func (l *Listener) handleOrderCreated(ctx context.Context, vLog types.Log) {
	if len(vLog.Topics) < 3 {
		return
	}
	orderID := vLog.Topics[1]
	seriesID := vLog.Topics[2]

	var oid, sid [32]byte
	copy(oid[:], orderID[:])
	copy(sid[:], seriesID[:])

	// Fetch order data from chain
	order, err := l.fetchOrder(ctx, sid, oid)
	if err != nil {
		log.Printf("[ingestion] failed to fetch order %x: %v", oid[:8], err)
		return
	}
	if order == nil {
		return // order was already filled or cancelled
	}

	// Ensure we know the series
	l.ensureSeries(ctx, sid)

	// Track trader for liquidation
	l.traders.Track(order.Trader, sid)

	// Insert into in-memory orderbook
	l.books.AddOrder(order)
	log.Printf("[ingestion] new order %x series=%x long=%v price=%s qty=%s",
		oid[:6], sid[:6], order.IsLong, order.LimitPrice.String(), order.Quantity.String())
}

func (l *Listener) handleOrderCancelled(ctx context.Context, vLog types.Log) {
	if len(vLog.Topics) < 3 {
		return
	}
	orderID := vLog.Topics[1]
	seriesID := vLog.Topics[2]

	var oid, sid [32]byte
	copy(oid[:], orderID[:])
	copy(sid[:], seriesID[:])

	// We need to know if the order was long or short to remove from correct side
	// Fetch order first to get isLong (if it still exists on-chain, it won't after cancel)
	// Actually, after cancel the order no longer exists on-chain, so we need to track
	// isLong in memory or try both sides. Let's try both sides (simpler).

	removed := l.books.RemoveOrder(sid, oid, true) // Try bid side
	if !removed {
		removed = l.books.RemoveOrder(sid, oid, false) // Try ask side
	}

	if removed {
		log.Printf("[ingestion] cancelled order %x series=%x", oid[:6], sid[:6])
	} else {
		log.Printf("[ingestion] cancel event for unknown order %x series=%x", oid[:6], sid[:6])
	}
}

func (l *Listener) fetchOrder(ctx context.Context, seriesID, orderID [32]byte) (*models.Order, error) {
	callData, err := OrderbookABI.Pack("orders", seriesID, orderID)
	if err != nil {
		return nil, err
	}

	result, err := l.client.CallContract(ctx, ethereum.CallMsg{
		To:   &l.cfg.OrderbookAddr,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, err
	}

	outputs, err := OrderbookABI.Unpack("orders", result)
	if err != nil {
		return nil, err
	}

	trader := outputs[0].(common.Address)
	if trader == (common.Address{}) {
		return nil, nil // deleted order
	}

	isLong := outputs[1].(bool)
	limitPrice := outputs[2].(*big.Int)
	quantity := outputs[3].(*big.Int)
	marginLocked := outputs[4].(*big.Int)
	intent := outputs[5].(uint8)
	isLiquidation := outputs[6].(bool)

	return &models.Order{
		OrderID:       orderID,
		SeriesID:      seriesID,
		Trader:        trader,
		IsLong:        isLong,
		LimitPrice:    new(big.Int).Set(limitPrice),
		Quantity:      new(big.Int).Set(quantity),
		MarginLocked:  new(big.Int).Set(marginLocked),
		Intent:        models.OrderIntent(intent),
		IsLiquidation: isLiquidation,
	}, nil
}

func (l *Listener) ensureSeries(ctx context.Context, seriesID [32]byte) {
	l.knownSeriesMu.RLock()
	if _, ok := l.knownSeries[seriesID]; ok {
		l.knownSeriesMu.RUnlock()
		return
	}
	l.knownSeriesMu.RUnlock()

	callData, err := OrderbookABI.Pack("getSeries", seriesID)
	if err != nil {
		return
	}

	result, err := l.client.CallContract(ctx, ethereum.CallMsg{
		To:   &l.cfg.OrderbookAddr,
		Data: callData,
	}, nil)
	if err != nil {
		return
	}

	outputs, err := OrderbookABI.Unpack("getSeries", result)
	if err != nil {
		return
	}

	var feedID [32]byte
	feedID = outputs[0].([32]byte)

	series := models.Series{
		SeriesID:              seriesID,
		UnderlyingPriceFeedID: feedID,
		IsCall:                outputs[1].(bool),
		StrikePrice:           outputs[2].(*big.Int),
		Expiry:                outputs[3].(*big.Int).Uint64(),
		ContractSize:          new(big.Int).Set(outputs[4].(*big.Int)),
	}

	l.knownSeriesMu.Lock()
	l.knownSeries[seriesID] = series
	l.knownSeriesMu.Unlock()
}

// FetchExistingOrders replays past LimitOrderCreated and LimitOrderCancelled events to populate the book on startup.
func (l *Listener) FetchExistingOrders(ctx context.Context, fromBlock uint64) error {
	orderCreatedSig := OrderbookABI.Events["LimitOrderCreated"].ID
	orderCancelledSig := OrderbookABI.Events["LimitOrderCancelled"].ID

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		Addresses: []common.Address{l.cfg.OrderbookAddr},
		Topics:    [][]common.Hash{{orderCreatedSig, orderCancelledSig}},
	}

	logs, err := l.client.FilterLogs(ctx, query)
	if err != nil {
		return fmt.Errorf("filter past order events: %w", err)
	}

	log.Printf("[ingestion] replaying %d historical order events from block %d", len(logs), fromBlock)

	for _, vLog := range logs {
		if len(vLog.Topics) == 0 {
			continue
		}
		eventSig := vLog.Topics[0]
		if eventSig == orderCreatedSig {
			l.handleOrderCreated(ctx, vLog)
		} else if eventSig == orderCancelledSig {
			l.handleOrderCancelled(ctx, vLog)
		}
	}

	return nil
}

// IsLiquidatable calls the on-chain isLiquidatable view function
func (l *Listener) IsLiquidatable(ctx context.Context, trader common.Address, seriesID [32]byte) (bool, *big.Int, error) {
	callData, err := MarginAccountABI.Pack("isLiquidatable", trader, seriesID)
	if err != nil {
		return false, nil, err
	}

	result, err := l.client.CallContract(ctx, ethereum.CallMsg{
		To:   &l.cfg.MarginAccountAddr,
		Data: callData,
	}, nil)
	if err != nil {
		return false, nil, err
	}

	outputs, err := MarginAccountABI.Unpack("isLiquidatable", result)
	if err != nil {
		return false, nil, err
	}

	liquidatable := outputs[0].(bool)
	optionPrice := outputs[1].(*big.Int)
	return liquidatable, optionPrice, nil
}

// GetActiveSeriesForTrader fetches on-chain active series for a trader
func (l *Listener) GetActiveSeriesForTrader(ctx context.Context, trader common.Address) ([][32]byte, error) {
	callData, err := MarginAccountABI.Pack("getActiveSeries", trader)
	if err != nil {
		return nil, err
	}

	result, err := l.client.CallContract(ctx, ethereum.CallMsg{
		To:   &l.cfg.MarginAccountAddr,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, err
	}

	outputs, err := MarginAccountABI.Unpack("getActiveSeries", result)
	if err != nil {
		return nil, err
	}

	rawSeries := outputs[0].([][32]byte)
	return rawSeries, nil
}

// CallerOpts returns a no-op call opts for view functions
func CallerOpts() *bind.CallOpts {
	return &bind.CallOpts{Pending: false}
}
