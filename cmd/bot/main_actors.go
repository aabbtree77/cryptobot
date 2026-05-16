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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

//
// ============================================================
// CONFIG
// ============================================================
//
// These values define the liveness model.
//
// The key reliability issue:
//
// TCP CAN STAY "ALIVE" EVEN WHEN THE NETWORK IS DEAD.
//
// That means:
//
//	conn.ReadMessage()
//
// may block forever unless:
//
//   - read deadlines are enforced
//   - pong frames extend liveness
//   - ping frames actively probe the path
//
// The actor owns all liveness semantics.
//
// ============================================================
//

const (

	// --------------------------------------------------------
	// Websocket liveness
	// --------------------------------------------------------

	pingInterval = 10 * time.Second

	// If no pong or data arrives before this deadline,
	// the connection is considered dead.
	pongWait = 30 * time.Second

	// Write timeout for ping frames.
	writeWait = 5 * time.Second

	// Dial timeout.
	dialTimeout = 10 * time.Second

	// --------------------------------------------------------
	// Reconnect
	// --------------------------------------------------------

	minReconnect = 1 * time.Second
	maxReconnect = 30 * time.Second

	// If a connection survives long enough,
	// reconnect penalties reset.
	stableConnectionTime = 1 * time.Minute

	// --------------------------------------------------------
	// Logging
	// --------------------------------------------------------

	debugLogging = true
)

//
// ============================================================
// LOGGER
// ============================================================
//

func debugf(format string, args ...any) {
	if debugLogging {
		log.Printf(format, args...)
	}
}

//
// ============================================================
// EVENTS
// ============================================================
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
// ============================================================
// ACTOR MESSAGES
// ============================================================
//
// Internal actor protocol.
//
// These messages form the explicit state machine.
//
// ============================================================
//

type Msg interface{}

type Connect struct{}

type DialSuccess struct {
	Conn *websocket.Conn
}

type DialFailure struct {
	Err error
}

type SocketFrame struct {
	Data []byte
}

type SocketFailure struct {
	Err error
}

type PingTick struct{}

type ReconnectTick struct{}

//
// ============================================================
// CONNECTION STATE MACHINE
// ============================================================
//

type ConnState int

const (
	StateDisconnected ConnState = iota
	StateConnecting
	StateLive
	StateBackingOff
	StateStopping
)

func (s ConnState) String() string {
	switch s {

	case StateDisconnected:
		return "Disconnected"

	case StateConnecting:
		return "Connecting"

	case StateLive:
		return "Live"

	case StateBackingOff:
		return "BackingOff"

	case StateStopping:
		return "Stopping"

	default:
		return "Unknown"
	}
}

//
// ============================================================
// FIXED-SIZE DEDUP CACHE
// ============================================================
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

	if _, exists := d.items[id]; exists {
		return
	}

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
// ============================================================
// RUNTIME ACTOR
// ============================================================
//
// Owns:
//   - dedup cache
//   - deterministic event processing
//
// IMPORTANT:
//
// Only THIS actor mutates runtime state.
//
// ============================================================
//

type RuntimeActor struct {
	inbox chan Event
	dedup *TradeDedup
}

func NewRuntimeActor() *RuntimeActor {
	return &RuntimeActor{
		inbox: make(chan Event, 4096),
		dedup: NewTradeDedup(100_000),
	}
}

func (r *RuntimeActor) Inbox() chan<- Event {
	return r.inbox
}

func (r *RuntimeActor) Run(ctx context.Context) {

	debugf("[runtime] actor started")

	for {

		select {

		case <-ctx.Done():

			debugf("[runtime] shutdown")

			return

		case ev := <-r.inbox:

			r.handle(ev)
		}
	}
}

func (r *RuntimeActor) handle(ev Event) {

	switch e := ev.(type) {

	case WsConnected:

		debugf("[runtime] websocket connected")

	case WsDisconnected:

		debugf(
			"[runtime] websocket disconnected: %s",
			e.Err,
		)

	case MarketTrade:

		if r.dedup.Seen(e.TradeID) {

			debugf(
				"[runtime] duplicate trade ignored: %d",
				e.TradeID,
			)

			return
		}

		r.dedup.Add(e.TradeID)

		debugf(
			"[runtime] trade: id=%d symbol=%s price=%.2f qty=%.6f",
			e.TradeID,
			e.Symbol,
			e.Price,
			e.Qty,
		)
	}
}

//
// ============================================================
// WEBSOCKET ACTOR
// ============================================================
//
// This actor owns:
//
//   - websocket lifecycle
//   - reconnect logic
//   - heartbeat
//   - deadlines
//   - connection state machine
//
// CRITICAL:
//
// Only THIS actor mutates websocket state.
//
// The socket reader goroutine is NOT architecture.
//
// It is only a blocking I/O adapter.
//
// ============================================================
//

type WSActor struct {
	inbox chan Msg

	out chan<- Event

	state ConnState

	conn *websocket.Conn

	backoff time.Duration

	connectedAt time.Time

	connectionID uint64
}

func NewWSActor(out chan<- Event) *WSActor {

	return &WSActor{
		inbox:   make(chan Msg, 4096),
		out:     out,
		state:   StateDisconnected,
		backoff: minReconnect,
	}
}

func (a *WSActor) Run(ctx context.Context) {

	debugf("[ws] actor started")

	// bootstrap actor
	a.inbox <- Connect{}

	for {

		select {

		case <-ctx.Done():

			debugf("[ws] shutdown requested")

			a.transition(StateStopping)

			a.closeConnection()

			return

		case msg := <-a.inbox:

			a.handle(ctx, msg)
		}
	}
}

//
// ============================================================
// ACTOR MESSAGE HANDLER
// ============================================================
//
// This is the HEART of the system.
//
// One serialized stream.
//
// One owner.
//
// One state machine.
//
// Joe Armstrong would nod silently from a cloud of Erlang smoke ☁️
//
// ============================================================
//

func (a *WSActor) handle(
	ctx context.Context,
	msg Msg,
) {

	switch m := msg.(type) {

	// --------------------------------------------------------
	// CONNECT
	// --------------------------------------------------------

	case Connect:

		if a.state != StateDisconnected &&
			a.state != StateBackingOff {

			return
		}

		a.transition(StateConnecting)

		go a.dial(ctx)

	// --------------------------------------------------------
	// DIAL SUCCESS
	// --------------------------------------------------------

	case DialSuccess:

		a.conn = m.Conn

		a.connectedAt = time.Now()

		a.backoff = minReconnect

		a.transition(StateLive)

		debugf("[ws] connected")

		a.out <- WsConnected{
			Time: time.Now(),
		}

		// reader adapter
		go a.readLoop(ctx, m.Conn)

		// heartbeat timer source
		go a.pingLoop(ctx)

	// --------------------------------------------------------
	// DIAL FAILURE
	// --------------------------------------------------------

	case DialFailure:

		debugf("[ws] dial failed: %v", m.Err)

		a.scheduleReconnect(ctx)

	// --------------------------------------------------------
	// READ FRAME
	// --------------------------------------------------------

	case SocketFrame:

		a.processFrame(m.Data)

	// --------------------------------------------------------
	// SOCKET FAILURE
	// --------------------------------------------------------

	case SocketFailure:

		// Ignore stale failures from dead connections.
		//
		// This matters because old goroutines may still
		// report errors after reconnect.
		if a.state != StateLive {
			return
		}

		debugf("[ws] socket failure: %v", m.Err)

		a.out <- WsDisconnected{
			Err:  m.Err.Error(),
			Time: time.Now(),
		}

		a.closeConnection()

		a.scheduleReconnect(ctx)

	// --------------------------------------------------------
	// HEARTBEAT
	// --------------------------------------------------------

	case PingTick:

		if a.state != StateLive {
			return
		}

		if err := a.sendPing(); err != nil {

			a.inbox <- SocketFailure{
				Err: fmt.Errorf(
					"ping failed: %w",
					err,
				),
			}
		}
	}
}

//
// ============================================================
// STATE TRANSITION
// ============================================================
//

func (a *WSActor) transition(next ConnState) {

	old := a.state

	a.state = next

	debugf(
		"[ws] state transition: %s -> %s",
		old,
		next,
	)
}

//
// ============================================================
// DIAL
// ============================================================
//
// IMPORTANT:
//
// The actor itself never blocks on I/O.
//
// This goroutine is merely a kernel adapter.
//
// ============================================================
//

func (a *WSActor) dial(ctx context.Context) {

	debugf("[ws] dialing Binance")

	const wsURL = "wss://stream.testnet.binance.vision/ws/btcusdt@trade"

	dialer := websocket.Dialer{
		HandshakeTimeout: dialTimeout,

		NetDialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	conn, _, err := dialer.Dial(wsURL, nil)

	if err != nil {

		a.inbox <- DialFailure{
			Err: err,
		}

		return
	}

	// --------------------------------------------------------
	// DEAD CONNECTION DETECTION
	// --------------------------------------------------------
	//
	// THIS IS THE IMPORTANT PART.
	//
	// Read deadline guarantees:
	//
	//   if no frames or pong arrive in time,
	//   ReadMessage() FAILS.
	//
	// Without this:
	//
	//   dead TCP sessions can block forever.
	//
	// --------------------------------------------------------

	err = conn.SetReadDeadline(
		time.Now().Add(pongWait),
	)

	if err != nil {

		conn.Close()

		a.inbox <- DialFailure{
			Err: err,
		}

		return
	}

	// --------------------------------------------------------
	// PONG EXTENDS LIVENESS WINDOW
	// --------------------------------------------------------
	//
	// Every pong refreshes the read deadline.
	//
	// This creates active liveness probing.
	//
	// --------------------------------------------------------

	conn.SetPongHandler(func(string) error {

		debugf("[ws] pong received")

		return conn.SetReadDeadline(
			time.Now().Add(pongWait),
		)
	})

	a.inbox <- DialSuccess{
		Conn: conn,
	}
}

//
// ============================================================
// READER LOOP
// ============================================================
//
// This goroutine is intentionally dumb.
//
// No reconnect logic.
// No ownership.
// No lifecycle decisions.
//
// Only:
//   socket -> actor mailbox
//
// ============================================================
//

func (a *WSActor) readLoop(
	ctx context.Context,
	conn *websocket.Conn,
) {

	id := atomic.AddUint64(
		&a.connectionID,
		1,
	)

	debugf("[ws-reader-%d] started", id)

	defer debugf("[ws-reader-%d] stopped", id)

	for {

		_, msg, err := conn.ReadMessage()

		if err != nil {

			a.inbox <- SocketFailure{
				Err: err,
			}

			return
		}

		a.inbox <- SocketFrame{
			Data: msg,
		}
	}
}

//
// ============================================================
// PING LOOP
// ============================================================
//
// IMPORTANT:
//
// Ping loop does NOT own the socket.
//
// It only emits timer events into the actor.
//
// Actor decides what to do.
//
// This distinction is subtle but enormous.
//
// ============================================================
//

func (a *WSActor) pingLoop(ctx context.Context) {

	ticker := time.NewTicker(pingInterval)

	defer ticker.Stop()

	for {

		select {

		case <-ctx.Done():
			return

		case <-ticker.C:

			a.inbox <- PingTick{}
		}
	}
}

//
// ============================================================
// SEND PING
// ============================================================
//
// We actively probe the network.
//
// If the path is dead:
//
//   - ping write fails
//   - or pong never arrives
//   - read deadline expires
//
// BOTH paths terminate the connection.
//
// This solves:
//
//   "TCP alive but internet dead"
//
// ============================================================
//

func (a *WSActor) sendPing() error {

	if a.conn == nil {
		return fmt.Errorf("no active connection")
	}

	err := a.conn.SetWriteDeadline(
		time.Now().Add(writeWait),
	)

	if err != nil {
		return err
	}

	debugf("[ws] sending ping")

	return a.conn.WriteMessage(
		websocket.PingMessage,
		nil,
	)
}

//
// ============================================================
// RECONNECT
// ============================================================
//

func (a *WSActor) scheduleReconnect(
	ctx context.Context,
) {

	if a.state == StateStopping {
		return
	}

	aliveFor := time.Since(a.connectedAt)

	if aliveFor >= stableConnectionTime {

		a.backoff = minReconnect
	}

	delay := a.backoff

	a.transition(StateBackingOff)

	debugf(
		"[ws] reconnect scheduled in %s",
		delay,
	)

	go func() {

		select {

		case <-ctx.Done():
			return

		case <-time.After(delay):

			a.inbox <- Connect{}
		}
	}()

	a.backoff *= 2

	if a.backoff > maxReconnect {
		a.backoff = maxReconnect
	}
}

//
// ============================================================
// CLOSE CONNECTION
// ============================================================
//

func (a *WSActor) closeConnection() {

	if a.conn == nil {
		return
	}

	debugf("[ws] closing connection")

	_ = a.conn.Close()

	a.conn = nil

	a.transition(StateDisconnected)
}

//
// ============================================================
// PAYLOAD NORMALIZATION
// ============================================================
//

func (a *WSActor) processFrame(data []byte) {

	var payload struct {
		EventType string `json:"e"`
		EventTime int64  `json:"E"`

		Symbol string `json:"s"`

		TradeID int64 `json:"t"`

		Price string `json:"p"`
		Qty   string `json:"q"`
	}

	if err := json.Unmarshal(data, &payload); err != nil {

		debugf("[ws] bad payload: %v", err)

		return
	}

	price, err := strconv.ParseFloat(
		payload.Price,
		64,
	)

	if err != nil {

		debugf("[ws] bad price: %v", err)

		return
	}

	qty, err := strconv.ParseFloat(
		payload.Qty,
		64,
	)

	if err != nil {

		debugf("[ws] bad qty: %v", err)

		return
	}

	trade := MarketTrade{
		TradeID: payload.TradeID,
		Symbol:  payload.Symbol,
		Price:   price,
		Qty:     qty,
		Time:    time.UnixMilli(payload.EventTime),
	}

	a.out <- trade
}

//
// ============================================================
// MAIN
// ============================================================
//
// Small.
// Calm.
// Declarative.
//
// The actor system does the work.
//
// ============================================================
//

func main() {

	log.SetFlags(
		log.LstdFlags |
			log.Lmicroseconds,
	)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)

	defer cancel()

	// --------------------------------------------------------
	// Runtime actor
	// --------------------------------------------------------

	runtimeActor := NewRuntimeActor()

	go runtimeActor.Run(ctx)

	// --------------------------------------------------------
	// Websocket actor
	// --------------------------------------------------------

	wsActor := NewWSActor(
		runtimeActor.Inbox(),
	)

	go wsActor.Run(ctx)

	debugf("[main] actor system started")

	// --------------------------------------------------------
	// Wait for shutdown
	// --------------------------------------------------------

	<-ctx.Done()

	debugf("[main] shutdown signal received")

	time.Sleep(500 * time.Millisecond)

	debugf("[main] shutdown complete")
}
