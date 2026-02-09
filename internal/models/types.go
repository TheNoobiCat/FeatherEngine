package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// OrderIntent mirrors the Solidity enum Orderbook.OrderIntent
type OrderIntent uint8

const (
	BuyToOpen   OrderIntent = 0
	BuyToClose  OrderIntent = 1
	SellToOpen  OrderIntent = 2
	SellToClose OrderIntent = 3
)

// Order mirrors the on-chain Orderbook.Order struct
type Order struct {
	OrderID      [32]byte       // keccak256 order hash
	SeriesID     [32]byte       // series key
	Trader       common.Address // owner
	IsLong       bool           // buy side = true
	LimitPrice   *big.Int       // 18 decimals
	Quantity     *big.Int       // contract units
	MarginLocked *big.Int       // 18 decimals
	Intent       OrderIntent
}

// Series mirrors the on-chain Orderbook.Series struct
type Series struct {
	SeriesID              [32]byte
	UnderlyingPriceFeedID [32]byte
	IsCall                bool
	StrikePrice           *big.Int // 18 decimals
	Expiry                uint64   // unix timestamp
}

// Market mirrors on-chain OptionMarket.Market
type Market struct {
	PriceFeedID       [32]byte
	Deployer          common.Address
	Community         bool
	ContractSizeAsset *big.Int
}

// Match is the struct submitted to Orderbook.bulkMatch
type Match struct {
	SeriesID [32]byte
	TakerID  [32]byte
	MakerID  [32]byte
}

// Position mirrors on-chain MarginAccount.Position
type Position struct {
	SeriesID          [32]byte
	Quantity          *big.Int
	EntryPrice        *big.Int
	IsLong            bool
	MarginPerContract *big.Int
}
