package ingestion

import (
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Minimal ABI fragments for each contract — only the events and functions we need.

const orderbookABIJSON = `[
  {
    "anonymous": false,
    "inputs": [
      {"indexed": true, "name": "orderId", "type": "bytes32"},
      {"indexed": true, "name": "seriesId", "type": "bytes32"}
    ],
    "name": "LimitOrderCreated",
    "type": "event"
  },
  {
    "inputs": [
      {"name": "seriesId", "type": "bytes32"}
    ],
    "name": "getSeries",
    "outputs": [
      {"name": "underlyingPriceFeedId", "type": "bytes32"},
      {"name": "isCall", "type": "bool"},
      {"name": "strikePrice", "type": "uint256"},
      {"name": "expiry", "type": "uint256"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "seriesId", "type": "bytes32"},
      {"name": "orderId", "type": "bytes32"}
    ],
    "name": "orders",
    "outputs": [
      {"name": "trader", "type": "address"},
      {"name": "isLong", "type": "bool"},
      {"name": "limitPrice", "type": "uint256"},
      {"name": "quantity", "type": "uint256"},
      {"name": "marginLocked", "type": "uint256"},
      {"name": "intent", "type": "uint8"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {
        "components": [
          {"name": "seriesId", "type": "bytes32"},
          {"name": "takerId", "type": "bytes32"},
          {"name": "makerId", "type": "bytes32"}
        ],
        "name": "matches",
        "type": "tuple[]"
      }
    ],
    "name": "bulkMatch",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]`

const optionMarketABIJSON = `[
  {
    "inputs": [
      {"name": "", "type": "bytes32"}
    ],
    "name": "markets",
    "outputs": [
      {"name": "deployer", "type": "address"},
      {"name": "community", "type": "bool"},
      {"name": "totalOpenInterest", "type": "uint256"},
      {"name": "totalVolume", "type": "uint256"},
      {"name": "contractSizeInAsset", "type": "uint256"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "officialMarketCount",
    "outputs": [
      {"name": "", "type": "uint256"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "", "type": "uint256"}
    ],
    "name": "officialMarketIds",
    "outputs": [
      {"name": "", "type": "bytes32"}
    ],
    "stateMutability": "view",
    "type": "function"
  }
]`

const marginAccountABIJSON = `[
  {
    "inputs": [
      {"name": "trader", "type": "address"},
      {"name": "seriesId", "type": "bytes32"}
    ],
    "name": "positions",
    "outputs": [
      {"name": "seriesId", "type": "bytes32"},
      {"name": "quantity", "type": "uint256"},
      {"name": "entryPrice", "type": "uint256"},
      {"name": "isLong", "type": "bool"},
      {"name": "marginPerContract", "type": "uint256"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "trader", "type": "address"},
      {"name": "seriesId", "type": "bytes32"}
    ],
    "name": "isLiquidatable",
    "outputs": [
      {"name": "liquidatable", "type": "bool"},
      {"name": "currentOptionPrice", "type": "uint256"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [
      {"name": "trader", "type": "address"}
    ],
    "name": "getActiveSeries",
    "outputs": [
      {"name": "seriesIds", "type": "bytes32[]"}
    ],
    "stateMutability": "view",
    "type": "function"
  },
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

var (
	OrderbookABI     abi.ABI
	OptionMarketABI  abi.ABI
	MarginAccountABI abi.ABI
)

func init() {
	var err error
	OrderbookABI, err = abi.JSON(strings.NewReader(orderbookABIJSON))
	if err != nil {
		panic("failed to parse Orderbook ABI: " + err.Error())
	}
	OptionMarketABI, err = abi.JSON(strings.NewReader(optionMarketABIJSON))
	if err != nil {
		panic("failed to parse OptionMarket ABI: " + err.Error())
	}
	MarginAccountABI, err = abi.JSON(strings.NewReader(marginAccountABIJSON))
	if err != nil {
		panic("failed to parse MarginAccount ABI: " + err.Error())
	}
}
