# cryptobot

Cryptocurrency trading bot. Inspired by 

Building Your Own Trading Bot in F# By Nikhil Barthwal, #LambdaConf2005. [Presentation video](https://www.youtube.com/watch?v=iyx2qIv8DDw&t=2498s), [code](https://github.com/nikhilbarthwal/Vyapari/blob/master/Vyapari/).

Instead of Gemini and Tradier I will use Binance. 

Also Go instead of F#, with more emphasis on reliability than trading algorithms. Handling properly websocket connection drop, data duplication, that sort of stuff.

This is the start of development (May 9th, 2026), just main.go with flow layout, graceful shutdown, 
and a tiny simulation to test it all, for now. Nothing else done yet.

```bash
mkdir cryptobot
cd cryptobot
go mod init cryptobot
mkdir -p cmd/bot
nano cmd/bot/main.go
```

```bash
go run ./cmd/bot
2026/05/09 20:50:22 [main] runtime started
2026/05/09 20:50:22 [ws] connecting

[main] event received: WsConnected
2026/05/09 20:50:22 [runtime] websocket connected

[main] event received: MarketTrade
2026/05/09 20:50:23 [runtime] trade processed: id=1001 symbol=BTCUSDT price=108500.10 qty=0.0010

[main] event received: MarketTrade
2026/05/09 20:50:24 [runtime] trade processed: id=1002 symbol=BTCUSDT price=108500.50 qty=0.0020

[main] event received: MarketTrade
2026/05/09 20:50:25 [runtime] duplicate trade ignored: id=1002
2026/05/09 20:50:26 [ws] connection lost: server closed connection | reconnecting in 1s

[main] event received: MarketTrade
2026/05/09 20:50:26 [runtime] trade processed: id=1003 symbol=BTCUSDT price=108501.25 qty=0.0040

[main] event received: WsDisconnected
2026/05/09 20:50:26 [runtime] websocket disconnected: server closed connection
2026/05/09 20:50:27 [ws] connecting

[main] event received: WsConnected
2026/05/09 20:50:27 [runtime] websocket connected

[main] event received: MarketTrade
2026/05/09 20:50:28 [runtime] duplicate trade ignored: id=1001

[main] event received: MarketTrade
2026/05/09 20:50:29 [runtime] duplicate trade ignored: id=1002
2026/05/09 20:50:30 [ws] connection lost: simulated EOF | reconnecting in 2s

[main] event received: WsDisconnected
2026/05/09 20:50:30 [runtime] websocket disconnected: simulated EOF
2026/05/09 20:50:32 [ws] connecting

[main] event received: WsConnected
2026/05/09 20:50:32 [runtime] websocket connected

[main] event received: MarketTrade
2026/05/09 20:50:33 [runtime] duplicate trade ignored: id=1001

[main] event received: MarketTrade
2026/05/09 20:50:34 [runtime] duplicate trade ignored: id=1002

[main] event received: MarketTrade
2026/05/09 20:50:35 [runtime] duplicate trade ignored: id=1002

[main] event received: MarketTrade
2026/05/09 20:50:36 [runtime] duplicate trade ignored: id=1003
2026/05/09 20:50:36 [ws] connection lost: server closed connection | reconnecting in 4s

[main] event received: WsDisconnected
2026/05/09 20:50:36 [runtime] websocket disconnected: server closed connection
^C2026/05/09 20:50:36 [main] shutdown signal received
2026/05/09 20:50:37 [main] shutdown complete
```
