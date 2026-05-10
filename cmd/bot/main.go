package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
// RUNTIME
//

type Runtime struct {
	SeenTradeIDs map[int64]struct{}
}

func NewRuntime() *Runtime {
	return &Runtime{
		SeenTradeIDs: make(map[int64]struct{}),
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

		if _, exists := r.SeenTradeIDs[e.TradeID]; exists {
			log.Printf(
				"[runtime] duplicate trade ignored: id=%d",
				e.TradeID,
			)
			return
		}

		r.SeenTradeIDs[e.TradeID] = struct{}{}

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

func (w *BinanceWS) Run(ctx context.Context) {
	backoff := time.Second

	for {
		err := w.runConnection(ctx)

		// ---------------------------------------------
		// Graceful shutdown
		// ---------------------------------------------

		if ctx.Err() != nil {
			log.Printf("[ws] shutdown requested")
			return
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

		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (w *BinanceWS) runConnection(
	ctx context.Context,
) error {
	log.Printf("[ws] connecting to Binance Testnet")

	const wsURL =
		"wss://stream.testnet.binance.vision/ws/btcusdt@trade"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	defer conn.Close()

	log.Printf("[ws] connected")

	w.out <- WsConnected{
		Time: time.Now(),
	}

	// ---------------------------------------------
	// Read loop
	// ---------------------------------------------

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			w.out <- WsDisconnected{
				Err:  err.Error(),
				Time: time.Now(),
			}

			return err
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

		price, err := strconv.ParseFloat(payload.Price, 64)
		if err != nil {
			log.Printf("[ws] bad price: %v", err)
			continue
		}

		qty, err := strconv.ParseFloat(payload.Qty, 64)
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
	// Graceful shutdown context
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

			// later:
			// - flush sqlite
			// - persist snapshots
			// - close files
			// - drain queues

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
