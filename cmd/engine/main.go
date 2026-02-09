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

	// Replay historical events to populate orderbook
	// Look back ~345600 blocks on Arbitrum (~24h at 250ms block time)
	startBlock := utils.SafeStartBlock(ctx, client, 345600)
	log.Printf("replaying events from block %d", startBlock)

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
