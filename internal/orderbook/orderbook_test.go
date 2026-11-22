package orderbook

import (
	"math/big"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thenoobicat/featherengine/internal/models"
)

// Test helpers
func createTestOrder(orderID [32]byte, seriesID [32]byte, isLong bool, price int64, quantity int64) *models.Order {
	return &models.Order{
		OrderID:      orderID,
		SeriesID:     seriesID,
		Trader:       [20]byte{1, 2, 3}, // dummy address
		IsLong:       isLong,
		LimitPrice:   big.NewInt(price),
		Quantity:     big.NewInt(quantity),
		MarginLocked: big.NewInt(quantity * price / 100), // dummy margin
		Intent:       0,
	}
}

func BenchmarkOrderbookInsert(b *testing.B) {
	book := NewSeriesBook([32]byte{1})
	order := createTestOrder([32]byte{1}, [32]byte{1}, true, 1000000000000000000, 1000000000000000000) // 1 ETH

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		order.OrderID = [32]byte{byte(i)}
		book.AddOrder(order)
	}
}

func BenchmarkOrderbookMatch(b *testing.B) {
	book := NewSeriesBook([32]byte{1})

	// Pre-populate with crossing orders
	for i := 0; i < 1000; i++ {
		bidID := [32]byte{byte(i), 0}
		askID := [32]byte{byte(i), 1}

		bid := createTestOrder(bidID, [32]byte{1}, true, 1000000000000000000, 1000000000000000000)
		ask := createTestOrder(askID, [32]byte{1}, false, 1000000000000000000, 1000000000000000000)

		book.AddOrder(bid)
		book.AddOrder(ask)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches := book.FindMatches(50)
		_ = matches
	}
}

func TestOrderbookBasicOperations(t *testing.T) {
	book := NewSeriesBook([32]byte{1})

	// Test empty book
	matches := book.FindMatches(10)
	if len(matches) != 0 {
		t.Errorf("Expected 0 matches for empty book, got %d", len(matches))
	}

	// Add bid and ask at same price
	bid := createTestOrder([32]byte{1}, [32]byte{1}, true, 1000000000000000000, 1000000000000000000)
	ask := createTestOrder([32]byte{2}, [32]byte{1}, false, 1000000000000000000, 1000000000000000000)

	book.AddOrder(bid)
	book.AddOrder(ask)

	matches = book.FindMatches(10)
	if len(matches) != 1 {
		t.Errorf("Expected 1 match for crossing orders, got %d", len(matches))
	}

	if matches[0].TakerID != bid.OrderID || matches[0].MakerID != ask.OrderID {
		t.Errorf("Match has wrong taker/maker IDs")
	}
}

func TestOrderbookPricePriority(t *testing.T) {
	book := NewSeriesBook([32]byte{1})

	// Add bids at different prices (higher price = better bid)
	bid1 := createTestOrder([32]byte{1}, [32]byte{1}, true, 900000000000000000, 1000000000000000000)  // 0.9 ETH
	bid2 := createTestOrder([32]byte{2}, [32]byte{1}, true, 1100000000000000000, 1000000000000000000) // 1.1 ETH

	// Add asks at different prices (lower price = better ask)
	ask1 := createTestOrder([32]byte{3}, [32]byte{1}, false, 1200000000000000000, 1000000000000000000) // 1.2 ETH
	ask2 := createTestOrder([32]byte{4}, [32]byte{1}, false, 1000000000000000000, 1000000000000000000) // 1.0 ETH

	book.AddOrder(bid1)
	book.AddOrder(bid2)
	book.AddOrder(ask1)
	book.AddOrder(ask2)

	matches := book.FindMatches(10)

	// Should match best bid (1.1) with best ask (1.0)
	if len(matches) != 1 {
		t.Errorf("Expected 1 match, got %d", len(matches))
	}

	if matches[0].TakerID != bid2.OrderID || matches[0].MakerID != ask2.OrderID {
		t.Errorf("Wrong match: expected bid2-ask2, got %x-%x", matches[0].TakerID[:4], matches[0].MakerID[:4])
	}
}

func TestOrderbookPartialFills(t *testing.T) {
	book := NewSeriesBook([32]byte{1})

	// Bid for 2 units, ask for 1 unit
	bid := createTestOrder([32]byte{1}, [32]byte{1}, true, 1000000000000000000, 2000000000000000000)
	ask := createTestOrder([32]byte{2}, [32]byte{1}, false, 1000000000000000000, 1000000000000000000)

	book.AddOrder(bid)
	book.AddOrder(ask)

	matches := book.FindMatches(10)

	if len(matches) != 1 {
		t.Errorf("Expected 1 match, got %d", len(matches))
	}

	// Check that bid quantity was reduced to 1
	if bid.Quantity.Cmp(big.NewInt(1000000000000000000)) != 0 {
		t.Errorf("Bid quantity should be 1 after partial fill, got %s", bid.Quantity.String())
	}

	// Check that ask was fully filled (removed)
	if book.Asks.Len() != 0 {
		t.Errorf("Ask should be fully filled and removed")
	}
}

func TestOrderbookConcurrency(t *testing.T) {
	book := NewSeriesBook([32]byte{1})

	var wg sync.WaitGroup
	numGoroutines := 100
	ordersPerGoroutine := 100

	// Launch multiple goroutines adding orders
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < ordersPerGoroutine; i++ {
				orderID := [32]byte{byte(goroutineID), byte(i)}
				isLong := i%2 == 0
				price := int64(1000000000000000000 + int64(goroutineID*10000000000000000))
				order := createTestOrder(orderID, [32]byte{1}, isLong, price, 1000000000000000000)
				book.AddOrder(order)
			}
		}(g)
	}

	wg.Wait()

	// Verify all orders were added
	expectedOrders := numGoroutines * ordersPerGoroutine
	if book.Bids.Len()+book.Asks.Len() != expectedOrders {
		t.Errorf("Expected %d total orders, got %d", expectedOrders, book.Bids.Len()+book.Asks.Len())
	}
}

func TestOrderbookRemoveOrder(t *testing.T) {
	book := NewSeriesBook([32]byte{1})

	order := createTestOrder([32]byte{1}, [32]byte{1}, true, 1000000000000000000, 1000000000000000000)
	book.AddOrder(order)

	// Verify order exists
	if book.Bids.Len() != 1 {
		t.Errorf("Order should exist after adding")
	}

	// Remove order
	removed := book.RemoveOrder([32]byte{1}, true)
	if !removed {
		t.Errorf("Order should be removed successfully")
	}

	// Verify order is gone
	if book.Bids.Len() != 0 {
		t.Errorf("Order should not exist after removing")
	}

	// Try to remove non-existent order
	removed = book.RemoveOrder([32]byte{99}, true)
	if removed {
		t.Errorf("Removing non-existent order should return false")
	}
}

func TestBookManagerConcurrency(t *testing.T) {
	bm := NewBookManager()

	var wg sync.WaitGroup
	numGoroutines := 50
	seriesPerGoroutine := 10

	// Launch goroutines creating series and adding orders
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for s := 0; s < seriesPerGoroutine; s++ {
				seriesID := [32]byte{byte(goroutineID), byte(s)}
				book := bm.GetOrCreate(seriesID)

				// Add some orders to each series
				for o := 0; o < 5; o++ {
					orderID := [32]byte{byte(goroutineID), byte(s), byte(o)}
					order := createTestOrder(orderID, seriesID, o%2 == 0, 1000000000000000000, 1000000000000000000)
					book.AddOrder(order)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify all series were created
	expectedSeries := numGoroutines * seriesPerGoroutine
	if len(bm.books) != expectedSeries {
		t.Errorf("Expected %d series, got %d", expectedSeries, len(bm.books))
	}
}

func TestBigIntPooling(t *testing.T) {
	// Test that BigInt pool works correctly
	bi1 := getBigInt()
	bi2 := getBigInt()

	bi1.SetInt64(12345)
	bi2.SetInt64(67890)

	if bi1.Int64() != 12345 || bi2.Int64() != 67890 {
		t.Errorf("BigInt pool returned incorrect values")
	}

	putBigInt(bi1)
	putBigInt(bi2)

	// Get them back and verify they're reset
	bi3 := getBigInt()
	bi4 := getBigInt()

	if bi3.Int64() != 0 || bi4.Int64() != 0 {
		t.Errorf("BigInt pool should return zeroed values, got %d, %d", bi3.Int64(), bi4.Int64())
	}
}

// Stress test with high volume
func TestOrderbookHighVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high volume test in short mode")
	}

	book := NewSeriesBook([32]byte{1})

	start := time.Now()
	numOrders := 10000

	for i := 0; i < numOrders; i++ {
		orderID := [32]byte{byte(i % 256), byte((i / 256) % 256), byte(i / 65536)}
		isLong := i%2 == 0
		price := int64(1000000000000000000 + int64(i%100)*10000000000000000) // Vary prices
		order := createTestOrder(orderID, [32]byte{1}, isLong, price, 1000000000000000000)
		book.AddOrder(order)
	}

	elapsed := time.Since(start)
	t.Logf("Added %d orders in %v (%.0f orders/sec)", numOrders, elapsed, float64(numOrders)/elapsed.Seconds())

	// Run matching
	start = time.Now()
	matches := book.FindMatches(1000)
	elapsed = time.Since(start)

	t.Logf("Found %d matches in %v", len(matches), elapsed)

	// Verify some matches occurred
	if len(matches) == 0 {
		t.Errorf("Expected some matches with %d orders", numOrders)
	}
}

// Ultimate stress test for 20 million orders

func TestUltimate20MillionOrderStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 20M order stress test in short mode - this will take several minutes and significant RAM")
	}
	// This test needs roughly 4-8 GB of RAM and several minutes. Keep it
	// opt-in so the default `go test ./...` and CI stay fast and reliable.
	if os.Getenv("FEATHER_STRESS_TEST") != "1" {
		t.Skip("Skipping 20M order stress test; set FEATHER_STRESS_TEST=1 to run it")
	}

	const (
		numMarkets      = 200                          // 200 markets
		ordersPerMarket = 100000                       // 100k orders per market
		totalOrders     = numMarkets * ordersPerMarket // 20 million total
		numWorkers      = 50                           // Workers for concurrent insertion
		testDuration    = 60 * time.Second             // 1 minute of continuous operation
		batchSize       = 100000                       // Report progress every 100k orders
	)

	t.Logf("Scale: %d markets × %d orders = %d total orders", numMarkets, ordersPerMarket, totalOrders)
	t.Logf("Concurrency: %d workers for %v", numWorkers, testDuration)
	t.Logf("Memory requirement estimate: ~4-8 GB RAM")
	t.Logf("Estimated runtime: 2-5 minutes depending on hardware")

	bm := NewBookManager()

	// Phase 1: Concurrent order insertion across all markets
	t.Logf("Phase 1: Inserting %d orders across %d markets...", totalOrders, numMarkets)

	start := time.Now()

	jobs := make(chan insertionJob, numWorkers*10)
	results := make(chan bool, numWorkers*10)

	// Start workers
	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			for job := range jobs {
				// Create order with unique ID
				orderID := [32]byte{
					byte(job.marketID % 256),
					byte((job.marketID / 256) % 256),
					byte((job.marketID / 65536) % 256),
					byte(job.orderID % 256),
					byte((job.orderID / 256) % 256),
					byte((job.orderID / 65536) % 256),
					byte((job.orderID / 16777216) % 256),
					byte(workerID % 256),
				}

				// Create realistic price distribution (bell curve around $1000)
				basePrice := int64(1000_000000000000000)                    // $1000 in 18 decimals (fits in int64)
				priceVariation := int64(job.priceOffset) * 1000000000000000 // $1 variation steps
				price := basePrice + priceVariation

				order := createTestOrder(orderID, job.seriesID, job.isLong, price, 1000000000000000000)
				bm.AddOrder(order)

				results <- true
			}
		}(w)
	}

	go func() {
		defer close(jobs)
		for market := 0; market < numMarkets; market++ {
			seriesID := [32]byte{
				byte(market % 256),
				byte((market / 256) % 256),
				byte((market / 65536) % 256),
				byte(market / 16777216),
			}

			for order := 0; order < ordersPerMarket; order++ {
				isLong := order%2 == 0
				// More sophisticated price distribution for realistic testing
				priceOffset := (order % 2000) - 1000 // -1000 to +999 range for price variation

				jobs <- insertionJob{
					marketID:    market,
					orderID:     order,
					seriesID:    seriesID,
					isLong:      isLong,
					priceOffset: priceOffset,
				}
			}
		}
	}()

	completed := 0
	nextMilestone := batchSize
	for completed < totalOrders {
		<-results
		completed++

		if completed >= nextMilestone {
			progress := float64(completed) / float64(totalOrders) * 100
			elapsed := time.Since(start)
			rate := float64(completed) / elapsed.Seconds()
			eta := time.Duration(float64(totalOrders-completed)/rate) * time.Second

			t.Logf("Inserted %d/%d orders (%.2f%%) - %.0f orders/sec - ETA: %v",
				completed, totalOrders, progress, rate, eta)

			nextMilestone += batchSize
		}
	}

	insertionTime := time.Since(start)
	insertionRate := float64(totalOrders) / insertionTime.Seconds()
	t.Logf("Phase 1 Complete: %d orders inserted in %v (%.0f orders/sec)",
		totalOrders, insertionTime, insertionRate)

	// Phase 2: Continuous matching under load
	t.Logf("Phase 2: Continuous matching for %v...", testDuration)

	matchCount := int64(0)
	start = time.Now()
	deadline := time.Now().Add(testDuration)

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond) // 100 Hz matching for substantial scale
		defer ticker.Stop()

		matchTicker := time.NewTicker(10 * time.Second) // Report matches every 10 seconds
		defer matchTicker.Stop()

		for time.Now().Before(deadline) {
			select {
			case <-ticker.C:
				matches := bm.FindAllMatches(1000) // Find up to 1k matches per cycle
				atomic.AddInt64(&matchCount, int64(len(matches)))
			case <-matchTicker.C:
				currentMatches := atomic.LoadInt64(&matchCount)
				elapsed := time.Since(start)
				rate := float64(currentMatches) / elapsed.Seconds()
				t.Logf("Matches so far: %d (%.0f matches/sec)", currentMatches, rate)
			}
		}
	}()

	// Phase 3: Concurrent order modifications
	t.Logf("Phase 3: Concurrent order modifications...")

	cancelJobs := make(chan cancelJob, numWorkers*10)
	cancelResults := make(chan bool, numWorkers*10)

	for w := 0; w < numWorkers/2; w++ {
		go func() {
			for job := range cancelJobs {
				// Random cancellation
				if job.shouldCancel {
					bm.RemoveOrder(job.seriesID, job.orderID, job.isLong)
				}
				cancelResults <- true
			}
		}()
	}

	go func() {
		defer close(cancelJobs)
		ticker := time.NewTicker(100 * time.Millisecond) // Generate jobs at controlled rate
		defer ticker.Stop()

		for time.Now().Before(deadline) {
			<-ticker.C

			// Generate multiple cancellation jobs per tick
			for i := 0; i < 100; i++ {
				market := rand.Intn(numMarkets)
				order := rand.Intn(ordersPerMarket)
				seriesID := [32]byte{
					byte(market % 256),
					byte((market / 256) % 256),
					byte((market / 65536) % 256),
					byte(market / 16777216),
				}
				orderID := [32]byte{
					byte(market % 256),
					byte((market / 256) % 256),
					byte((market / 65536) % 256),
					byte(order % 256),
					byte((order / 256) % 256),
					byte((order / 65536) % 256),
					byte((order / 16777216) % 256),
					0, // worker ID 0 for simplicity
				}

				cancelJobs <- cancelJob{
					seriesID:     seriesID,
					orderID:      orderID,
					isLong:       order%2 == 0,
					shouldCancel: rand.Float32() < 0.05, // 5% cancellation rate
				}
			}
		}
	}()

	statusTicker := time.NewTicker(30 * time.Second) // Status every 30 seconds
	defer statusTicker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-statusTicker.C:
			elapsed := time.Since(start)
			currentMatches := atomic.LoadInt64(&matchCount)
			rate := float64(currentMatches) / elapsed.Seconds()

			// Sample memory stats
			totalBooks := len(bm.AllSeriesIDs())
			sampleOrders := 0
			if totalBooks > 0 {
				// Sample first 100 books for order count
				sampleSize := min(100, totalBooks)
				for i := 0; i < sampleSize; i++ {
					seriesIDs := bm.AllSeriesIDs()
					if book := bm.Get(seriesIDs[i]); book != nil {
						sampleOrders += book.Bids.Len() + book.Asks.Len()
					}
				}
				avgOrdersPerBook := float64(sampleOrders) / float64(sampleSize)
				estimatedTotalOrders := int(avgOrdersPerBook * float64(totalBooks))
				t.Logf("Status: %d markets active, ~%d total orders, %d matches (%.0f/sec)",
					totalBooks, estimatedTotalOrders, currentMatches, rate)
			}
		}
	}

	drainStart := time.Now()
	drained := 0
	for range cancelJobs {
		<-cancelResults
		drained++
		if drained%10000 == 0 {
			t.Logf("Draining remaining jobs: %d processed", drained)
		}
	}
	drainTime := time.Since(drainStart)
	t.Logf("Drained %d remaining jobs in %v", drained, drainTime)

	matchingTime := time.Since(start)
	finalMatchCount := atomic.LoadInt64(&matchCount)
	matchRate := float64(finalMatchCount) / matchingTime.Seconds()

	t.Logf("Phase 2-3 Complete: %d matches found in %v (%.0f matches/sec)",
		finalMatchCount, matchingTime, matchRate)

	// Phase 4: Final statistics
	t.Logf("Phase 4: Final statistics...")

	totalBooks := len(bm.AllSeriesIDs())
	actualTotalOrders := 0

	sampleSize := min(500, totalBooks)
	t.Logf("Sampling %d out of %d markets for final stats...", sampleSize, totalBooks)

	sampleStart := time.Now()
	for i := 0; i < sampleSize; i++ {
		seriesIDs := bm.AllSeriesIDs()
		if book := bm.Get(seriesIDs[i]); book != nil {
			actualTotalOrders += book.Bids.Len() + book.Asks.Len()
		}

		if i%100 == 0 && i > 0 {
			t.Logf("Sampled %d/%d markets...", i, sampleSize)
		}
	}
	sampleTime := time.Since(sampleStart)

	avgOrdersPerBook := float64(actualTotalOrders) / float64(sampleSize)
	estimatedTotalOrders := int(avgOrdersPerBook * float64(totalBooks))

	t.Logf("Final State (sampled):")
	t.Logf("   • Active markets: %d", totalBooks)
	t.Logf("   • Sampled orders: %d (from %d markets)", actualTotalOrders, sampleSize)
	t.Logf("   • Avg orders per market: %.1f", avgOrdersPerBook)
	t.Logf("   • Estimated total orders: %d", estimatedTotalOrders)
	t.Logf("   • Sampling time: %v", sampleTime)
	t.Logf("   • Memory efficiency: BigInt pool active")

	// Performance assertions
	if insertionRate < 10000 {
		t.Errorf("Insertion rate too slow: %.0f orders/sec (expected >10k for 20M scale)", insertionRate)
	}
	if matchRate < 500 {
		t.Errorf("Matching rate too slow: %.0f matches/sec (expected >500 for 20M scale)", matchRate)
	}
	if totalBooks < numMarkets/2 {
		t.Errorf("Too many markets lost: %d/%d remaining (expected >50%% survival)", totalBooks, numMarkets)
	}

	t.Logf("Peak insertion rate: %.0f orders/sec", insertionRate)
	t.Logf("Peak matching rate: %.0f matches/sec", matchRate)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type insertionJob struct {
	marketID    int
	orderID     int
	seriesID    [32]byte
	isLong      bool
	priceOffset int
}

type cancelJob struct {
	seriesID     [32]byte
	orderID      [32]byte
	isLong       bool
	shouldCancel bool
}
