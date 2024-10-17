package sse

import (
	"context"
	"log"
	"sync"
)

type EventServer struct {
	broadcast     chan []byte
	context       context.Context
	cancel        context.CancelFunc
	ConnectClient chan chan []byte
	CloseClient   chan chan []byte
	clients       map[chan []byte]struct{} // Map to keep track of connected clients
	sync          sync.Mutex
}

func NewSSEServer(parentCtx context.Context) *EventServer {
	// Create a cancellable context derived from the parent context
	ctx, cancel := context.WithCancel(parentCtx)

	return &EventServer{
		broadcast:     make(chan []byte),
		context:       ctx,
		cancel:        cancel, // Store the cancel function to stop the server later
		ConnectClient: make(chan chan []byte),
		CloseClient:   make(chan chan []byte),
		clients:       make(map[chan []byte]struct{}),
	}
}

func (sseServer *EventServer) Run() {
	for {
		select {
		case <-sseServer.context.Done():
			log.Println("stopping sse server")
			for client := range sseServer.clients {
				log.Println("closing client: ", client)
				close(client)
				delete(sseServer.clients, client)
			}
			return

		case clientConnection := <-sseServer.ConnectClient:
			sseServer.sync.Lock()
			log.Println("client connected")
			sseServer.clients[clientConnection] = struct{}{}
			sseServer.sync.Unlock()

		case clientDisconnect := <-sseServer.CloseClient:
			sseServer.sync.Lock()
			log.Println("client disconnected")
			delete(sseServer.clients, clientDisconnect)
			sseServer.sync.Unlock()

		case message := <-sseServer.broadcast:
			for clientConnection := range sseServer.clients {
				log.Println("client:", clientConnection)
				log.Println("broadcast:", string(message))
			}
		}
	}
}

func (sseServer *EventServer) Stop() {
	sseServer.cancel()
}
