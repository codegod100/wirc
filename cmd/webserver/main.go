package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

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

type WebSocketServer struct {
	clients   map[*websocket.Conn]bool
	broadcast chan Message
	mu        sync.RWMutex
	daemonURL string
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewWebSocketServer(daemonURL string) *WebSocketServer {
	return &WebSocketServer{
		clients:   make(map[*websocket.Conn]bool),
		broadcast: make(chan Message, 100),
		daemonURL: daemonURL,
	}
}

func (ws *WebSocketServer) handleConnections(w http.ResponseWriter, r *http.Request) {
	log.Printf("WebSocket: New connection from %s", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket: Upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ws.mu.Lock()
	ws.clients[conn] = true
	clientCount := len(ws.clients)
	ws.mu.Unlock()

	log.Printf("WebSocket: Client connected. Total clients: %d", clientCount)

	// Send current IRC state to new client
	state, err := ws.getIRCState()
	if err == nil && state.Connected {
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
	}

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Printf("WebSocket: Read error from %s: %v", r.RemoteAddr, err)
			ws.mu.Lock()
			delete(ws.clients, conn)
			clientCount := len(ws.clients)
			ws.mu.Unlock()
			log.Printf("WebSocket: Client disconnected. Total clients: %d", clientCount)
			break
		}

		log.Printf("WebSocket: Received message: type=%s, channel=%s, server=%s, port=%s, nick=%s",
			msg.Type, msg.Channel, msg.Server, msg.Port, msg.Nick)

		ws.handleMessage(msg)
	}
}

func (ws *WebSocketServer) handleMessage(msg Message) {
	switch msg.Type {
	case "connect":
		err := ws.connectToIRC(msg.Server, msg.Port, msg.Nick)
		if err != nil {
			ws.broadcast <- Message{
				Type: "error",
				Text: "Failed to connect: " + err.Error(),
			}
		}
	case "disconnect":
		ws.disconnectFromIRC()
		ws.broadcast <- Message{
			Type: "disconnected",
			Text: "Disconnected from IRC",
		}
	case "join":
		ws.joinChannel(msg.Channel)
	case "message":
		ws.sendMessage(msg.Channel, msg.Text)
	case "names":
		ws.requestNames(msg.Channel)
	case "status":
		state, err := ws.getIRCState()
		if err != nil {
			ws.broadcast <- Message{
				Type: "error",
				Text: "Failed to get status: " + err.Error(),
			}
		} else {
			ws.broadcast <- Message{
				Type: "status",
				Text: encodeState(state),
			}
		}
	}
}

func (ws *WebSocketServer) connectToIRC(server, port, nick string) error {
	reqBody, _ := json.Marshal(map[string]string{
		"server": server,
		"port":   port,
		"nick":   nick,
	})

	resp, err := http.Post(ws.daemonURL+"/connect", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("IRC daemon connect error: %s", string(body))
		return err
	}

	return nil
}

func (ws *WebSocketServer) disconnectFromIRC() error {
	resp, err := http.Post(ws.daemonURL+"/disconnect", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (ws *WebSocketServer) joinChannel(channel string) error {
	reqBody, _ := json.Marshal(map[string]string{"channel": channel})
	resp, err := http.Post(ws.daemonURL+"/join", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (ws *WebSocketServer) sendMessage(channel, text string) error {
	reqBody, _ := json.Marshal(map[string]string{
		"channel": channel,
		"text":    text,
	})
	resp, err := http.Post(ws.daemonURL+"/message", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (ws *WebSocketServer) requestNames(channel string) error {
	reqBody, _ := json.Marshal(map[string]string{"channel": channel})
	resp, err := http.Post(ws.daemonURL+"/names", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (ws *WebSocketServer) getIRCState() (*IRCState, error) {
	resp, err := http.Get(ws.daemonURL + "/state")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var state IRCState
	err = json.NewDecoder(resp.Body).Decode(&state)
	return &state, err
}

func (ws *WebSocketServer) streamMessages() {
	resp, err := http.Get(ws.daemonURL + "/messages")
	if err != nil {
		log.Printf("Failed to connect to IRC daemon message stream: %v", err)
		return
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if err != nil {
			log.Printf("Error reading message stream: %v", err)
			break
		}

		lines := bytes.Split(buf[:n], []byte("\n"))
		for _, line := range lines {
			lineStr := string(line)
			if len(lineStr) > 6 && lineStr[:6] == "data: " {
				var msg Message
				if err := json.Unmarshal([]byte(lineStr[6:]), &msg); err == nil {
					ws.sendToAllClients(msg)
				}
			}
		}
	}
}

func (ws *WebSocketServer) broadcastToClients() {
	go ws.streamMessages()

	for msg := range ws.broadcast {
		ws.sendToAllClients(msg)
	}
}

func (ws *WebSocketServer) sendToAllClients(msg Message) {
	log.Printf("WebSocket: Broadcasting message: type=%s, channel=%s, user=%s",
		msg.Type, msg.Channel, msg.User)

	ws.mu.RLock()
	clientCount := len(ws.clients)
	for client := range ws.clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Printf("WebSocket: Failed to send message to client: %v", err)
			client.Close()
			delete(ws.clients, client)
		}
	}
	ws.mu.RUnlock()

	log.Printf("WebSocket: Message sent to %d clients", clientCount)
}

func encodeState(state *IRCState) string {
	data, _ := json.Marshal(state)
	return string(data)
}

func main() {
	log.Printf("Starting WIRC web server...")

	daemonURL := os.Getenv("IRC_DAEMON_URL")
	if daemonURL == "" {
		daemonURL = "http://localhost:8081"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := NewWebSocketServer(daemonURL)
	go server.broadcastToClients()

	http.HandleFunc("/ws", server.handleConnections)
	http.Handle("/", http.FileServer(http.Dir("./static/")))

	log.Printf("WebSocket server starting on :%s", port)
	log.Printf("Serving static files from ./static/")
	log.Printf("Connecting to IRC daemon at %s", daemonURL)
	log.Printf("IRC connections managed by separate daemon process")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
