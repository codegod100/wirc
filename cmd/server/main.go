package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type    string `json:"type"`
	Channel string `json:"channel,omitempty"`
	User    string `json:"user,omitempty"`
	Text    string `json:"text,omitempty"`
	Server  string `json:"server,omitempty"`
	Port    string `json:"port,omitempty"`
	Nick    string `json:"nick,omitempty"`
}

type IRCState struct {
	Server    string          `json:"server"`
	Port      string          `json:"port"`
	Nickname  string          `json:"nickname"`
	Channels  map[string]bool `json:"channels"`
	Connected bool            `json:"connected"`
}

type CombinedServer struct {
	// IRC connection
	ircConn      net.Conn
	nickname     string
	server       string
	port         string
	channels     map[string]bool
	ircConnected bool
	ircMu        sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc

	// WebSocket
	wsClient *websocket.Conn
	wsMu     sync.RWMutex

	// Message handling
	history    []Message
	maxHistory int
	historyMu  sync.RWMutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewCombinedServer() *CombinedServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &CombinedServer{
		channels:   make(map[string]bool),
		history:    make([]Message, 0),
		maxHistory: 100,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// IRC Management Methods
func (s *CombinedServer) connectToIRC(server, port, nickname string) error {
	s.ircMu.Lock()
	defer s.ircMu.Unlock()

	if port == "" {
		port = "6667"
	}

	serverAddr := server + ":" + port
	log.Printf("IRC: Connecting to %s with nickname: %s", serverAddr, nickname)

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Printf("IRC: Failed to connect to %s: %v", serverAddr, err)
		return err
	}

	// Close existing connection if any
	if s.ircConn != nil {
		s.ircConn.Close()
	}

	s.ircConn = conn
	s.server = server
	s.port = port
	s.nickname = nickname
	s.ircConnected = true

	// Send IRC handshake
	fmt.Fprintf(conn, "NICK %s\r\n", nickname)
	fmt.Fprintf(conn, "USER %s 0 * :%s\r\n", nickname, nickname)

	log.Printf("IRC: Successfully connected to %s", serverAddr)

	// Start reading IRC messages
	go s.ircReadLoop()

	// Notify WebSocket client
	s.sendToWebSocket(Message{
		Type: "connected",
		Text: "Connected to " + serverAddr,
	})

	return nil
}

func (s *CombinedServer) disconnectFromIRC() {
	log.Printf("IRC: Starting disconnect...")

	s.ircMu.Lock()
	defer s.ircMu.Unlock()

	if s.ircConn != nil {
		log.Printf("IRC: Disconnecting from %s", s.server)
		s.ircConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprintf(s.ircConn, "QUIT :Client disconnecting\r\n")
		s.ircConn.Close()
		s.ircConn = nil
	}
	s.ircConnected = false
	log.Printf("IRC: Disconnect complete")

	s.sendToWebSocket(Message{
		Type: "disconnected",
		Text: "Disconnected from IRC",
	})
}

func (s *CombinedServer) joinChannel(channel string) {
	s.ircMu.Lock()
	defer s.ircMu.Unlock()

	if !s.ircConnected || s.ircConn == nil {
		log.Printf("IRC: Cannot join channel %s: not connected", channel)
		return
	}

	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	log.Printf("IRC: Joining channel: %s", channel)
	fmt.Fprintf(s.ircConn, "JOIN %s\r\n", channel)
	s.channels[channel] = true

	// Auto-request names list for the channel
	log.Printf("IRC: Requesting names for channel: %s", channel)
	fmt.Fprintf(s.ircConn, "NAMES %s\r\n", channel)
	log.Printf("IRC: NAMES command sent for %s", channel)
}

func (s *CombinedServer) sendMessage(channel, text string) {
	s.ircMu.RLock()

	if !s.ircConnected || s.ircConn == nil {
		log.Printf("IRC: Cannot send message to %s: not connected", channel)
		s.ircMu.RUnlock()
		return
	}

	log.Printf("IRC: Sending message to %s: %s", channel, text)
	fmt.Fprintf(s.ircConn, "PRIVMSG %s :%s\r\n", channel, text)

	// Create local echo
	msg := Message{
		Type:    "message",
		Channel: channel,
		User:    s.nickname,
		Text:    text,
	}

	s.ircMu.RUnlock()

	// Send to WebSocket and add to history
	s.sendToWebSocket(msg)
	s.addToHistory(msg)
}

func (s *CombinedServer) requestNames(channel string) {
	s.ircMu.RLock()
	defer s.ircMu.RUnlock()

	if !s.ircConnected || s.ircConn == nil {
		log.Printf("IRC: Cannot request names for %s: not connected", channel)
		return
	}

	log.Printf("IRC: Requesting names for channel: %s", channel)
	fmt.Fprintf(s.ircConn, "NAMES %s\r\n", channel)
	log.Printf("IRC: NAMES command sent for %s", channel)
}

func (s *CombinedServer) getIRCState() IRCState {
	s.ircMu.RLock()
	defer s.ircMu.RUnlock()

	// Copy channels map
	channels := make(map[string]bool)
	for k, v := range s.channels {
		channels[k] = v
	}

	return IRCState{
		Server:    s.server,
		Port:      s.port,
		Nickname:  s.nickname,
		Channels:  channels,
		Connected: s.ircConnected,
	}
}

func (s *CombinedServer) ircReadLoop() {
	defer func() {
		log.Printf("IRC: Read loop exiting")
	}()

	scanner := bufio.NewScanner(s.ircConn)

	for scanner.Scan() {
		select {
		case <-s.ctx.Done():
			log.Printf("IRC: Read loop cancelled")
			return
		default:
			line := scanner.Text()
			log.Printf("IRC <- %s", line)
			s.parseIRCMessage(line)
		}
	}

	// Connection lost
	s.ircMu.Lock()
	wasConnected := s.ircConnected
	s.ircConnected = false
	s.ircMu.Unlock()

	if wasConnected {
		if err := scanner.Err(); err != nil {
			log.Printf("IRC: Connection read error: %v", err)
			s.sendToWebSocket(Message{
				Type: "error",
				Text: "IRC connection lost: " + err.Error(),
			})
		} else {
			log.Printf("IRC: Connection closed gracefully")
		}
	}
}

func (s *CombinedServer) parseIRCMessage(line string) {
	if strings.HasPrefix(line, "PING") {
		pong := strings.Replace(line, "PING", "PONG", 1)
		log.Printf("IRC -> %s", pong)
		fmt.Fprintf(s.ircConn, "%s\r\n", pong)
		return
	}

	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return
	}

	switch parts[1] {
	case "PRIVMSG":
		if len(parts) >= 4 {
			sender := strings.Split(parts[0], "!")[0][1:]
			channel := parts[2]
			message := strings.Join(parts[3:], " ")[1:]

			// Only process if it's not from ourselves (avoid duplicates)
			if sender != s.nickname {
				msg := Message{
					Type:    "message",
					Channel: channel,
					User:    sender,
					Text:    message,
				}
				s.sendToWebSocket(msg)
				s.addToHistory(msg)
			}
		}
	case "JOIN":
		if len(parts) >= 3 {
			sender := strings.Split(parts[0], "!")[0][1:]
			channel := parts[2]

			msg := Message{
				Type:    "join",
				Channel: channel,
				User:    sender,
			}
			s.sendToWebSocket(msg)
			s.addToHistory(msg)
		}
	case "353": // Names list
		log.Printf("IRC: Got 353 NAMES response with %d parts: %v", len(parts), parts)
		// Format: :server 353 nick = #channel :user1 user2 user3
		// or:     :server 353 nick @ #channel :user1 user2 user3
		var channel string
		var usersStart int

		// Find the channel (starts with # or &)
		for i := 3; i < len(parts); i++ {
			if strings.HasPrefix(parts[i], "#") || strings.HasPrefix(parts[i], "&") {
				channel = parts[i]
				usersStart = i + 1
				break
			}
		}

		if channel != "" && usersStart < len(parts) {
			users := strings.Join(parts[usersStart:], " ")
			// Remove leading : if present
			if strings.HasPrefix(users, ":") {
				users = users[1:]
			}

			log.Printf("IRC: Users in %s: %s", channel, users)

			msg := Message{
				Type:    "users",
				Channel: channel,
				Text:    users,
			}
			log.Printf("IRC: Sending users message to WebSocket: channel=%s, users=%s", channel, users)
			s.sendToWebSocket(msg)
			s.addToHistory(msg)
		} else {
			log.Printf("IRC: Could not parse 353 message: %v", parts)
		}
	case "001": // Welcome message
		log.Printf("IRC: Successfully registered with server")
		s.sendToWebSocket(Message{
			Type: "system",
			Text: "Successfully registered with IRC server",
		})
	}
}

// WebSocket Methods
func (s *CombinedServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("WebSocket: New connection from %s", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket: Upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	s.wsMu.Lock()
	// Close existing connection if any
	if s.wsClient != nil {
		s.wsClient.Close()
	}
	s.wsClient = conn
	s.wsMu.Unlock()

	log.Printf("WebSocket: Client connected")

	// Send current IRC state
	state := s.getIRCState()
	if state.Connected {
		conn.WriteJSON(Message{
			Type:   "state",
			Text:   "reconnected",
			Server: state.Server,
			Port:   state.Port,
			Nick:   state.Nickname,
		})

		// Send channel list
		for channel := range state.Channels {
			conn.WriteJSON(Message{
				Type:    "channel_joined",
				Channel: channel,
			})
		}

		// Send message history
		history := s.getHistory()
		log.Printf("WebSocket: Sending %d history messages", len(history))
		for _, msg := range history {
			conn.WriteJSON(msg)
		}
	}

	// Handle incoming messages
	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Printf("WebSocket: Read error: %v", err)
			s.wsMu.Lock()
			if s.wsClient == conn {
				s.wsClient = nil
			}
			s.wsMu.Unlock()
			break
		}

		log.Printf("WebSocket: Received message: type=%s", msg.Type)
		s.handleWebSocketMessage(msg)
	}
}

func (s *CombinedServer) handleWebSocketMessage(msg Message) {
	switch msg.Type {
	case "connect":
		err := s.connectToIRC(msg.Server, msg.Port, msg.Nick)
		if err != nil {
			s.sendToWebSocket(Message{
				Type: "error",
				Text: "Failed to connect: " + err.Error(),
			})
		}
	case "disconnect":
		s.disconnectFromIRC()
	case "join":
		s.joinChannel(msg.Channel)
	case "message":
		s.sendMessage(msg.Channel, msg.Text)
	case "names":
		log.Printf("WebSocket: Received names request for channel: %s", msg.Channel)
		s.requestNames(msg.Channel)
	case "status":
		state := s.getIRCState()
		s.sendToWebSocket(Message{
			Type: "status",
			Text: encodeState(state),
		})
	}
}

func (s *CombinedServer) sendToWebSocket(msg Message) {
	s.wsMu.RLock()
	client := s.wsClient
	s.wsMu.RUnlock()

	if client != nil {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Printf("WebSocket: Failed to send message: %v", err)
			s.wsMu.Lock()
			if s.wsClient == client {
				s.wsClient.Close()
				s.wsClient = nil
			}
			s.wsMu.Unlock()
		}
	}
}

// History Management
func (s *CombinedServer) addToHistory(msg Message) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()

	s.history = append(s.history, msg)
	if len(s.history) > s.maxHistory {
		s.history = s.history[len(s.history)-s.maxHistory:]
	}
}

func (s *CombinedServer) getHistory() []Message {
	s.historyMu.RLock()
	defer s.historyMu.RUnlock()

	history := make([]Message, len(s.history))
	copy(history, s.history)
	return history
}

func encodeState(state IRCState) string {
	data, _ := json.Marshal(state)
	return string(data)
}

func main() {
	log.Printf("Starting WIRC combined server...")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := NewCombinedServer()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Printf("Received shutdown signal")
		server.cancel()
		server.disconnectFromIRC()
		os.Exit(0)
	}()

	// HTTP handlers
	http.HandleFunc("/ws", server.handleWebSocket)
	http.Handle("/", http.FileServer(http.Dir("./static/")))

	log.Printf("Combined server starting on :%s", port)
	log.Printf("Serving static files from ./static/")
	log.Printf("WebSocket endpoint: /ws")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
