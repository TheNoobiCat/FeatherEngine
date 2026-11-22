package matcher

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/ingestion"
)

var optionMarketLiqABI abi.ABI

func init() {
	const liqABIJSON = `[
	  {
	    "inputs": [
	      {"name": "trader", "type": "address"},
	      {"name": "seriesId", "type": "bytes32"},
	      {"name": "priceUpdateData", "type": "bytes[]"}
	    ],
	    "name": "liquidatePosition",
	    "outputs": [],
	    "stateMutability": "nonpayable",
	    "type": "function"
	  },
	  {
	    "inputs": [
	      {"name": "trader", "type": "address"},
	      {"name": "seriesId", "type": "bytes32"},
	      {"name": "priceUpdateData", "type": "bytes[]"}
	    ],
	    "name": "settlePosition",
	    "outputs": [],
	    "stateMutability": "nonpayable",
	    "type": "function"
	  }
	]`
	var err error
	optionMarketLiqABI, err = abi.JSON(strings.NewReader(liqABIJSON))
	if err != nil {
		panic("failed to parse liquidation ABI: " + err.Error())
	}
}

// Liquidator scans all tracked short positions and liquidates undercollateralised ones.
// Also settles expired positions.
type Liquidator struct {
	cfg      *config.Config
	client   *ethclient.Client
	listener *ingestion.Listener
	traders  *ingestion.TraderTracker
}

func NewLiquidator(
	cfg *config.Config,
	client *ethclient.Client,
	listener *ingestion.Listener,
	traders *ingestion.TraderTracker,
) *Liquidator {
	return &Liquidator{
		cfg:      cfg,
		client:   client,
		listener: listener,
		traders:  traders,
	}
}

// Run starts the liquidation scanning loop. Blocks until ctx is cancelled.
func (liq *Liquidator) Run(ctx context.Context) {
	ticker := time.NewTicker(liq.cfg.LiquidationInterval)
	defer ticker.Stop()

	log.Printf("[liquidator] started — interval=%v", liq.cfg.LiquidationInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("[liquidator] shutting down")
			return
		case <-ticker.C:
			liq.scan(ctx)
		}
	}
}

func (liq *Liquidator) scan(ctx context.Context) {
	now := uint64(time.Now().Unix())
	knownSeries := liq.listener.KnownSeries()
	traders := liq.traders.AllTraders()

	for _, trader := range traders {
		// Get on-chain active series for this trader
		activeSeries, err := liq.listener.GetActiveSeriesForTrader(ctx, trader)
		if err != nil {
			continue
		}

		for _, seriesID := range activeSeries {
			series, known := knownSeries[seriesID]

			// Check for expired positions that need settlement
			if known && series.Expiry > 0 && series.Expiry <= now {
				liq.settlePosition(ctx, trader, seriesID)
				continue
			}

			// Check if position is liquidatable
			liquidatable, _, err := liq.listener.IsLiquidatable(ctx, trader, seriesID)
			if err != nil {
				continue
			}

			if liquidatable {
				log.Printf("[liquidator] LIQUIDATABLE: trader=%s series=%x",
					trader.Hex(), seriesID[:6])
				liq.liquidatePosition(ctx, trader, seriesID)
			}
		}
	}
}

func (liq *Liquidator) liquidatePosition(ctx context.Context, trader common.Address, seriesID [32]byte) {
	// PRICE DATA LIMITATION: this reference implementation sends an empty
	// priceUpdateData array, so the on-chain contract falls back to cached
	// Pyth prices (Oracle.getPriceNoUpdate). Stale cached prices can make
	// liquidations revert or execute at the wrong price during volatility.
	// Production deployments must fetch fresh Pyth update data (e.g. from
	// Hermes) and pass it here. No Pyth updater is included in this repo.
	emptyPriceData := make([][]byte, 0)

	callData, err := optionMarketLiqABI.Pack("liquidatePosition", trader, seriesID, emptyPriceData)
	if err != nil {
		log.Printf("[liquidator] pack error: %v", err)
		return
	}

	if err := liq.sendTx(ctx, callData, "liquidatePosition"); err != nil {
		log.Printf("[liquidator] liquidation tx failed for trader=%s series=%x: %v",
			trader.Hex(), seriesID[:6], err)
	}
}

func (liq *Liquidator) settlePosition(ctx context.Context, trader common.Address, seriesID [32]byte) {
	// Same stale-price limitation as liquidatePosition: settlement relies on
	// cached Pyth prices because priceUpdateData is empty. Production callers
	// should pass fresh Pyth update data.
	emptyPriceData := make([][]byte, 0)

	callData, err := optionMarketLiqABI.Pack("settlePosition", trader, seriesID, emptyPriceData)
	if err != nil {
		log.Printf("[liquidator] pack settle error: %v", err)
		return
	}

	if err := liq.sendTx(ctx, callData, "settlePosition"); err != nil {
		log.Printf("[liquidator] settlement tx failed for trader=%s series=%x: %v",
			trader.Hex(), seriesID[:6], err)
	}
}

func (liq *Liquidator) sendTx(ctx context.Context, callData []byte, label string) error {
	nonce, err := liq.client.PendingNonceAt(ctx, liq.cfg.WalletAddr)
	if err != nil {
		return fmt.Errorf("get nonce: %w", err)
	}

	gasPrice, err := liq.client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("suggest gas price: %w", err)
	}

	if gasPrice.Cmp(liq.cfg.MaxGasPriceWei) > 0 {
		return fmt.Errorf("gas price %s exceeds max", gasPrice.String())
	}

	tx := types.NewTransaction(
		nonce,
		liq.cfg.OptionMarketAddr,
		big.NewInt(0),
		liq.cfg.GasLimit,
		gasPrice,
		callData,
	)

	signer := types.NewEIP155Signer(liq.cfg.ChainID)
	signedTx, err := types.SignTx(tx, signer, liq.cfg.PrivateKey)
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}

	if err := liq.client.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("send tx: %w", err)
	}

	log.Printf("[liquidator] submitted %s tx=%s", label, signedTx.Hash().Hex())

	receipt, err := bind.WaitMined(ctx, liq.client, signedTx)
	if err != nil {
		return fmt.Errorf("wait mined: %w", err)
	}

	if receipt.Status == 0 {
		return fmt.Errorf("tx reverted: %s", signedTx.Hash().Hex())
	}

	log.Printf("[liquidator] %s confirmed tx=%s block=%d gas=%d",
		label, signedTx.Hash().Hex(), receipt.BlockNumber.Uint64(), receipt.GasUsed)

	return nil
}
