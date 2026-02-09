package config

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/joho/godotenv"
)

type Config struct {
	PrivateKey   *ecdsa.PrivateKey
	WalletAddr   common.Address
	AlchemyWSURL string

	OptionMarketAddr  common.Address
	OrderbookAddr     common.Address
	MarginAccountAddr common.Address

	ChainID *big.Int

	MatchInterval       time.Duration
	LiquidationInterval time.Duration
	MaxMatchesPerTx     int

	GasLimit       uint64
	MaxGasPriceWei *big.Int
}

func Load() (*Config, error) {
	_ = godotenv.Load() // best-effort .env load

	privHex := os.Getenv("PRIVATE_KEY")
	if privHex == "" {
		return nil, fmt.Errorf("PRIVATE_KEY not set")
	}
	privateKey, err := crypto.HexToECDSA(privHex)
	if err != nil {
		return nil, fmt.Errorf("invalid PRIVATE_KEY: %w", err)
	}
	walletAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	wsURL := os.Getenv("ALCHEMY_WS_URL")
	if wsURL == "" {
		return nil, fmt.Errorf("ALCHEMY_WS_URL not set")
	}

	optionMarket := os.Getenv("OPTION_MARKET_ADDRESS")
	if optionMarket == "" {
		return nil, fmt.Errorf("OPTION_MARKET_ADDRESS not set")
	}
	orderbookAddr := os.Getenv("ORDERBOOK_ADDRESS")
	if orderbookAddr == "" {
		return nil, fmt.Errorf("ORDERBOOK_ADDRESS not set")
	}
	marginAccountAddr := os.Getenv("MARGIN_ACCOUNT_ADDRESS")
	if marginAccountAddr == "" {
		return nil, fmt.Errorf("MARGIN_ACCOUNT_ADDRESS not set")
	}

	chainID, _ := strconv.ParseInt(getEnvDefault("CHAIN_ID", "421614"), 10, 64)
	matchIntervalMs, _ := strconv.Atoi(getEnvDefault("MATCH_INTERVAL_MS", "500"))
	liqIntervalMs, _ := strconv.Atoi(getEnvDefault("LIQUIDATION_INTERVAL_MS", "5000"))
	maxMatches, _ := strconv.Atoi(getEnvDefault("MAX_MATCHES_PER_TX", "50"))
	gasLimit, _ := strconv.ParseUint(getEnvDefault("GAS_LIMIT", "3000000"), 10, 64)
	maxGasGwei, _ := strconv.ParseInt(getEnvDefault("MAX_GAS_PRICE_GWEI", "5"), 10, 64)

	return &Config{
		PrivateKey:          privateKey,
		WalletAddr:          walletAddr,
		AlchemyWSURL:        wsURL,
		OptionMarketAddr:    common.HexToAddress(optionMarket),
		OrderbookAddr:       common.HexToAddress(orderbookAddr),
		MarginAccountAddr:   common.HexToAddress(marginAccountAddr),
		ChainID:             big.NewInt(chainID),
		MatchInterval:       time.Duration(matchIntervalMs) * time.Millisecond,
		LiquidationInterval: time.Duration(liqIntervalMs) * time.Millisecond,
		MaxMatchesPerTx:     maxMatches,
		GasLimit:            gasLimit,
		MaxGasPriceWei:      new(big.Int).Mul(big.NewInt(maxGasGwei), big.NewInt(1e9)),
	}, nil
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
