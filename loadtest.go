package main

import (
	"context"
	"log"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thenoobicat/featherengine/internal/models"
	"github.com/thenoobicat/featherengine/internal/orderbook"
)

// OrderGenerator simulates high-volume order streams like a webhook
type OrderGenerator struct {
	books      *orderbook.BookManager
	seriesIDs  [][32]byte
	orderCount int64
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func NewOrderGenerator(books *orderbook.BookManager, numSeries int) *OrderGenerator {
	seriesIDs := make([][32]byte, numSeries)
	for i := 0; i < numSeries; i++ {
		seriesIDs[i] = [32]byte{byte(i)}
	}

	return &OrderGenerator{
		books:     books,
		seriesIDs: seriesIDs,
		stopCh:    make(chan struct{}),
	}
}

func (og *OrderGenerator) generateOrder() *models.Order {
	orderID := atomic.AddInt64(&og.orderCount, 1)
	seriesID := og.seriesIDs[rand.Intn(len(og.seriesIDs))]
	isLong := rand.Intn(2) == 0

	// Generate realistic prices around 1 ETH with some spread
	basePrice := int64(1000000000000000000)                   // 1 ETH in wei
	priceSpread := rand.Intn(200) - 100                       // -100 to +100
	price := basePrice + int64(priceSpread)*10000000000000000 // ±0.1 ETH steps

	// Generate realistic quantities
	quantity := int64(rand.Intn(10)+1) * 100000000000000000 // 0.1 to 1.0 ETH

	var oid [32]byte
	for i := 0; i < 8; i++ {
		oid[i] = byte(orderID >> (i * 8))
	}

	return &models.Order{
		OrderID:      oid,
		SeriesID:     seriesID,
		Trader:       [20]byte{byte(rand.Intn(256))}, // Random trader
		IsLong:       isLong,
		LimitPrice:   big.NewInt(price),
		Quantity:     big.NewInt(quantity),
		MarginLocked: big.NewInt(quantity * price / 1000), // Simplified margin
		Intent:       0,
	}
}

func (og *OrderGenerator) Start(numWorkers int, ordersPerSecond int) {
	log.Printf("Starting order generator: %d workers, %d orders/sec", numWorkers, ordersPerSecond)

	interval := time.Second / time.Duration(ordersPerSecond/numWorkers)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	for i := 0; i < numWorkers; i++ {
		og.wg.Add(1)
		go func(workerID int) {
			defer og.wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-og.stopCh:
					return
				case <-ticker.C:
					order := og.generateOrder()
					book := og.books.GetOrCreate(order.SeriesID)
					book.AddOrder(order)
				}
			}
		}(i)
	}
}

func (og *OrderGenerator) Stop() {
	close(og.stopCh)
	og.wg.Wait()
	log.Printf("Order generator stopped. Total orders generated: %d", atomic.LoadInt64(&og.orderCount))
}

func (og *OrderGenerator) GetStats() (int64, int) {
	return atomic.LoadInt64(&og.orderCount), len(og.seriesIDs)
}

// LoadTest runs comprehensive performance tests
func RunLoadTest(books *orderbook.BookManager, duration time.Duration) {
	log.Printf("Starting load test for %v", duration)

	generator := NewOrderGenerator(books, 10) // 10 series
	generator.Start(20, 1000)                 // 20 workers, 1000 orders/sec total

	start := time.Now()
	startOrders := atomic.LoadInt64(&generator.orderCount)

	time.Sleep(duration)

	endOrders := atomic.LoadInt64(&generator.orderCount)
	totalTime := time.Since(start)

	generator.Stop()

	ordersProcessed := endOrders - startOrders
	ordersPerSec := float64(ordersProcessed) / totalTime.Seconds()

	log.Printf("Load test results:")
	log.Printf("  Duration: %v", totalTime)
	log.Printf("  Orders processed: %d", ordersProcessed)
	log.Printf("  Orders/sec: %.0f", ordersPerSec)
	log.Printf("  Series count: %d", len(books.AllSeriesIDs()))

	// Test matching performance
	log.Printf("Testing matching performance...")
	matchStart := time.Now()
	matches := books.FindAllMatches(1000)
	matchTime := time.Since(matchStart)

	log.Printf("  Matches found: %d in %v", len(matches), matchTime)
	if matchTime > 0 {
		log.Printf("  Matches/sec: %.0f", float64(len(matches))/matchTime.Seconds())
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	rand.Seed(time.Now().UnixNano())

	books := orderbook.NewBookManager()

	// Run load test
	RunLoadTest(books, 30*time.Second)

	// Run memory leak test
	log.Printf("Running memory leak test...")
	generator := NewOrderGenerator(books, 5)
	generator.Start(10, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				orders, series := generator.GetStats()
				log.Printf("Progress: %d orders across %d series", orders, series)
			}
		}
	}()

	<-ctx.Done()
	generator.Stop()

	log.Printf("Load testing complete!")
}
