package main

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

//
// EVENTS
//

type Event interface {
	EventType() string
}

type MarketTrade struct {
	TradeID int64
	Symbol  string
	Price   float64
	Qty     float64
	Time    time.Time
}

func (e MarketTrade) EventType() string {
	return "MarketTrade"
}

type WsConnected struct {
	Time time.Time
}

func (e WsConnected) EventType() string {
	return "WsConnected"
}

type WsDisconnected struct {
	Err  string
	Time time.Time
}

func (e WsDisconnected) EventType() string {
	return "WsDisconnected"
}

//
// FIXED-SIZE DEDUP CACHE
//
// Standard approach:
// - bounded memory
// - O(1) lookup
// - O(1) eviction
//

type TradeDedup struct {
	maxSize int
	items   map[int64]*list.Element
	order   *list.List
}

func NewTradeDedup(maxSize int) *TradeDedup {
	return &TradeDedup{
		maxSize: maxSize,
		items:   make(map[int64]*list.Element),
		order:   list.New(),
	}
}

func (d *TradeDedup) Seen(id int64) bool {
	_, exists := d.items[id]
	return exists
}

func (d *TradeDedup) Add(id int64) {

	// already exists
	if _, exists := d.items[id]; exists {
		return
	}

	// evict oldest
	if d.order.Len() >= d.maxSize {
		oldest := d.order.Front()

		if oldest != nil {
			oldID := oldest.Value.(int64)

			delete(d.items, oldID)
			d.order.Remove(oldest)
		}
	}

	elem := d.order.PushBack(id)
	d.items[id] = elem
}

//
// RUNTIME
//

type Runtime struct {
	SeenTrades *TradeDedup
}

func NewRuntime() *Runtime {
	return &Runtime{
		SeenTrades: NewTradeDedup(100_000),
	}
}

func (r *Runtime) Handle(ev Event) {
	switch e := ev.(type) {

	case WsConnected:
		log.Printf("[runtime] websocket connected")

	case WsDisconnected:
		log.Printf(
			"[runtime] websocket disconnected: %s",
			e.Err,
		)

	case MarketTrade:

		// ---------------------------------------------
		// Deduplication
		// ---------------------------------------------

		if r.SeenTrades.Seen(e.TradeID) {
			log.Printf(
				"[runtime] duplicate trade ignored: id=%d",
				e.TradeID,
			)
			return
		}

		r.SeenTrades.Add(e.TradeID)

		// ---------------------------------------------
		// Main deterministic event processing
		// ---------------------------------------------

		log.Printf(
			"[runtime] trade processed: id=%d symbol=%s price=%.2f qty=%.6f",
			e.TradeID,
			e.Symbol,
			e.Price,
			e.Qty,
		)
	}
}

//
// BINANCE TESTNET WEBSOCKET
//

type BinanceWS struct {
	out chan<- Event
}

func NewBinanceWS(out chan<- Event) *BinanceWS {
	return &BinanceWS{
		out: out,
	}
}

const (
	pongWait   = 30 * time.Second
	pingPeriod = 10 * time.Second
	writeWait  = 5 * time.Second

	maxBackoff = 30 * time.Second
	minBackoff = 1 * time.Second

	// reset reconnect penalty
	// if connection survives long enough
	stableConnectionTime = 1 * time.Minute
)

func (w *BinanceWS) Run(ctx context.Context) {

	backoff := minBackoff

	for {

		started := time.Now()

		err := w.runConnection(ctx)

		aliveFor := time.Since(started)

		// ---------------------------------------------
		// Graceful shutdown
		// ---------------------------------------------

		if ctx.Err() != nil {
			log.Printf("[ws] shutdown requested")
			return
		}

		// ---------------------------------------------
		// Reset backoff after stable connection
		// ---------------------------------------------

		if aliveFor >= stableConnectionTime {
			backoff = minBackoff
		}

		// ---------------------------------------------
		// Emit disconnect event
		// ---------------------------------------------

		w.out <- WsDisconnected{
			Err:  err.Error(),
			Time: time.Now(),
		}

		// ---------------------------------------------
		// Reconnect handling
		// ---------------------------------------------

		log.Printf(
			"[ws] connection lost: %v | reconnecting in %s",
			err,
			backoff,
		)

		select {

		case <-ctx.Done():
			return

		case <-time.After(backoff):
		}

		// exponential backoff
		backoff *= 2

		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (w *BinanceWS) runConnection(
	ctx context.Context,
) error {

	log.Printf("[ws] connecting to Binance Testnet")

	const wsURL = "wss://stream.testnet.binance.vision/ws/btcusdt@trade"

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,

		NetDialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	defer conn.Close()

	log.Printf("[ws] connected")

	w.out <- WsConnected{
		Time: time.Now(),
	}

	// ---------------------------------------------
	// Read deadline
	// ---------------------------------------------

	if err := conn.SetReadDeadline(
		time.Now().Add(pongWait),
	); err != nil {
		return err
	}

	// ---------------------------------------------
	// Pong extends liveness
	// ---------------------------------------------

	conn.SetPongHandler(func(string) error {

		return conn.SetReadDeadline(
			time.Now().Add(pongWait),
		)
	})

	// ---------------------------------------------
	// Ping loop
	// ---------------------------------------------

	errChan := make(chan error, 1)

	go func() {

		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()

		for {

			select {

			case <-ctx.Done():
				return

			case <-ticker.C:

				conn.SetWriteDeadline(
					time.Now().Add(writeWait),
				)

				err := conn.WriteMessage(
					websocket.PingMessage,
					nil,
				)

				if err != nil {

					select {
					case errChan <- fmt.Errorf(
						"ping failed: %w",
						err,
					):
					default:
					}

					return
				}
			}
		}
	}()

	// ---------------------------------------------
	// Read loop
	// ---------------------------------------------

	for {

		select {

		case <-ctx.Done():
			return ctx.Err()

		case err := <-errChan:
			return err

		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf(
				"read failed: %w",
				err,
			)
		}

		// ---------------------------------------------
		// Binance payload
		// ---------------------------------------------

		var payload struct {
			EventType string `json:"e"`
			EventTime int64  `json:"E"`

			Symbol string `json:"s"`

			TradeID int64 `json:"t"`

			Price string `json:"p"`
			Qty   string `json:"q"`
		}

		if err := json.Unmarshal(msg, &payload); err != nil {
			log.Printf("[ws] bad payload: %v", err)
			continue
		}

		price, err := strconv.ParseFloat(
			payload.Price,
			64,
		)
		if err != nil {
			log.Printf("[ws] bad price: %v", err)
			continue
		}

		qty, err := strconv.ParseFloat(
			payload.Qty,
			64,
		)
		if err != nil {
			log.Printf("[ws] bad qty: %v", err)
			continue
		}

		// ---------------------------------------------
		// Normalize into internal event
		// ---------------------------------------------

		trade := MarketTrade{
			TradeID: payload.TradeID,
			Symbol:  payload.Symbol,
			Price:   price,
			Qty:     qty,
			Time:    time.UnixMilli(payload.EventTime),
		}

		select {

		case <-ctx.Done():
			return ctx.Err()

		case w.out <- trade:
		}
	}
}

//
// MAIN
//

func main() {

	// ---------------------------------------------
	// Graceful shutdown
	// ---------------------------------------------

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	// ---------------------------------------------
	// Central event channel
	// ---------------------------------------------

	eventChan := make(chan Event, 1024)

	// ---------------------------------------------
	// Runtime state
	// ---------------------------------------------

	runtime := NewRuntime()

	// ---------------------------------------------
	// Websocket subsystem
	// ---------------------------------------------

	ws := NewBinanceWS(eventChan)

	go ws.Run(ctx)

	log.Printf("[main] runtime started")

	// ---------------------------------------------
	// Central deterministic event loop
	// ---------------------------------------------

	for {

		select {

		case <-ctx.Done():

			log.Printf("[main] shutdown signal received")

			time.Sleep(500 * time.Millisecond)

			log.Printf("[main] shutdown complete")

			return

		case ev := <-eventChan:

			fmt.Printf(
				"\n[main] event received: %s\n",
				ev.EventType(),
			)

			runtime.Handle(ev)
		}
	}
}
