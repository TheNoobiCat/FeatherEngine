package matcher

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"reflect"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/ingestion"
	"github.com/thenoobicat/featherengine/internal/models"
	"github.com/thenoobicat/featherengine/internal/orderbook"
)

// Matcher periodically scans for crossing orders and submits bulkMatch transactions.
type Matcher struct {
	cfg    *config.Config
	client *ethclient.Client
	books  *orderbook.BookManager
}

func New(cfg *config.Config, client *ethclient.Client, books *orderbook.BookManager) *Matcher {
	return &Matcher{
		cfg:    cfg,
		client: client,
		books:  books,
	}
}

// Run starts the event-driven matching loop. Blocks until ctx is cancelled.
func (m *Matcher) Run(ctx context.Context) {
	// Fallback ticker for safety (in case triggers are missed)
	ticker := time.NewTicker(m.cfg.MatchInterval)
	defer ticker.Stop()

	log.Printf("[matcher] started — event-driven with %v fallback, maxPerTx=%d", m.cfg.MatchInterval, m.cfg.MaxMatchesPerTx)

	// Single channel for match signals
	matchCh := make(chan struct{}, 1)

	// Single goroutine to aggregate all series triggers
	go func() {
		defer close(matchCh)
		triggers := m.books.GetAllMatchTriggers()

		// Use reflection to create a select case for each trigger
		cases := make([]reflect.SelectCase, len(triggers)+1)
		cases[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())}
		for i, trigger := range triggers {
			cases[i+1] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(trigger)}
		}

		for {
			chosen, _, ok := reflect.Select(cases)
			if !ok || chosen == 0 { // ctx.Done() or channel closed
				return
			}

			// Signal match attempt (non-blocking)
			select {
			case matchCh <- struct{}{}:
			default:
				// Channel full, match already pending
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("[matcher] shutting down")
			return
		case <-ticker.C:
			// Periodic fallback matching
			m.cycle(ctx)
		case <-matchCh:
			// Event-driven matching
			m.cycle(ctx)
		}
	}
}

func (m *Matcher) cycle(ctx context.Context) {
	matches := m.books.FindAllMatches(m.cfg.MaxMatchesPerTx)
	if len(matches) == 0 {
		return
	}

	log.Printf("[matcher] found %d matches, submitting bulkMatch", len(matches))

	// Batch into chunks of MaxMatchesPerTx
	for i := 0; i < len(matches); i += m.cfg.MaxMatchesPerTx {
		end := i + m.cfg.MaxMatchesPerTx
		if end > len(matches) {
			end = len(matches)
		}
		batch := matches[i:end]

		if err := m.submitBulkMatch(ctx, batch); err != nil {
			log.Printf("[matcher] bulkMatch tx failed: %v", err)
			// Put orders back? In practice, the next event replay will re-sync.
			// The on-chain state is the source of truth.
		}
	}
}

func (m *Matcher) submitBulkMatch(ctx context.Context, matches []models.Match) error {
	// Convert to ABI-compatible struct format
	type MatchTuple struct {
		SeriesId [32]byte
		TakerId  [32]byte
		MakerId  [32]byte
	}

	tuples := make([]MatchTuple, len(matches))
	for i, match := range matches {
		tuples[i] = MatchTuple{
			SeriesId: match.SeriesID,
			TakerId:  match.TakerID,
			MakerId:  match.MakerID,
		}
	}

	callData, err := ingestion.OrderbookABI.Pack("bulkMatch", tuples)
	if err != nil {
		return fmt.Errorf("pack bulkMatch: %w", err)
	}

	nonce, err := m.client.PendingNonceAt(ctx, m.cfg.WalletAddr)
	if err != nil {
		return fmt.Errorf("get nonce: %w", err)
	}

	gasPrice, err := m.client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("suggest gas price: %w", err)
	}

	// Cap gas price
	if gasPrice.Cmp(m.cfg.MaxGasPriceWei) > 0 {
		log.Printf("[matcher] gas price %s > max %s, skipping", gasPrice.String(), m.cfg.MaxGasPriceWei.String())
		return nil
	}

	tx := types.NewTransaction(
		nonce,
		m.cfg.OrderbookAddr,
		big.NewInt(0),
		m.cfg.GasLimit,
		gasPrice,
		callData,
	)

	signer := types.NewEIP155Signer(m.cfg.ChainID)
	signedTx, err := types.SignTx(tx, signer, m.cfg.PrivateKey)
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}

	if err := m.client.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("send tx: %w", err)
	}

	log.Printf("[matcher] submitted bulkMatch tx=%s matches=%d nonce=%d gas_price=%s",
		signedTx.Hash().Hex(), len(matches), nonce, gasPrice.String())

	// Wait for receipt
	receipt, err := bind.WaitMined(ctx, m.client, signedTx)
	if err != nil {
		return fmt.Errorf("wait mined: %w", err)
	}

	if receipt.Status == 0 {
		return fmt.Errorf("tx reverted: %s", signedTx.Hash().Hex())
	}

	log.Printf("[matcher] bulkMatch confirmed tx=%s block=%d gas_used=%d",
		signedTx.Hash().Hex(), receipt.BlockNumber.Uint64(), receipt.GasUsed)

	return nil
}
