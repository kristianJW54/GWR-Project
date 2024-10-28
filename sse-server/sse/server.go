package sse

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

type Server struct {
	EventServer *EventServer
	logger      *slog.Logger
	config      *Config
}

func NewServer(es *EventServer, config *Config, logger *slog.Logger) *Server {
	return &Server{
		EventServer: es,
		logger:      logger,
		config:      config,
	}
}

func ServerHandler(s *Server) http.Handler {

	mux := http.NewServeMux()
	addRoutes(
		mux,
		s.logger,
		s.config,
		s.EventServer)

	var handler http.Handler = mux
	// Can add top level middleware
	return handler
}

// EventServer manages Server-Sent Events (SSE) by handling client connections.
// Each client is represented by a channel of byte slices in the 'clients' map.
// When a client connects a new channel and the connections is added to the map to monitor closures.
// Clients receive messages over their individual channels, and the server uses the HTTP connection
// (via http.ResponseWriter) to push data to them over the established SSE connection.
type EventServer struct {
	context       context.Context
	cancel        context.CancelFunc
	ConnectClient chan chan []byte
	CloseClient   chan chan []byte
	clients       map[chan []byte]struct{} // Map to keep track of connected clients
	sync          sync.Mutex
	subscriber    Subscriber
	logger        *slog.Logger
}

func NewSSEServer(parentCtx context.Context, sub Subscriber, logger *slog.Logger) *EventServer {
	// Create a cancellable context derived from the parent context
	ctx, cancel := context.WithCancel(parentCtx)

	return &EventServer{
		context:       ctx,
		cancel:        cancel, // Store the cancel function to stop the server later
		ConnectClient: make(chan chan []byte),
		CloseClient:   make(chan chan []byte),
		clients:       make(map[chan []byte]struct{}),
		subscriber:    sub,
		logger:        logger,
	}
}

func (sseServer *EventServer) Run() {
	log.Println("Started server")
	for {
		select {
		case <-sseServer.context.Done():
			log.Println("Stopping SSE server")
			for client := range sseServer.clients {
				sseServer.logger.Debug("closing client", "client", client)
				close(client)
				delete(sseServer.clients, client)
			}
			return

		case clientConnection := <-sseServer.ConnectClient:
			sseServer.sync.Lock()
			sseServer.clients[clientConnection] = struct{}{}
			sseServer.logger.Debug("connecting client", "client", sseServer.clients[clientConnection])
			sseServer.sync.Unlock()

		case clientDisconnect := <-sseServer.CloseClient:
			sseServer.sync.Lock()
			sseServer.logger.Debug("closing client", "client", sseServer.clients[clientDisconnect])
			delete(sseServer.clients, clientDisconnect)
			sseServer.sync.Unlock()
		}
	}
}

func (sseServer *EventServer) Stop() {
	sseServer.cancel()
}

//================================================
// Shutdown Logic
//================================================

func GracefulShutdown(
	ctx context.Context,
	cancel context.CancelFunc,
	wg *sync.WaitGroup,
	signalChan chan os.Signal,
	logger *slog.Logger,
	srv *http.Server,
) error {
	defer wg.Done() // Ensure WaitGroup is decremented once

	select {
	case <-ctx.Done():
		logger.Info("Context canceled - initiating graceful shutdown")
	case <-signalChan:
		logger.Info("Received shutdown signal - initiating graceful shutdown")
		cancel() // Cancel main context to propagate shutdown
		// Drain remaining signals if any
		for len(signalChan) > 0 {
			<-signalChan
		}
	}

	// Create a shutdown context with a timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down HTTP server:", "err", err)
		return err
	}

	logger.Info("HTTP server shutdown completed successfully")
	return nil
}

//================================================
// Handling connections
//================================================

func (sseServer *EventServer) HandleConnection(w http.ResponseWriter, req *http.Request) {

	// Set headers to mimic SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	message := make(chan []byte)
	sseServer.ConnectClient <- message

	//keeping the connection alive with keep-alive protocol
	keepAliveTicker := time.NewTicker(15 * time.Second)
	keepAliveMsg := []byte(":keepalive\n\n")
	notify := req.Context().Done()
	serverNotify := sseServer.context.Done()

	go func() {
		select {
		case <-serverNotify:
			sseServer.logger.Debug("sse server context signalled done - closing connection")
			sseServer.CloseClient <- message
			keepAliveTicker.Stop()
		case <-notify:
			sseServer.logger.Debug("request scope context signalled done - closing connection")
			sseServer.CloseClient <- message
			keepAliveTicker.Stop()
		}
	}()

	clientPathRequested := req.URL.Path
	sseServer.logger.Debug("client path requested: ", slog.String("path", clientPathRequested))

	go sseServer.subscriber.Subscribe(sseServer.context, "*", message)

	// Loop to handle sending messages or keep-alive signals
	for {
		select {
		case msg, ok := <-message:
			if !ok {
				return
			}
			_, err := fmt.Fprintf(w, "data: %s\n\n", msg)
			sseServer.logger.Debug("broadcast: ", slog.String("data", string(msg)))
			if err != nil {
				log.Printf("error writing messeage: %v", err)
				return
			}
			flusher.Flush()
		case <-keepAliveTicker.C:
			// Send the keep-alive ping
			_, err := w.Write(keepAliveMsg)
			if err != nil {
				sseServer.logger.Debug("error writing keepalive", "err", err)
				return
			}
			flusher.Flush()
		case <-serverNotify:
			for msg := range message {
				// Handle remaining messages
				if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
					sseServer.logger.Debug("error writing server message", "err", err)
					return
				}
				flusher.Flush()
			}
			sseServer.logger.Debug("no more messages - buffer cleared")
			return
		}
	}

}
