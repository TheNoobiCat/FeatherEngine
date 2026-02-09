package config

import (
	"os"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	// Set required environment variables
	envVars := map[string]string{
		"PRIVATE_KEY":             "1234567890123456789012345678901234567890123456789012345678901234",
		"ALCHEMY_WS_URL":          "wss://test.alchemy.com/ws",
		"OPTION_MARKET_ADDRESS":   "0x1234567890123456789012345678901234567890",
		"ORDERBOOK_ADDRESS":       "0x1234567890123456789012345678901234567890",
		"MARGIN_ACCOUNT_ADDRESS":  "0x1234567890123456789012345678901234567890",
		"CHAIN_ID":                "421614",
		"MATCH_INTERVAL_MS":       "500",
		"LIQUIDATION_INTERVAL_MS": "5000",
		"MAX_MATCHES_PER_TX":      "50",
		"GAS_LIMIT":               "3000000",
		"MAX_GAS_PRICE_GWEI":      "5",
	}

	// Set environment variables
	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify config values
	if cfg.WalletAddr.Hex() == "" {
		t.Error("WalletAddr should not be empty")
	}

	if cfg.AlchemyWSURL != "wss://test.alchemy.com/ws" {
		t.Errorf("AlchemyWSURL = %s, want wss://test.alchemy.com/ws", cfg.AlchemyWSURL)
	}

	if cfg.OptionMarketAddr.Hex() != "0x1234567890123456789012345678901234567890" {
		t.Errorf("OptionMarketAddr = %s", cfg.OptionMarketAddr.Hex())
	}

	if cfg.MatchInterval != 500000000 { // 500ms in nanoseconds
		t.Errorf("MatchInterval = %v, want 500ms", cfg.MatchInterval)
	}

	if cfg.MaxMatchesPerTx != 50 {
		t.Errorf("MaxMatchesPerTx = %d, want 50", cfg.MaxMatchesPerTx)
	}
}

func TestLoadMissingPrivateKey(t *testing.T) {
	// Clear PRIVATE_KEY
	os.Unsetenv("PRIVATE_KEY")

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail when PRIVATE_KEY is missing")
	}
}

func TestLoadInvalidPrivateKey(t *testing.T) {
	os.Setenv("PRIVATE_KEY", "invalid")
	defer os.Unsetenv("PRIVATE_KEY")

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail with invalid PRIVATE_KEY")
	}
}

func TestLoadMissingAlchemyURL(t *testing.T) {
	// Set minimal required vars except ALCHEMY_WS_URL
	os.Setenv("PRIVATE_KEY", "0x1234567890123456789012345678901234567890123456789012345678901234")
	os.Setenv("OPTION_MARKET_ADDRESS", "0x1234567890123456789012345678901234567890")
	os.Setenv("ORDERBOOK_ADDRESS", "0x1234567890123456789012345678901234567890")
	os.Setenv("MARGIN_ACCOUNT_ADDRESS", "0x1234567890123456789012345678901234567890")
	defer func() {
		os.Unsetenv("PRIVATE_KEY")
		os.Unsetenv("OPTION_MARKET_ADDRESS")
		os.Unsetenv("ORDERBOOK_ADDRESS")
		os.Unsetenv("MARGIN_ACCOUNT_ADDRESS")
	}()

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail when ALCHEMY_WS_URL is missing")
	}
}

func TestLoadDefaultValues(t *testing.T) {
	envVars := map[string]string{
		"PRIVATE_KEY":            "1234567890123456789012345678901234567890123456789012345678901234",
		"ALCHEMY_WS_URL":         "wss://test.alchemy.com/ws",
		"OPTION_MARKET_ADDRESS":  "0x1234567890123456789012345678901234567890",
		"ORDERBOOK_ADDRESS":      "0x1234567890123456789012345678901234567890",
		"MARGIN_ACCOUNT_ADDRESS": "0x1234567890123456789012345678901234567890",
		// Omit optional values to test defaults
	}

	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Check default values
	if cfg.ChainID.Int64() != 421614 {
		t.Errorf("ChainID default = %d, want 421614", cfg.ChainID.Int64())
	}

	if cfg.MatchInterval != 500000000 {
		t.Errorf("MatchInterval default = %v, want 500ms", cfg.MatchInterval)
	}

	if cfg.LiquidationInterval != 5000000000 {
		t.Errorf("LiquidationInterval default = %v, want 5000ms", cfg.LiquidationInterval)
	}

	if cfg.MaxMatchesPerTx != 50 {
		t.Errorf("MaxMatchesPerTx default = %d, want 50", cfg.MaxMatchesPerTx)
	}

	if cfg.GasLimit != 3000000 {
		t.Errorf("GasLimit default = %d, want 3000000", cfg.GasLimit)
	}
}

func TestGetEnvDefault(t *testing.T) {
	// Test with existing env var
	os.Setenv("TEST_VAR", "custom_value")
	defer os.Unsetenv("TEST_VAR")

	if got := getEnvDefault("TEST_VAR", "default"); got != "custom_value" {
		t.Errorf("getEnvDefault() = %s, want custom_value", got)
	}

	// Test with missing env var
	if got := getEnvDefault("MISSING_VAR", "default"); got != "default" {
		t.Errorf("getEnvDefault() = %s, want default", got)
	}
}
