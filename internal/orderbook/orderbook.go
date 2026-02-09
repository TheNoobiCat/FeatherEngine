package orderbook

// B-tree (google/btree) based price-time priority orderbook.
// Bids: highest-price-first.  Asks: lowest-price-first.
// Within the same price level orders are FIFO (insertion order via container/list).
//
// Each side of each series book uses:
//   - btree.BTreeG[*PriceLevel]  — sorted price levels for O(log n) best/insert/delete
//   - map[string]*PriceLevel     — O(1) random access by price key
//   - map[[32]byte]*list.Element — O(1) order lookup/cancel by orderID

import (
	"container/list"
	"math/big"
	"sync"

	"github.com/google/btree"
	"github.com/thenoobicat/featherengine/internal/models"
)

// BigIntPool for reusing big.Int objects to reduce GC pressure
var bigIntPool = sync.Pool{
	New: func() interface{} {
		return new(big.Int)
	},
}

func getBigInt() *big.Int {
	return bigIntPool.Get().(*big.Int)
}

func putBigInt(bi *big.Int) {
	bi.SetInt64(0) // Reset to zero
	bigIntPool.Put(bi)
}

type PriceLevel struct {
	Price    *big.Int   // price key (18-decimal)
	Orders   *list.List // *models.Order elements, front = oldest (FIFO)
	TotalQty *big.Int   // sum of all order quantities at this level
}

func newPriceLevel(price *big.Int) *PriceLevel {
	return &PriceLevel{
		Price:    new(big.Int).Set(price),
		Orders:   list.New(),
		TotalQty: new(big.Int),
	}
}

type Side struct {
	tree       *btree.BTreeG[*PriceLevel]
	levels     map[string]*PriceLevel     // price.String() → level
	orderIndex map[[32]byte]*list.Element // orderID → list element (for O(1) cancel)
	orderLevel map[[32]byte]*PriceLevel   // orderID → which level it lives in
	isBid      bool
	size       int // total order count
}

func newSide(isBid bool) *Side {
	var less func(a, b *PriceLevel) bool
	if isBid {
		// Bids: higher price is "less" so Min() gives best bid
		less = func(a, b *PriceLevel) bool {
			return a.Price.Cmp(b.Price) > 0 // descending
		}
	} else {
		// Asks: lower price first (ascending, natural order)
		less = func(a, b *PriceLevel) bool {
			return a.Price.Cmp(b.Price) < 0
		}
	}

	return &Side{
		tree:       btree.NewG(32, less),
		levels:     make(map[string]*PriceLevel),
		orderIndex: make(map[[32]byte]*list.Element),
		orderLevel: make(map[[32]byte]*PriceLevel),
		isBid:      isBid,
	}
}

// Insert adds an order. If a level at that price exists, the order is appended (FIFO).
func (s *Side) Insert(order *models.Order) {
	key := order.LimitPrice.String()

	lvl, exists := s.levels[key]
	if !exists {
		lvl = newPriceLevel(order.LimitPrice)
		s.levels[key] = lvl
		s.tree.ReplaceOrInsert(lvl)
	}

	elem := lvl.Orders.PushBack(order)
	lvl.TotalQty.Add(lvl.TotalQty, order.Quantity)

	s.orderIndex[order.OrderID] = elem
	s.orderLevel[order.OrderID] = lvl
	s.size++
}

// Remove cancels a single order by ID in O(1).
func (s *Side) Remove(orderID [32]byte) bool {
	elem, ok := s.orderIndex[orderID]
	if !ok {
		return false
	}
	lvl := s.orderLevel[orderID]
	order := elem.Value.(*models.Order)

	lvl.TotalQty.Sub(lvl.TotalQty, order.Quantity)
	lvl.Orders.Remove(elem)

	delete(s.orderIndex, orderID)
	delete(s.orderLevel, orderID)
	s.size--

	// If the level is empty, remove it from tree + map
	if lvl.Orders.Len() == 0 {
		s.tree.Delete(lvl)
		delete(s.levels, lvl.Price.String())
	}

	return true
}

// Best returns the best price level (highest bid / lowest ask), or nil.
func (s *Side) Best() *PriceLevel {
	// With our comparators, Min() always gives the "best" side:
	//   bids: comparator is descending, so Min() = highest price
	//   asks: comparator is ascending,  so Min() = lowest price
	item, ok := s.tree.Min()
	if !ok {
		return nil
	}
	return item
}

// PopFront removes and returns the front (oldest) order from the best level.
// If the level becomes empty it is pruned from the tree.
func (s *Side) PopFront() *models.Order {
	lvl := s.Best()
	if lvl == nil || lvl.Orders.Len() == 0 {
		return nil
	}

	elem := lvl.Orders.Front()
	order := elem.Value.(*models.Order)

	lvl.TotalQty.Sub(lvl.TotalQty, order.Quantity)
	lvl.Orders.Remove(elem)

	delete(s.orderIndex, order.OrderID)
	delete(s.orderLevel, order.OrderID)
	s.size--

	if lvl.Orders.Len() == 0 {
		s.tree.Delete(lvl)
		delete(s.levels, lvl.Price.String())
	}

	return order
}

// PeekFront returns the front order of the best level without removing it.
func (s *Side) PeekFront() *models.Order {
	lvl := s.Best()
	if lvl == nil || lvl.Orders.Len() == 0 {
		return nil
	}
	return lvl.Orders.Front().Value.(*models.Order)
}

// Len returns total order count across all levels.
func (s *Side) Len() int { return s.size }

type SeriesBook struct {
	SeriesID [32]byte
	Bids     *Side
	Asks     *Side
	mu       sync.Mutex

	// Channel to signal when matching should be attempted
	matchTrigger chan struct{}
}

func NewSeriesBook(seriesID [32]byte) *SeriesBook {
	return &SeriesBook{
		SeriesID:     seriesID,
		Bids:         newSide(true),
		Asks:         newSide(false),
		matchTrigger: make(chan struct{}, 1), // Buffered channel to avoid blocking
	}
}

func (sb *SeriesBook) AddOrder(order *models.Order) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if order.IsLong {
		sb.Bids.Insert(order)
	} else {
		sb.Asks.Insert(order)
	}

	// Signal that matching should be attempted
	select {
	case sb.matchTrigger <- struct{}{}:
	default:
		// Channel is full, matching is already pending
	}
}

func (sb *SeriesBook) MatchTrigger() <-chan struct{} {
	return sb.matchTrigger
}

func (sb *SeriesBook) RemoveOrder(orderID [32]byte, isLong bool) bool {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if isLong {
		return sb.Bids.Remove(orderID)
	}
	return sb.Asks.Remove(orderID)
}

// FindMatches returns crossing pairs up to maxMatches.
// A match occurs when best bid price >= best ask price.
// The buyer (bid) is treated as taker; the seller (ask) is the maker.
func (sb *SeriesBook) FindMatches(maxMatches int) []models.Match {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	var matches []models.Match

	for len(matches) < maxMatches {
		bestBid := sb.Bids.Best()
		bestAsk := sb.Asks.Best()
		if bestBid == nil || bestAsk == nil {
			break
		}

		// Crossing check
		if bestBid.Price.Cmp(bestAsk.Price) < 0 {
			break
		}

		taker := sb.Bids.PeekFront()
		maker := sb.Asks.PeekFront()
		if taker == nil || maker == nil {
			break
		}

		matches = append(matches, models.Match{
			SeriesID: sb.SeriesID,
			TakerID:  taker.OrderID,
			MakerID:  maker.OrderID,
		})

		// Compute fill quantity
		fillQty := getBigInt()
		defer putBigInt(fillQty)
		fillQty.Set(taker.Quantity)
		if maker.Quantity.Cmp(fillQty) < 0 {
			fillQty.Set(maker.Quantity)
		}

		// Proportionally reduce margin locked
		if taker.Quantity.Sign() > 0 {
			takerMarginUsed := getBigInt()
			defer putBigInt(takerMarginUsed)
			marginRatio := getBigInt()
			defer putBigInt(marginRatio)
			marginRatio.Div(taker.MarginLocked, taker.Quantity)
			takerMarginUsed.Mul(marginRatio, fillQty)
			taker.MarginLocked.Sub(taker.MarginLocked, takerMarginUsed)
		}
		if maker.Quantity.Sign() > 0 {
			makerMarginUsed := getBigInt()
			defer putBigInt(makerMarginUsed)
			marginRatio := getBigInt()
			defer putBigInt(marginRatio)
			marginRatio.Div(maker.MarginLocked, maker.Quantity)
			makerMarginUsed.Mul(marginRatio, fillQty)
			maker.MarginLocked.Sub(maker.MarginLocked, makerMarginUsed)
		}

		// Reduce quantities
		taker.Quantity.Sub(taker.Quantity, fillQty)
		maker.Quantity.Sub(maker.Quantity, fillQty)

		// Prune fully filled orders
		if taker.Quantity.Sign() == 0 {
			sb.Bids.PopFront()
		}
		if maker.Quantity.Sign() == 0 {
			sb.Asks.PopFront()
		}
	}

	return matches
}

type BookManager struct {
	books map[[32]byte]*SeriesBook
	mu    sync.RWMutex
}

func NewBookManager() *BookManager {
	return &BookManager{
		books: make(map[[32]byte]*SeriesBook),
	}
}

func (bm *BookManager) GetOrCreate(seriesID [32]byte) *SeriesBook {
	bm.mu.RLock()
	if book, ok := bm.books[seriesID]; ok {
		bm.mu.RUnlock()
		return book
	}
	bm.mu.RUnlock()

	bm.mu.Lock()
	defer bm.mu.Unlock()
	if book, ok := bm.books[seriesID]; ok {
		return book
	}
	book := NewSeriesBook(seriesID)
	bm.books[seriesID] = book
	return book
}

func (bm *BookManager) Get(seriesID [32]byte) *SeriesBook {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.books[seriesID]
}

func (bm *BookManager) AllSeriesIDs() [][32]byte {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	ids := make([][32]byte, 0, len(bm.books))
	for id := range bm.books {
		ids = append(ids, id)
	}
	return ids
}

func (bm *BookManager) AddOrder(order *models.Order) {
	book := bm.GetOrCreate(order.SeriesID)
	book.AddOrder(order)
}

func (bm *BookManager) RemoveOrder(seriesID, orderID [32]byte, isLong bool) bool {
	book := bm.Get(seriesID)
	if book == nil {
		return false
	}
	return book.RemoveOrder(orderID, isLong)
}

func (bm *BookManager) FindAllMatches(maxPerSeries int) []models.Match {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	var allMatches []models.Match
	for _, book := range bm.books {
		// Sequential processing to avoid race conditions
		// TODO: Implement proper parallel matching with isolated state
		matches := book.FindMatches(maxPerSeries)
		allMatches = append(allMatches, matches...)
	}
	return allMatches
}

func (bm *BookManager) GetAllMatchTriggers() []<-chan struct{} {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	triggers := make([]<-chan struct{}, 0, len(bm.books))
	for _, book := range bm.books {
		triggers = append(triggers, book.MatchTrigger())
	}
	return triggers
}
