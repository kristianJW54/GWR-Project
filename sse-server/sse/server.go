package sse

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EventServer struct manages client connections and broadcasts messages to those clients using SSE.
// Channels are used to handle client registration, de-registration and broadcasting messages concurrently
type EventServer struct {
	NewClientChannel     chan chan []byte //Using chan chan - as the chan []byte receives another chan (chan []byte)
	ClosingClientChannel chan chan []byte //Using chan chan - as the chan []byte receives another chan (chan []byte)
	broadcast            chan []byte
	clients              map[chan []byte]struct{} // A map that keeps track of all connected clients - empty struct{} used to indicate presence without allocating memory
	RedisClient          RedisSubscriber
	mu                   sync.Mutex
}

func NewSSEServer(redisClient RedisSubscriber) *EventServer {
	return &EventServer{
		NewClientChannel:     make(chan chan []byte),
		ClosingClientChannel: make(chan chan []byte),
		broadcast:            make(chan []byte),
		clients:              make(map[chan []byte]struct{}),
		RedisClient:          redisClient,
	}
}

// Start is the main loop of the SSEServer
func (sseServer *EventServer) Start() {
	for {
		select {
		case client := <-sseServer.NewClientChannel:
			sseServer.mu.Lock()
			sseServer.clients[client] = struct{}{}
			sseServer.mu.Unlock()

		case client := <-sseServer.ClosingClientChannel:
			sseServer.mu.Lock()
			delete(sseServer.clients, client)
			sseServer.mu.Unlock()

		case message := <-sseServer.broadcast:
			for client := range sseServer.clients {
				select {
				case client <- message: // Publish ...?
				default:
					close(client)
					delete(sseServer.clients, client)
				}
			}
		}
	}
}

func (sseServer *EventServer) handleClientConnection(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)

	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Can insert an interface which activates these methods
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")

	messageChan := make(chan []byte)

	// Signalling the server of a new client connection
	sseServer.NewClientChannel <- messageChan

	keepAlive := time.NewTicker(30 * time.Second)
	notify := req.Context().Done()

	go func() {
		<-notify
		log.Println("Client connection closed")
		sseServer.ClosingClientChannel <- messageChan
		keepAlive.Stop()
	}()

	defer func() {
		sseServer.ClosingClientChannel <- messageChan
	}()

	// Redis Pub/Sub logic below

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	// Handle pattern match for channels
	channel := strings.TrimPrefix(req.URL.Path, "/events/")
	if channel == "" {
		channel = "*"
	} else {
		channel = fmt.Sprintf("%s.*", channel)
	}

	go sseServer.RedisClient.RedisSubscriber(ctx, channel, messageChan)

	for {
		select {
		case <-keepAlive.C:
			fmt.Fprintf(w, ":keepalive\n\n")
			flusher.Flush()
		case message := <-messageChan:
			fmt.Fprintf(w, "message: %s\n\n", message)
			flusher.Flush()
		}
	}
}
