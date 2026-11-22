package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/thenoobicat/featherengine/internal/config"
	"github.com/thenoobicat/featherengine/internal/ingestion"
	"github.com/thenoobicat/featherengine/internal/matcher"
	"github.com/thenoobicat/featherengine/internal/orderbook"
	"github.com/thenoobicat/featherengine/internal/pusher"
	"github.com/thenoobicat/featherengine/internal/utils"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	log.Println("FeatherEngine — Options Matching Engine & Liquidator")

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	log.Printf("wallet:        %s", cfg.WalletAddr.Hex())
	log.Printf("OptionMarket:  %s", cfg.OptionMarketAddr.Hex())
	log.Printf("Orderbook:     %s", cfg.OrderbookAddr.Hex())
	log.Printf("MarginAccount: %s", cfg.MarginAccountAddr.Hex())
	log.Printf("chain:         %s", cfg.ChainID.String())

	// Connect to Alchemy WebSocket
	client, err := ethclient.Dial(cfg.AlchemyWSURL)
	if err != nil {
		log.Fatalf("ws connect: %v", err)
	}
	defer client.Close()
	log.Println("connected to Alchemy WebSocket")

	// Verify chain ID
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Fatalf("chain ID: %v", err)
	}
	if chainID.Cmp(cfg.ChainID) != 0 {
		log.Fatalf("chain ID mismatch: expected %s got %s", cfg.ChainID.String(), chainID.String())
	}

	// Initialize components
	books := orderbook.NewBookManager()
	traders := ingestion.NewTraderTracker()
	listener := ingestion.NewListener(cfg, client, books, traders)
	eventPusher := pusher.New(cfg, client, books)
	engine := matcher.New(cfg, client, books)
	liquidator := matcher.NewLiquidator(cfg, client, listener, traders)

	// Replay historical events to populate the in-memory orderbook.
	//
	// START_BLOCK takes precedence when set. Otherwise replay the last
	// REPLAY_LOOKBACK_BLOCKS blocks (default 345600, roughly 24h on Arbitrum
	// at 250ms block time). Orders and positions opened before the replay
	// start are not loaded, so the liquidator can miss older positions. Set
	// START_BLOCK to the Orderbook deployment block for full history.
	startBlock := cfg.StartBlock
	if startBlock == 0 {
		startBlock = utils.SafeStartBlock(ctx, client, cfg.ReplayLookbackBlocks)
		log.Printf("START_BLOCK not set; replaying last %d blocks (from block %d)",
			cfg.ReplayLookbackBlocks, startBlock)
	} else {
		log.Printf("replaying events from START_BLOCK %d", startBlock)
	}
	if startBlock > 0 {
		log.Printf("WARNING: orders/positions opened before block %d are not replayed and may be missed by the liquidator", startBlock)
	}

	if err := listener.FetchExistingOrders(ctx, startBlock); err != nil {
		log.Printf("warning: historical replay failed: %v", err)
	}
	if err := eventPusher.ReplaySettlementEvents(ctx, startBlock); err != nil {
		log.Printf("warning: settlement replay failed: %v", err)
	}

	// Subscribe to live events
	if err := listener.Start(ctx); err != nil {
		log.Fatalf("listener: %v", err)
	}
	if err := eventPusher.Start(ctx); err != nil {
		log.Fatalf("event pusher: %v", err)
	}

	// Start matching engine & liquidator
	go engine.Run(ctx)
	go liquidator.Run(ctx)

	log.Println("FeatherEngine is running. Ctrl+C to stop.")

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	cancel()
	log.Println("goodbye")
}
