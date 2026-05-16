# cryptobot

Cryptocurrency trading bot. Inspired by

Nikhil Barthwal. Building Your Own Trading Bot in F#. #LambdaConf2025. [Presentation video](https://www.youtube.com/watch?v=iyx2qIv8DDw&t=2498s), [code](https://github.com/nikhilbarthwal/Vyapari/blob/master/Vyapari/), [slides](https://www.lambdadays.org/static/upload/media/1686573948256988nikhilbarthwalbuildingyourowntradingbotinf.pdf).

This code:

- Go instead of F#.

- More emphasis on reliability than trading. Reconnects, data duplication...

- Binance instead of Gemini and Tradier.

- During an active connection, three live goroutines (main, ws, ping):

  ```bash
  main()
  ├── main event loop
  └── go ws.Run()

  ws.Run()
  └── reconnect loop
      └── runConnection()
          ├── websocket read loop
          └── go ping loop
  ```

So far (May 12th, 2026, ~500 LOC in Go) the program:

- connects to "wss://stream.testnet.binance.vision/ws/btcusdt@trade",

- reads the stream, formats data, and outputs it to the terminal,

- skips duplicates,

- survives connection loss, packet loss, packet delays (read below),

- handles ctrl+c shutdown.

Gorilla WebSocket is used. It allows one concurrent reader and one concurrent writer.

The trades are stored in `SeenTrades` which is a fixed-capacity FIFO: newest IDs added at tail, oldest evicted from head, max size fixed to 10K. No leaks as every insertion beyond capacity evicts exactly one old item.

# Surviving Network Loss

When the internet/connection is lost, the TCP socket is still alive and conn.ReadMessage() may block. Websockets alone do NOT reliably detect dead connections.

The industry standard seems to be the following solution (ChatGPT-5):

Every 10s you send a websocket ping.

Binance responds with pong.

Every pong extends read deadline by 30s.

If:

internet dies

WiFi freezes

router blackholes packets

Binance stalls

upstream silently disappears

TCP becomes half-open

then eventually:

no pong arrives

read deadline expires

ReadMessage() exits with timeout

reconnect loop activates

# Test 1: Connection Loss

```bash
mkdir cryptobot
cd cryptobot
go mod init cryptobot
mkdir -p cmd/bot
nano cmd/bot/main.go
go get github.com/gorilla/websocket
go run cmd/bot/main.go
2026/05/11 22:54:45 [main] runtime started
2026/05/11 22:54:45 [ws] connecting to Binance Testnet
2026/05/11 22:54:46 [ws] connected

[main] event received: WsConnected
2026/05/11 22:54:46 [runtime] websocket connected

[main] event received: MarketTrade
2026/05/11 22:54:48 [runtime] trade processed: id=1778529288623 symbol=BTCUSDT price=81958.33 qty=0.001000

[main] event received: MarketTrade
2026/05/11 22:54:48 [runtime] trade processed: id=1778529288628 symbol=BTCUSDT price=81940.99 qty=0.010040

...
```

Open another terminal and kill internet temporarily:

```bash
nmcli device status
DEVICE           TYPE      STATE      CONNECTION
enp3s0           ethernet  connected  Wired connection 1
sudo ip link set enp3s0 down
```

Wait for a while and enable it again:

```bash
sudo ip link set enp3s0 up
```

Inside the previous terminal you should see the recovery:

```bash
[main] event received: MarketTrade
2026/05/11 22:55:52 [runtime] trade processed: id=1778529351948 symbol=BTCUSDT price=81974.61 qty=0.024400

[main] event received: MarketTrade
2026/05/11 22:55:52 [runtime] trade processed: id=1778529351981 symbol=BTCUSDT price=81974.61 qty=0.001000
2026/05/11 22:56:17 [ws] connection lost: read failed: read tcp 192.168.0.101:55280->13.192.24.88:443: i/o timeout | reconnecting in 1s

[main] event received: WsDisconnected
2026/05/11 22:56:17 [runtime] websocket disconnected: read failed: read tcp 192.168.0.101:55280->13.192.24.88:443: i/o timeout
2026/05/11 22:56:18 [ws] connecting to Binance Testnet
2026/05/11 22:56:18 [ws] connection lost: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving | reconnecting in 2s

[main] event received: WsDisconnected
2026/05/11 22:56:18 [runtime] websocket disconnected: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving
2026/05/11 22:56:20 [ws] connecting to Binance Testnet
2026/05/11 22:56:20 [ws] connection lost: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving | reconnecting in 4s

[main] event received: WsDisconnected
2026/05/11 22:56:20 [runtime] websocket disconnected: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving
2026/05/11 22:56:24 [ws] connecting to Binance Testnet
2026/05/11 22:56:24 [ws] connection lost: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving | reconnecting in 8s

[main] event received: WsDisconnected
2026/05/11 22:56:24 [runtime] websocket disconnected: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving
2026/05/11 22:56:32 [ws] connecting to Binance Testnet
2026/05/11 22:56:32 [ws] connection lost: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving | reconnecting in 16s

[main] event received: WsDisconnected
2026/05/11 22:56:32 [runtime] websocket disconnected: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving
2026/05/11 22:56:48 [ws] connecting to Binance Testnet
2026/05/11 22:56:48 [ws] connection lost: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving | reconnecting in 30s

[main] event received: WsDisconnected
2026/05/11 22:56:48 [runtime] websocket disconnected: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving
2026/05/11 22:57:18 [ws] connecting to Binance Testnet
2026/05/11 22:57:18 [ws] connection lost: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving | reconnecting in 30s

[main] event received: WsDisconnected
2026/05/11 22:57:18 [runtime] websocket disconnected: dial failed: dial tcp: lookup stream.testnet.binance.vision on 127.0.0.53:53: server misbehaving
2026/05/11 22:57:48 [ws] connecting to Binance Testnet
2026/05/11 22:57:49 [ws] connected

[main] event received: WsConnected
2026/05/11 22:57:49 [runtime] websocket connected

[main] event received: MarketTrade
2026/05/11 22:57:50 [runtime] trade processed: id=1778529470466 symbol=BTCUSDT price=81970.99 qty=0.001000

[main] event received: MarketTrade
2026/05/11 22:57:51 [runtime] trade processed: id=1778529471518 symbol=BTCUSDT price=81970.99 qty=0.001000

...

[main] event received: MarketTrade
2026/05/11 22:58:00 [runtime] trade processed: id=1778529480495 symbol=BTCUSDT price=81971.00 qty=0.000070

[main] event received: MarketTrade
2026/05/11 22:58:03 [runtime] trade processed: id=1778529482901 symbol=BTCUSDT price=81971.00 qty=0.000240
^C2026/05/11 22:58:04 [main] shutdown signal received
2026/05/11 22:58:04 [main] shutdown complete
```

Note: "nmcli networking off/on" disables/enables internet without device names.

# Test 2: Zombie Socket

This will drop all outgoing packets:

```bash
sudo iptables -A OUTPUT -p tcp --dport 443 -j DROP
```

TCP socket still exists, reads hang, ping/pong timeout detects dead connection.

Delete the rule to get back to normal:

```bash
sudo iptables -D OUTPUT -p tcp --dport 443 -j DROP
```

One should get "read failed" and the same recovery as before.

# Test 3: Packet Latency and Loss (Advanced)

```bash
sudo apt install iproute2
sudo tc qdisc add dev enp3s0 root netem loss 50%
```

or

```bash
sudo tc qdisc add dev enp3s0 root netem delay 3000ms
```

Here enp3s0 is the active device name given by "nmcli device status".

To get back to normal:

```bash
sudo tc qdisc del dev enp3s0 root
```

In my case, the bot still works with 50% packet loss: duplicates arrive, occasional disconnect, the code handles it all. However, 70% loss: disconnection and hanging, automatic recovery only after the packet rate improves.

- 10s-delays induce mild duplicates, all handled, no disconnects appear.

- 20s-delays: still no disconnects, more duplicates, packets arrive in bursts.

- 30s-delays: disconnect, with automatic reconnect only when delay diminishes.

Disconnect due to delays works as predicted by params:

```go
pingPeriod = 10s
pongWait   = 30s
SetReadDeadline(now + 30s)
```

When delay is 20s:

t=0 ping sent
t=20 pong arrives, deadline extended to t=50
t=20 next ping sent
t=40 pong arrives, deadline extended to t=70

When delay is 30s:

t=0 ping sent
t=30+ pong arrives slightly late.

One subtlety here:

"
The read deadline applies to all websocket frames, not only pong frames.

If Binance is continuously sending trade data, that itself may satisfy reads.

This is why:

high latency alone does not always disconnect
active market traffic can keep connection alive even if pong is delayed

The websocket is considered alive if anything is read before deadline.
"

# Actor Alternative

I have asked ChatGTP to do the actor-ish implementation as well, the code (albeit with heavy comments) increases from 500 LOC to 1000 LOC. The hope is that actors are easier to understand and debug, but reconnection seems to demand some specific per actor initialization, and the error handling without all that Erlang OTP jungle remains moot. It is not clear if actors are the way, in Go in general, and with this websocket reconnection problem in particular.

Read the code review below.

# Code Review (ChatGPT 5)

Both are already in the “serious prototype” zone, not tutorial sludge. The reconnect/liveness handling is fundamentally correct in both. The interesting part is not “does reconnect work?”, but “what weird failure modes accumulate after 3 weeks in production while the exchange behaves like a caffeinated raccoon chewing ethernet cables”. 🦝⚡

I’ll discuss them separately.

---

# Implementation 1: `main.go`

This one is basically:

- one runtime loop
- one websocket supervisor loop
- one reader goroutine
- one ping goroutine
- shared coordination through channels

Very Go-ish. Small surface area. Easy to reason about.

## Big strengths

### 1. Dead connection detection is actually correct

A lot of bots fail here.

You correctly combined:

- ping frames
- pong deadline extension
- read deadlines

So this handles:

- cable unplug
- wifi death
- frozen TCP session
- upstream blackhole
- NAT timeout

without hanging forever in `ReadMessage()`.

That is the hardest reliability problem in beginner websocket bots and you solved it correctly.

---

### 2. Bounded dedup memory

Huge win.

Most bots accidentally do:

```go
map[int64]struct{}
```

and quietly build a RAM singularity after a month.

Your LRU-ish bounded structure is perfectly adequate.

---

### 3. Reconnect backoff reset after stable uptime

Good operational detail.

Without this, temporary outages permanently poison reconnect cadence.

---

## Big future problems

## 1. Ping goroutine lifetime leak risk

This is the biggest architectural issue.

You do:

```go
go func() {
    ticker := time.NewTicker(...)
    ...
}()
```

per connection.

The goroutine exits on:

- ctx.Done()
- ping write failure

BUT NOT on normal reconnect initiated from read failure.

Meaning:

- read loop exits
- `runConnection()` returns
- connection closes
- old ping goroutine may continue until next write attempt fails

Usually this dies quickly.

But under pathological conditions:

- blocked write
- kernel weirdness
- half-dead socket states

you can temporarily accumulate zombie ping goroutines.

Probably not catastrophic, but ugly over very long runtimes.

---

## 2. Concurrent websocket writes are “accidentally okay”

You currently only write pings.

So:

```go
conn.WriteMessage(PingMessage)
```

from one goroutine is okay.

But later, when you add:

- orders
- subscriptions
- auth refresh
- cancel messages

you WILL accidentally create:

- multiple writers
- concurrent websocket writes
- frame corruption/panics

Gorilla websocket requires a single writer discipline.

Your architecture does not enforce that.

This is the biggest future scaling trap.

---

## 3. Runtime backpressure can stall reconnect logic

This one is subtle and important.

You do:

```go
w.out <- trade
```

directly.

If runtime becomes slow:

- strategy computation
- DB flush
- logging burst
- GC pause

then websocket processing stalls.

That means:

- read loop blocks
- pong handling delays
- reconnect timing gets distorted

In extreme conditions, a slow strategy engine can indirectly kill websocket liveness.

This coupling becomes painful later.

---

## 4. Event channel overload has no policy

You have:

```go
eventChan := make(chan Event, 1024)
```

What happens during:

- reconnect burst
- market spike
- consumer slowdown

?

Eventually:

```go
w.out <- trade
```

blocks entire websocket pipeline.

You need a policy eventually:

- drop
- coalesce
- shard
- spill
- bounded queues per subsystem

Right now everything shares one artery.

---

## 5. No stale connection identity

This becomes important later.

Old goroutines can still report failures after reconnect.

In this version it mostly works because connection lifecycle is tightly scoped inside `runConnection()`.

But once more async pieces appear:

- writer loops
- subscriptions
- auth refresh
- order routing

you’ll eventually process stale events from dead sockets.

Actor version partially solves this.

---

# Implementation 2: `main_actors.go`

This is much more ambitious.

It’s basically:

- explicit websocket state machine
- mailbox ownership
- actor serialization
- I/O adapters

This is architecturally closer to telecom systems than standard Go apps. Tiny Erlang cathedral built from goroutines and channels. ☁️

---

# Big strengths

## 1. Single websocket owner

This is the biggest advantage.

Only actor mutates:

- conn
- state
- reconnect logic
- heartbeat

This prevents entire categories of future bugs.

Especially:

- concurrent writes
- double close
- reconnect races
- stale state mutation

This matters A LOT once trading features appear.

---

## 2. Explicit state machine

Very valuable long-term.

You already have:

- Connecting
- Live
- BackingOff
- Stopping

This becomes gold later when adding:

- authenticated sessions
- resubscription
- order replay
- sequence sync
- snapshot recovery

You already have a place to put those semantics.

---

## 3. Stale failure suppression

This is genuinely good:

```go
if a.state != StateLive {
    return
}
```

You identified a real production issue:

old goroutines reporting delayed failures after reconnect.

Good instinct.

---

## 4. Clean separation of blocking I/O from ownership

This part is actually sophisticated.

Your reader goroutine is intentionally dumb:

```text
socket -> mailbox
```

This prevents “goroutine soup”.

A lot of Go systems decay into:

- goroutine owns socket
- reconnect loop owns socket
- heartbeat owns socket
- auth loop owns socket

then nobody owns anything.

You avoided that.

---

# Big future problems

## 1. Ping loop leak is MUCH worse here

This is the biggest flaw in actor version.

Every successful reconnect launches:

```go
go a.pingLoop(ctx)
```

But old ping loops NEVER stop on reconnect.

They only stop on global shutdown.

That means after 50 reconnects:

you have 50 ping goroutines all sending:

```go
a.inbox <- PingTick{}
```

forever.

This is a real leak.

And not just memory:

- ticker leak
- goroutine leak
- mailbox spam amplification

This WILL bite later.

Ironically the “cleaner” architecture has the more dangerous leak.

---

## 2. Mailbox can become unbounded pressure amplifier

You use:

```go
inbox chan Msg, 4096
```

for EVERYTHING:

- reconnects
- frames
- failures
- ping ticks

Under overload:

- frame spam delays heartbeat
- heartbeat delays reconnect
- reconnect delays failure processing

Single mailbox actor systems often need:

- priorities
- separate control plane
- selective receive
- dropping policies

Otherwise data traffic starves control traffic.

---

## 3. Actor purity is partially fake

This is subtle.

You say:

> only actor owns socket

But actually:

- reader goroutine calls `ReadMessage`
- ping path directly calls `conn.WriteMessage`

So the actor is coordinating ownership, but actual socket access is still externalized.

This is okay NOW.

But later:

- order writes
- subscriptions
- auth refresh
- shutdown frames

you may drift into half-actor half-shared-resource ambiguity.

---

## 4. `a.inbox <- ...` can deadlock adapters

Reader goroutine does:

```go
a.inbox <- SocketFrame{}
```

with blocking send.

If actor stalls:

- mailbox fills
- reader blocks
- socket stops draining
- peer disconnects

This creates backpressure coupling again.

Not necessarily wrong, but important.

---

## 5. Reconnect scheduling races can appear later

Currently mostly okay.

But once you add:

- manual reconnect
- auth refresh
- resubscribe timers
- shutdown ordering

you’ll start needing:

- generation IDs
- cancelable timers
- connection epochs

because delayed reconnect timers may fire after newer successful connections.

You are approaching distributed systems territory.

---

# Which would I choose?

I would choose **the first one** (`main.go`) for a real cryptobot.

Not because actors are bad.

Because:

## The first version has:

- fewer moving parts
- fewer hidden queues
- fewer goroutine lifecycle hazards
- less scheduler coupling
- less architecture gravity

And most importantly:

## It is easier to keep correct under stress.

The actor version is intellectually cleaner, but it is already drifting toward framework-brain:

- internal protocol
- actor messages
- explicit FSM
- adapter goroutines
- mailbox semantics

That complexity only pays off when you truly have:

- many independent websocket sessions
- multiple exchanges
- supervision trees
- distributed workers
- shard routing
- resync orchestration

For ONE exchange connection, it is probably too much cathedral for the village.

---

# My actual recommendation

Take version 1 and steal ONLY these ideas from version 2:

- explicit connection state enum
- stale connection generation IDs
- centralized write ownership
- cleaner separation between control/data flow

But keep:

- single reconnect loop
- single read loop
- small topology
- obvious control flow

That tends to survive production better in Go.

The best Go systems often look “under-designed” right before they quietly run for 900 days without drama.

# TBC...
