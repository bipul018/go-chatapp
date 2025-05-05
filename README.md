## A chat application written in Go that uses websockets

+ Built as a part of learning of the go language
+ Uses gorilla websocket library of go along with the net/http package provided in std library of go
+ Used as a server that serves some static html files first consisting of code to connect using websocket back to the server
+ Then the server maintains the chat with the client through websockets
+ It sends the client the necessary html bits to communicate using websocket
+ Handles concurrency and synchronization using goroutines and channels

Currently assumes no TLS is used to create the websocket connection, it would have to be fixed by editing the js code in the index.html file

There is only a single `main.go` file that contains the entirety of code.

Even inside that, the majority of code is in a single function, reflecting poor design decisions of this novice go-code.
