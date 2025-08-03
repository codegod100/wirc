package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type IRCDaemon struct {
	ircManager *IRCManager
	httpServer *http.Server
}

func NewIRCDaemon() *IRCDaemon {
	return &IRCDaemon{
		ircManager: NewIRCManager(),
	}
}

func (d *IRCDaemon) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	state := d.ircManager.GetState()
	json.NewEncoder(w).Encode(state)
}

func (d *IRCDaemon) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Server string `json:"server"`
		Port   string `json:"port"`
		Nick   string `json:"nick"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	err := d.ircManager.Connect(req.Server, req.Port, req.Nick)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "connected"})
}

func (d *IRCDaemon) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	d.ircManager.Disconnect()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
}

func (d *IRCDaemon) handleJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Channel string `json:"channel"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	d.ircManager.JoinChannel(req.Channel)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "joined"})
}

func (d *IRCDaemon) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	d.ircManager.SendMessage(req.Channel, req.Text)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}

func (d *IRCDaemon) handleNames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Channel string `json:"channel"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	d.ircManager.RequestNames(req.Channel)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "requested"})
}

func (d *IRCDaemon) handleMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	messageCh := d.ircManager.GetMessageChannel()

	for {
		select {
		case msg := <-messageCh:
			data, _ := json.Marshal(msg)
			w.Write([]byte("data: " + string(data) + "\n\n"))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (d *IRCDaemon) start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/state", d.handleState)
	mux.HandleFunc("/connect", d.handleConnect)
	mux.HandleFunc("/disconnect", d.handleDisconnect)
	mux.HandleFunc("/join", d.handleJoin)
	mux.HandleFunc("/message", d.handleMessage)
	mux.HandleFunc("/names", d.handleNames)
	mux.HandleFunc("/messages", d.handleMessages)

	d.httpServer = &http.Server{
		Addr:    ":8081",
		Handler: mux,
	}

	log.Printf("IRC Daemon starting on :8081")
	log.Printf("API endpoints:")
	log.Printf("  GET  /state     - Get current IRC state")
	log.Printf("  POST /connect   - Connect to IRC server")
	log.Printf("  POST /disconnect - Disconnect from IRC")
	log.Printf("  POST /join      - Join IRC channel")
	log.Printf("  POST /message   - Send IRC message")
	log.Printf("  POST /names     - Request channel users list")
	log.Printf("  GET  /messages  - Stream IRC messages (SSE)")

	if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("IRC Daemon: HTTP server failed: %v", err)
	}
}

func (d *IRCDaemon) shutdown() {
	log.Printf("IRC Daemon: Shutting down...")

	if d.ircManager != nil {
		d.ircManager.Disconnect()
	}

	if d.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d.httpServer.Shutdown(ctx)
	}
}
func main() {
	daemon := NewIRCDaemon()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		daemon.shutdown()
		os.Exit(0)
	}()

	daemon.start()
}
