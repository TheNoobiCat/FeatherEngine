package matcher

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"reflect"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/ingestion"
	"github.com/thenoobicat/featherengine/internal/models"
	"github.com/thenoobicat/featherengine/internal/orderbook"
	"github.com/thenoobicat/featherengine/internal/revert"
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

	// Pre-flight simulation via eth_call to catch reverts before spending gas
	simMsg := ethereum.CallMsg{
		From: m.cfg.WalletAddr,
		To:   &m.cfg.OrderbookAddr,
		Gas:  m.cfg.GasLimit,
		Data: callData,
	}
	if _, err := m.client.CallContract(ctx, simMsg, nil); err != nil {
		reason := revert.ExtractRevertReason(err)
		return fmt.Errorf("simulation reverted: %s", reason)
	}

	nonce, err := m.client.PendingNonceAt(ctx, m.cfg.WalletAddr)
	if err != nil {
		return fmt.Errorf("get nonce: %w", err)
	}

	// Get base fee from latest block for EIP-1559
	header, err := m.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return fmt.Errorf("get latest header: %w", err)
	}
	baseFee := header.BaseFee
	if baseFee == nil {
		baseFee = big.NewInt(100000000) // 0.1 gwei fallback
	}

	// Suggest priority fee (tip)
	tipCap, err := m.client.SuggestGasTipCap(ctx)
	if err != nil {
		log.Printf("[matcher] SuggestGasTipCap failed: %v, using default 0.01 gwei", err)
		tipCap = big.NewInt(10000000) // 0.01 gwei
	}

	// EIP-1559: maxFeePerGas = (2 * baseFee) + maxPriorityFeePerGas
	// This allows for baseFee to double in the next block
	maxFeePerGas := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(2)),
		tipCap,
	)

	// Cap max fee to config limit
	if maxFeePerGas.Cmp(m.cfg.MaxGasPriceWei) > 0 {
		log.Printf("[matcher] maxFeePerGas %s > max %s, skipping", maxFeePerGas.String(), m.cfg.MaxGasPriceWei.String())
		return nil
	}

	// Create EIP-1559 transaction
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   m.cfg.ChainID,
		Nonce:     nonce,
		GasTipCap: tipCap,
		GasFeeCap: maxFeePerGas,
		Gas:       m.cfg.GasLimit,
		To:        &m.cfg.OrderbookAddr,
		Value:     big.NewInt(0),
		Data:      callData,
	})

	signer := types.LatestSignerForChainID(m.cfg.ChainID)
	signedTx, err := types.SignTx(tx, signer, m.cfg.PrivateKey)
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}

	if err := m.client.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("send tx: %w", err)
	}

	log.Printf("[matcher] submitted bulkMatch tx=%s matches=%d nonce=%d maxFee=%s tip=%s baseFee=%s",
		signedTx.Hash().Hex(), len(matches), nonce, maxFeePerGas.String(), tipCap.String(), baseFee.String())

	// Wait for receipt
	receipt, err := bind.WaitMined(ctx, m.client, signedTx)
	if err != nil {
		return fmt.Errorf("wait mined: %w", err)
	}

	if receipt.Status == 0 {
		// Try to extract revert reason via eth_call replay
		reason := m.extractRevertReason(ctx, signedTx)
		return fmt.Errorf("tx reverted: %s — %s", signedTx.Hash().Hex(), reason)
	}

	log.Printf("[matcher] bulkMatch confirmed tx=%s block=%d gas_used=%d",
		signedTx.Hash().Hex(), receipt.BlockNumber.Uint64(), receipt.GasUsed)

	return nil
}

// extractRevertReason replays the transaction via eth_call to get the revert reason
func (m *Matcher) extractRevertReason(ctx context.Context, tx *types.Transaction) string {
	msg := ethereum.CallMsg{
		From:      m.cfg.WalletAddr,
		To:        tx.To(),
		Gas:       tx.Gas(),
		GasFeeCap: tx.GasFeeCap(),
		GasTipCap: tx.GasTipCap(),
		Value:     tx.Value(),
		Data:      tx.Data(),
	}

	_, err := m.client.CallContract(ctx, msg, nil)
	if err != nil {
		return revert.ExtractRevertReason(err)
	}
	return "unknown (call succeeded on replay)"
}
