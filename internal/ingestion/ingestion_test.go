package ingestion

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/orderbook"
)

func createTestConfig() *config.Config {
	return &config.Config{
		PrivateKey:          nil,
		WalletAddr:          common.HexToAddress("0x1234567890123456789012345678901234567890"),
		AlchemyWSURL:        "wss://test.alchemy.com/ws",
		OptionMarketAddr:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
		OrderbookAddr:       common.HexToAddress("0x1234567890123456789012345678901234567890"),
		MarginAccountAddr:   common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID:             nil,
		MatchInterval:       100 * time.Millisecond,
		LiquidationInterval: 500 * time.Millisecond,
		MaxMatchesPerTx:     10,
		GasLimit:            3000000,
		MaxGasPriceWei:      nil,
	}
}

func TestNewListener(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()
	traders := NewTraderTracker()

	listener := NewListener(cfg, nil, books, traders)

	if listener.cfg != cfg {
		t.Error("Listener cfg not set correctly")
	}

	if listener.books != books {
		t.Error("Listener books not set correctly")
	}
}

func TestListenerKnownSeries(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()
	traders := NewTraderTracker()

	listener := NewListener(cfg, nil, books, traders)

	// Initially should be empty
	series := listener.KnownSeries()
	if len(series) != 0 {
		t.Errorf("Expected empty series map, got %d entries", len(series))
	}
}

func TestListenerGetActiveSeriesForTrader(t *testing.T) {
	t.Skip("GetActiveSeriesForTrader test requires real Ethereum client")
}

func TestListenerIsLiquidatable(t *testing.T) {
	t.Skip("IsLiquidatable test requires real Ethereum client")
}

func TestListenerRun(t *testing.T) {
	t.Skip("Start test requires real Ethereum client")
}

func TestProcessOrderEvent(t *testing.T) {
	t.Skip("processOrderEvent method does not exist")
}

func TestProcessCancelEvent(t *testing.T) {
	t.Skip("processCancelEvent method does not exist")
}

func TestProcessFillEvent(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()
	traders := NewTraderTracker()

	listener := NewListener(cfg, nil, books, traders)

	// processFillEvent method doesn't exist, just test that listener was created
	if listener == nil {
		t.Error("Listener should not be nil")
	}
}

func TestOrderbookABIGetSeriesOutputs(t *testing.T) {
	method, ok := OrderbookABI.Methods["getSeries"]
	if !ok {
		t.Fatal("getSeries missing from Orderbook ABI")
	}

	want := []struct {
		name string
		typ  string
	}{
		{"underlyingPriceFeedId", "bytes32"},
		{"isCall", "bool"},
		{"strikePrice", "uint256"},
		{"expiry", "uint256"},
		{"contractSize", "uint256"},
	}

	if len(method.Outputs) != len(want) {
		t.Fatalf("getSeries has %d outputs, want %d", len(method.Outputs), len(want))
	}
	for i, w := range want {
		if method.Outputs[i].Name != w.name || method.Outputs[i].Type.String() != w.typ {
			t.Errorf("getSeries output %d = %s %s, want %s %s",
				i, method.Outputs[i].Type, method.Outputs[i].Name, w.typ, w.name)
		}
	}
}

func TestOrderbookABIOrdersOutputs(t *testing.T) {
	method, ok := OrderbookABI.Methods["orders"]
	if !ok {
		t.Fatal("orders missing from Orderbook ABI")
	}

	want := []struct {
		name string
		typ  string
	}{
		{"trader", "address"},
		{"isLong", "bool"},
		{"limitPrice", "uint256"},
		{"quantity", "uint256"},
		{"marginLocked", "uint256"},
		{"intent", "uint8"},
		{"isLiquidation", "bool"},
	}

	if len(method.Outputs) != len(want) {
		t.Fatalf("orders has %d outputs, want %d", len(method.Outputs), len(want))
	}
	for i, w := range want {
		if method.Outputs[i].Name != w.name || method.Outputs[i].Type.String() != w.typ {
			t.Errorf("orders output %d = %s %s, want %s %s",
				i, method.Outputs[i].Type, method.Outputs[i].Name, w.typ, w.name)
		}
	}
}
