# cryptobot

Cryptocurrency trading bot. Inspired by

Nikhil Barthwal. Building Your Own Trading Bot in F#. #LambdaConf2025. [Presentation video](https://www.youtube.com/watch?v=iyx2qIv8DDw&t=2498s), [code](https://github.com/nikhilbarthwal/Vyapari/blob/master/Vyapari/), [slides](https://www.lambdadays.org/static/upload/media/1686573948256988nikhilbarthwalbuildingyourowntradingbotinf.pdf).

- Binance instead of Gemini and Tradier.

- Go instead of F#.

- More emphasis on reliability than trading algorithms. Reconnects, data duplication...

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

The industry standard seems to be the following solution (Chatgpt5):

"
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

That is the canonical websocket liveness architecture.
"

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

# Appendix: Why Websockets

Pros:

- low latency,
- server push,
- full duplex,
- widely supported,
- simple,
- cheap.

Cons:

- disconnect constantly,
- silent stale connections,
- message bursts,
- ordering edge cases,
- reconnect complexity,
- state resync pain.

Better protocol?

For HFT:

- raw TCP,
- FIX,
- proprietary binary protocols,
- multicast feeds.

But: massively more difficult.
