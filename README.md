# cryptobot

Cryptocurrency trading bot. Inspired by 

Nikhil Barthwal. Building Your Own Trading Bot in F#. #LambdaConf2025. [Presentation video](https://www.youtube.com/watch?v=iyx2qIv8DDw&t=2498s), [code](https://github.com/nikhilbarthwal/Vyapari/blob/master/Vyapari/), [slides](https://www.lambdadays.org/static/upload/media/1686573948256988nikhilbarthwalbuildingyourowntradingbotinf.pdf).

Instead of Gemini and Tradier I will use Binance. Instead of F# -> Go, with more emphasis on reliability than trading algorithms. Handling websocket connection drop, data duplication...

The code will rely on a deterministic event loop implemented with two goroutines: one for main.go, and the other for the websocket. Deterministic = no multiple workers mutating shared state.

So far (May 11th, 2026) the program:

- connects to "wss://stream.testnet.binance.vision/ws/btcusdt@trade",

- reads the stream, formats data, and outputs it to the terminal,

- handles ctrl+c shutdown,

- SeenTradeIDs map[int64]struct{} grows forever (no ring buffer, TTL, or LRU cache yet),

- "nmcli networking off" stops the stream which resumes after "nmcli networking on". 

**This is not good enough.** When the internet/connection is lost, the TCP socket is still alive and conn.ReadMessage() blocks. No reconnect/disconnect event, the traffic resumes later. Websockets alone do NOT reliably detect dead connections. 

# Sample Run

```bash
mkdir cryptobot
cd cryptobot
go mod init cryptobot
mkdir -p cmd/bot
nano cmd/bot/main.go
go get github.com/gorilla/websocket
```

```bash
go run ./cmd/bot
go run ./cmd/bot
2026/05/11 01:07:06 [main] runtime started
2026/05/11 01:07:06 [ws] connecting to Binance Testnet
2026/05/11 01:07:08 [ws] connected

[main] event received: WsConnected
2026/05/11 01:07:08 [runtime] websocket connected

[main] event received: MarketTrade
2026/05/11 01:07:08 [runtime] trade processed: id=1778450828372 symbol=BTCUSDT price=81055.66 qty=0.001000

[main] event received: MarketTrade
2026/05/11 01:07:09 [runtime] trade processed: id=1778450829784 symbol=BTCUSDT price=81055.67 qty=0.002000

[main] event received: MarketTrade
2026/05/11 01:07:09 [runtime] trade processed: id=1778450829803 symbol=BTCUSDT price=81055.67 qty=0.001190

...

[main] event received: MarketTrade
2026/05/11 01:07:22 [runtime] trade processed: id=1778450842204 symbol=BTCUSDT price=81206.44 qty=0.001230

[main] event received: MarketTrade
2026/05/11 01:07:22 [runtime] trade processed: id=1778450842629 symbol=BTCUSDT price=81206.45 qty=0.001000

[main] event received: MarketTrade
2026/05/11 01:07:22 [runtime] trade processed: id=1778450842730 symbol=BTCUSDT price=81206.44 qty=0.000340
^C2026/05/11 01:07:23 [main] shutdown signal received
2026/05/11 01:07:23 [ws] shutdown requested
2026/05/11 01:07:23 [main] shutdown complete
```

# Websockets 

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

