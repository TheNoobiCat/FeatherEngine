package pusher

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

func TestNew(t *testing.T) {
	cfg := createTestConfig()
	books := orderbook.NewBookManager()

	pusher := New(cfg, nil, books)

	if pusher.cfg != cfg {
		t.Error("EventPusher cfg not set correctly")
	}

	if pusher.books != books {
		t.Error("EventPusher books not set correctly")
	}
}

func TestEventPusherStart(t *testing.T) {
	t.Skip("Start test requires real Ethereum client")
}

func TestEventPusherReplaySettlementEvents(t *testing.T) {
	t.Skip("ReplaySettlementEvents test requires real Ethereum client")
}
