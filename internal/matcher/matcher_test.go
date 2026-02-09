package matcher

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/ingestion"
	"github.com/thenoobicat/featherengine/internal/models"
	"github.com/thenoobicat/featherengine/internal/orderbook"
)

func createTestConfig() *config.Config {
	return &config.Config{
		PrivateKey:          nil, // Not needed for these tests
		WalletAddr:          common.HexToAddress("0x1234567890123456789012345678901234567890"),
		AlchemyWSURL:        "wss://test.alchemy.com/ws",
		OptionMarketAddr:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
		OrderbookAddr:       common.HexToAddress("0x1234567890123456789012345678901234567890"),
		MarginAccountAddr:   common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID:             big.NewInt(421614),
		MatchInterval:       100 * time.Millisecond, // Fast for testing
		LiquidationInterval: 500 * time.Millisecond,
		MaxMatchesPerTx:     10,
		GasLimit:            3000000,
		MaxGasPriceWei:      big.NewInt(50000000000), // 50 gwei
	}
}

func TestNewMatcher(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()

	m := New(cfg, nil, books) // Use nil client for testing

	if m.cfg != cfg {
		t.Error("Matcher cfg not set correctly")
	}

	if m.books != books {
		t.Error("Matcher books not set correctly")
	}
}

func TestMatcherRun(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()

	m := New(cfg, nil, books)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start matcher
	start := time.Now()
	go m.Run(ctx)

	// Wait for context to timeout
	<-ctx.Done()

	elapsed := time.Since(start)
	if elapsed < 400*time.Millisecond || elapsed > 600*time.Millisecond {
		t.Errorf("Matcher run duration = %v, want ~500ms", elapsed)
	}
}

func TestMatcherCycle(t *testing.T) {
	// Skip the cycle test since it requires a real client
	t.Skip("cycle() test requires real Ethereum client to avoid nil pointer panic")
}

func TestMatcherSubmitBulkMatch(t *testing.T) {
	t.Skip("submitBulkMatch() test requires real Ethereum client")
}

// Mock types for testing liquidator
type mockListener struct{}

func (m *mockListener) KnownSeries() map[[32]byte]models.Series {
	return map[[32]byte]models.Series{
		[32]byte{1}: {
			SeriesID: [32]byte{1},
			Expiry:   uint64(time.Now().Add(24 * time.Hour).Unix()),
		},
	}
}

func (m *mockListener) GetActiveSeriesForTrader(ctx context.Context, trader common.Address) ([][32]byte, error) {
	return [][32]byte{{1}}, nil
}

func (m *mockListener) IsLiquidatable(ctx context.Context, trader common.Address, seriesID [32]byte) (bool, interface{}, error) {
	return false, nil, nil
}

type mockTraderTracker struct{}

func (m *mockTraderTracker) AllTraders() []common.Address {
	return []common.Address{common.HexToAddress("0x1234567890123456789012345678901234567890")}
}

func TestNewLiquidator(t *testing.T) {
	cfg := createTestConfig()

	// Create real instances instead of mocks
	books := orderbook.NewBookManager()
	traders := ingestion.NewTraderTracker()
	listener := ingestion.NewListener(cfg, nil, books, traders)

	liq := NewLiquidator(cfg, nil, listener, traders)

	if liq.cfg != cfg {
		t.Error("Liquidator cfg not set correctly")
	}
}

func TestLiquidatorRun(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()
	traders := ingestion.NewTraderTracker()
	listener := ingestion.NewListener(cfg, nil, books, traders)

	liq := NewLiquidator(cfg, nil, listener, traders)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	go liq.Run(ctx)

	<-ctx.Done()

	elapsed := time.Since(start)
	if elapsed < 150*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Errorf("Liquidator run duration = %v, want ~200ms", elapsed)
	}
}

func TestLiquidatorScan(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()
	traders := ingestion.NewTraderTracker()
	listener := ingestion.NewListener(cfg, nil, books, traders)

	liq := NewLiquidator(cfg, nil, listener, traders)

	ctx := context.Background()
	// This should run without panicking
	liq.scan(ctx)
}
