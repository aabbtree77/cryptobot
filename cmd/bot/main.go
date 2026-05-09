package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
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
		log.Printf("[runtime] websocket disconnected: %s", e.Err)

	case MarketTrade:
		// -------------------------------------------------
		// Deduplication
		// -------------------------------------------------

		if _, exists := r.SeenTradeIDs[e.TradeID]; exists {
			log.Printf(
				"[runtime] duplicate trade ignored: id=%d",
				e.TradeID,
			)
			return
		}

		r.SeenTradeIDs[e.TradeID] = struct{}{}

		// -------------------------------------------------
		// Main deterministic event processing
		// -------------------------------------------------

		log.Printf(
			"[runtime] trade processed: id=%d symbol=%s price=%.2f qty=%.4f",
			e.TradeID,
			e.Symbol,
			e.Price,
			e.Qty,
		)
	}
}

//
// WEBSOCKET SIMULATION
//

type FakeBinanceWS struct {
	out chan<- Event
}

func NewFakeBinanceWS(out chan<- Event) *FakeBinanceWS {
	return &FakeBinanceWS{
		out: out,
	}
}

func (w *FakeBinanceWS) Run(ctx context.Context) {
	backoff := time.Second

	for {
		err := w.runConnection(ctx)

		// -------------------------------------------------
		// Graceful shutdown path
		// -------------------------------------------------

		if ctx.Err() != nil {
			log.Printf("[ws] shutdown requested")
			return
		}

		// -------------------------------------------------
		// Reconnect path
		// -------------------------------------------------

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

		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

func (w *FakeBinanceWS) runConnection(
	ctx context.Context,
) error {
	log.Printf("[ws] connecting")

	w.out <- WsConnected{
		Time: time.Now(),
	}

	// -------------------------------------------------
	// Fake Binance trade stream
	// -------------------------------------------------

	trades := []MarketTrade{
		{
			TradeID: 1001,
			Symbol:  "BTCUSDT",
			Price:   108500.10,
			Qty:     0.001,
			Time:    time.Now(),
		},
		{
			TradeID: 1002,
			Symbol:  "BTCUSDT",
			Price:   108500.50,
			Qty:     0.002,
			Time:    time.Now(),
		},

		// intentional duplicate
		{
			TradeID: 1002,
			Symbol:  "BTCUSDT",
			Price:   108500.50,
			Qty:     0.002,
			Time:    time.Now(),
		},

		{
			TradeID: 1003,
			Symbol:  "BTCUSDT",
			Price:   108501.25,
			Qty:     0.004,
			Time:    time.Now(),
		},
	}

	for i, trade := range trades {

		// -------------------------------------------------
		// Simulate network delay
		// -------------------------------------------------

		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-time.After(1 * time.Second):
		}

		// -------------------------------------------------
		// Simulate random connection drop
		// -------------------------------------------------

		if i == 2 && rand.Intn(2) == 0 {
			w.out <- WsDisconnected{
				Err:  "simulated EOF",
				Time: time.Now(),
			}

			return errors.New("simulated EOF")
		}

		// -------------------------------------------------
		// Emit event into central event loop
		// -------------------------------------------------

		select {
		case <-ctx.Done():
			return ctx.Err()

		case w.out <- trade:
		}
	}

	// -------------------------------------------------
	// Simulate clean connection close
	// -------------------------------------------------

	w.out <- WsDisconnected{
		Err:  "server closed connection",
		Time: time.Now(),
	}

	return errors.New("server closed connection")
}

//
// MAIN
//

func main() {
	rand.Seed(time.Now().UnixNano())

	// -------------------------------------------------
	// Graceful shutdown context
	// -------------------------------------------------

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	// -------------------------------------------------
	// Central event channel
	// -------------------------------------------------

	eventChan := make(chan Event, 1024)

	// -------------------------------------------------
	// Runtime state
	// -------------------------------------------------

	runtime := NewRuntime()

	// -------------------------------------------------
	// Websocket subsystem
	// -------------------------------------------------

	ws := NewFakeBinanceWS(eventChan)

	go ws.Run(ctx)

	log.Printf("[main] runtime started")

	// -------------------------------------------------
	// Central deterministic event loop
	// -------------------------------------------------

	for {
		select {

		// ---------------------------------------------
		// Graceful shutdown
		// ---------------------------------------------

		case <-ctx.Done():
			log.Printf("[main] shutdown signal received")

			// Here later:
			// - flush sqlite
			// - save snapshots
			// - close websocket
			// - drain queues

			time.Sleep(500 * time.Millisecond)

			log.Printf("[main] shutdown complete")

			return

		// ---------------------------------------------
		// Central event processing
		// ---------------------------------------------

		case ev := <-eventChan:
			fmt.Printf(
				"\n[main] event received: %s\n",
				ev.EventType(),
			)

			runtime.Handle(ev)
		}
	}
}
