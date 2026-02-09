package main

import (
	"os"
	"testing"

	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/ingestion"
	"github.com/thenoobicat/featherengine/internal/matcher"
	"github.com/thenoobicat/featherengine/internal/orderbook"
	"github.com/thenoobicat/featherengine/internal/pusher"
)

func TestMainComponentsInitialization(t *testing.T) {
	// Set minimal required environment variables for config
	envVars := map[string]string{
		"PRIVATE_KEY":            "1234567890123456789012345678901234567890123456789012345678901234",
		"ALCHEMY_WS_URL":         "wss://test.alchemy.com/ws",
		"OPTION_MARKET_ADDRESS":  "0x1234567890123456789012345678901234567890",
		"ORDERBOOK_ADDRESS":      "0x1234567890123456789012345678901234567890",
		"MARGIN_ACCOUNT_ADDRESS": "0x1234567890123456789012345678901234567890",
	}

	// Set environment variables
	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	// Test config loading
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Config loading failed: %v", err)
	}

	// Test component initialization
	books := orderbook.NewBookManager()
	if books == nil {
		t.Error("BookManager initialization failed")
	}

	traders := ingestion.NewTraderTracker()
	if traders == nil {
		t.Error("TraderTracker initialization failed")
	}

	// Test matcher initialization (without real client)
	if cfg.WalletAddr.String() == "" {
		t.Error("Wallet address not set in config")
	}

	// Test that we can create the matcher (even with nil client for this test)
	m := matcher.New(cfg, nil, books)
	if m == nil {
		t.Error("Matcher initialization failed")
	}

	// Test liquidator initialization
	liq := matcher.NewLiquidator(cfg, nil, nil, traders)
	if liq == nil {
		t.Error("Liquidator initialization failed")
	}

	// Test event pusher initialization
	ep := pusher.New(cfg, nil, books)
	if ep == nil {
		t.Error("EventPusher initialization failed")
	}

	// Test listener initialization
	listener := ingestion.NewListener(cfg, nil, books, traders)
	if listener == nil {
		t.Error("Listener initialization failed")
	}
}

func TestMainFunctionStructure(t *testing.T) {
	// This test verifies that the main function structure is sound
	// We can't easily test the full main() due to external dependencies,
	// but we can test that the components it creates work

	envVars := map[string]string{
		"PRIVATE_KEY":            "1234567890123456789012345678901234567890123456789012345678901234",
		"ALCHEMY_WS_URL":         "wss://test.alchemy.com/ws",
		"OPTION_MARKET_ADDRESS":  "0x1234567890123456789012345678901234567890",
		"ORDERBOOK_ADDRESS":      "0x1234567890123456789012345678901234567890",
		"MARGIN_ACCOUNT_ADDRESS": "0x1234567890123456789012345678901234567890",
	}

	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Config loading failed: %v", err)
	}

	// Verify config has expected values
	if cfg.MatchInterval == 0 {
		t.Error("MatchInterval should be set")
	}

	if cfg.MaxMatchesPerTx == 0 {
		t.Error("MaxMatchesPerTx should be set")
	}

	if cfg.GasLimit == 0 {
		t.Error("GasLimit should be set")
	}
}

func BenchmarkComponentInitialization(b *testing.B) {
	envVars := map[string]string{
		"PRIVATE_KEY":            "0x1234567890123456789012345678901234567890123456789012345678901234",
		"ALCHEMY_WS_URL":         "wss://test.alchemy.com/ws",
		"OPTION_MARKET_ADDRESS":  "0x1234567890123456789012345678901234567890",
		"ORDERBOOK_ADDRESS":      "0x1234567890123456789012345678901234567890",
		"MARGIN_ACCOUNT_ADDRESS": "0x1234567890123456789012345678901234567890",
	}

	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg, _ := config.Load()
		books := orderbook.NewBookManager()
		traders := ingestion.NewTraderTracker()
		matcher.New(cfg, nil, books)
		matcher.NewLiquidator(cfg, nil, nil, traders)
		pusher.New(cfg, nil, books)
		ingestion.NewListener(cfg, nil, books, traders)
	}
}
