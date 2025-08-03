package main

import (
	"encoding/json"
	"log"
	"net/http"
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

type WebSocketServer struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan Message
	ircManager *IRCManager
	mu         sync.RWMutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewWebSocketServer() *WebSocketServer {
	return &WebSocketServer{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan Message, 100),
		ircManager: NewIRCManager(),
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
	if ws.ircManager.IsConnected() {
		state := ws.ircManager.GetState()
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
		err := ws.ircManager.Connect(msg.Server, msg.Port, msg.Nick)
		if err != nil {
			ws.broadcast <- Message{
				Type: "error",
				Text: "Failed to connect: " + err.Error(),
			}
		}
	case "disconnect":
		ws.ircManager.Disconnect()
		ws.broadcast <- Message{
			Type: "disconnected",
			Text: "Disconnected from IRC",
		}
	case "join":
		ws.ircManager.JoinChannel(msg.Channel)
	case "message":
		ws.ircManager.SendMessage(msg.Channel, msg.Text)
	case "status":
		state := ws.ircManager.GetState()
		ws.broadcast <- Message{
			Type: "status",
			Text: encodeState(state),
		}
	}
}

func (ws *WebSocketServer) broadcastToClients() {
	for {
		select {
		case msg := <-ws.broadcast:
			ws.sendToAllClients(msg)
		case ircMsg := <-ws.ircManager.GetMessageChannel():
			ws.sendToAllClients(ircMsg)
		}
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

func encodeState(state IRCState) string {
	data, _ := json.Marshal(state)
	return string(data)
}

func main() {
	log.Printf("Starting WIRC server...")

	server := NewWebSocketServer()
	go server.broadcastToClients()

	http.HandleFunc("/ws", server.handleConnections)
	http.Handle("/", http.FileServer(http.Dir("./static/")))

	log.Printf("WebSocket server starting on :8080")
	log.Printf("Serving static files from ./static/")
	log.Printf("IRC connections will persist across frontend reconnects")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
