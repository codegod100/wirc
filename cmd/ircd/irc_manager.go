package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
)

type IRCManager struct {
	conn        net.Conn
	nickname    string
	server      string
	port        string
	channels    map[string]bool
	connected   bool
	mu          sync.RWMutex
	messagesCh  chan Message
	reconnectCh chan bool
}

type IRCState struct {
	Server    string          `json:"server"`
	Port      string          `json:"port"`
	Nickname  string          `json:"nickname"`
	Channels  map[string]bool `json:"channels"`
	Connected bool            `json:"connected"`
}

type Message struct {
	Type    string `json:"type"`
	Channel string `json:"channel,omitempty"`
	User    string `json:"user,omitempty"`
	Text    string `json:"text,omitempty"`
	Server  string `json:"server,omitempty"`
	Port    string `json:"port,omitempty"`
	Nick    string `json:"nick,omitempty"`
}

func NewIRCManager() *IRCManager {
	return &IRCManager{
		channels:    make(map[string]bool),
		messagesCh:  make(chan Message, 100),
		reconnectCh: make(chan bool, 1),
	}
}

func (irc *IRCManager) Connect(server, port, nickname string) error {
	irc.mu.Lock()
	defer irc.mu.Unlock()

	if port == "" {
		port = "6667"
	}

	serverAddr := server + ":" + port
	log.Printf("IRC Manager: Attempting to connect to %s with nickname: %s", serverAddr, nickname)

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Printf("IRC Manager: Failed to connect to %s: %v", serverAddr, err)
		return err
	}

	// Close existing connection if any
	if irc.conn != nil {
		irc.conn.Close()
	}

	irc.conn = conn
	irc.server = server
	irc.port = port
	irc.nickname = nickname
	irc.connected = true

	// Send IRC handshake
	fmt.Fprintf(conn, "NICK %s\r\n", nickname)
	fmt.Fprintf(conn, "USER %s 0 * :%s\r\n", nickname, nickname)

	log.Printf("IRC Manager: Successfully connected to %s", serverAddr)

	// Start reading IRC messages
	go irc.readLoop()

	// Notify about connection
	irc.messagesCh <- Message{
		Type: "connected",
		Text: "Connected to " + serverAddr,
	}

	return nil
}

func (irc *IRCManager) Disconnect() {
	irc.mu.Lock()
	defer irc.mu.Unlock()

	if irc.conn != nil {
		log.Printf("IRC Manager: Disconnecting from %s", irc.server)
		irc.conn.Close()
		irc.conn = nil
	}
	irc.connected = false
}

func (irc *IRCManager) IsConnected() bool {
	irc.mu.RLock()
	defer irc.mu.RUnlock()
	return irc.connected
}

func (irc *IRCManager) GetState() IRCState {
	irc.mu.RLock()
	defer irc.mu.RUnlock()

	// Copy channels map
	channels := make(map[string]bool)
	for k, v := range irc.channels {
		channels[k] = v
	}

	return IRCState{
		Server:    irc.server,
		Port:      irc.port,
		Nickname:  irc.nickname,
		Channels:  channels,
		Connected: irc.connected,
	}
}

func (irc *IRCManager) JoinChannel(channel string) {
	irc.mu.Lock()
	defer irc.mu.Unlock()

	if !irc.connected || irc.conn == nil {
		log.Printf("IRC Manager: Cannot join channel %s: not connected", channel)
		return
	}

	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	log.Printf("IRC Manager: Joining channel: %s", channel)
	fmt.Fprintf(irc.conn, "JOIN %s\r\n", channel)
	irc.channels[channel] = true
}

func (irc *IRCManager) SendMessage(channel, text string) {
	irc.mu.RLock()
	defer irc.mu.RUnlock()

	if !irc.connected || irc.conn == nil {
		log.Printf("IRC Manager: Cannot send message to %s: not connected", channel)
		return
	}

	log.Printf("IRC Manager: Sending message to %s: %s", channel, text)
	fmt.Fprintf(irc.conn, "PRIVMSG %s :%s\r\n", channel, text)

	// Echo back to frontend
	irc.messagesCh <- Message{
		Type:    "message",
		Channel: channel,
		User:    irc.nickname,
		Text:    text,
	}
}

func (irc *IRCManager) RequestNames(channel string) {
	irc.mu.RLock()
	defer irc.mu.RUnlock()

	if !irc.connected || irc.conn == nil {
		log.Printf("IRC Manager: Cannot request names for %s: not connected", channel)
		return
	}

	log.Printf("IRC Manager: Requesting names for channel: %s", channel)
	fmt.Fprintf(irc.conn, "NAMES %s\r\n", channel)
}

func (irc *IRCManager) GetMessageChannel() <-chan Message {
	return irc.messagesCh
}

func (irc *IRCManager) readLoop() {
	scanner := bufio.NewScanner(irc.conn)
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("IRC Manager <- %s", line)
		irc.parseMessage(line)
	}

	// Connection lost
	irc.mu.Lock()
	irc.connected = false
	irc.mu.Unlock()

	if err := scanner.Err(); err != nil {
		log.Printf("IRC Manager: Connection read error: %v", err)
		irc.messagesCh <- Message{
			Type: "error",
			Text: "IRC connection lost: " + err.Error(),
		}
	} else {
		log.Printf("IRC Manager: Connection closed")
		irc.messagesCh <- Message{
			Type: "error",
			Text: "IRC connection closed",
		}
	}
}

func (irc *IRCManager) parseMessage(line string) {
	if strings.HasPrefix(line, "PING") {
		pong := strings.Replace(line, "PING", "PONG", 1)
		log.Printf("IRC Manager -> %s", pong)
		fmt.Fprintf(irc.conn, "%s\r\n", pong)
		return
	}

	parts := strings.Split(line, " ")
	log.Printf("IRC Manager: Parsing message with %d parts: %v", len(parts), parts)
	if len(parts) < 3 {
		log.Printf("IRC Manager: Not enough parts in message, skipping")
		return
	}

	log.Printf("IRC Manager: Message type: %s", parts[1])
	switch parts[1] {
	case "PRIVMSG":
		if len(parts) >= 4 {
			sender := strings.Split(parts[0], "!")[0][1:]
			channel := parts[2]
			message := strings.Join(parts[3:], " ")[1:]

			log.Printf("IRC Manager: Message from %s in %s: %s", sender, channel, message)

			irc.messagesCh <- Message{
				Type:    "message",
				Channel: channel,
				User:    sender,
				Text:    message,
			}
		}
	case "JOIN":
		if len(parts) >= 3 {
			sender := strings.Split(parts[0], "!")[0][1:]
			channel := parts[2]

			log.Printf("IRC Manager: User %s joined %s", sender, channel)

			irc.messagesCh <- Message{
				Type:    "join",
				Channel: channel,
				User:    sender,
			}
		}
	case "353": // Names list
		log.Printf("IRC Manager: Got 353 NAMES response with %d parts", len(parts))
		if len(parts) >= 5 {
			channel := parts[4]
			users := strings.Join(parts[5:], " ")[1:]

			log.Printf("IRC Manager: Users in %s: %s", channel, users)

			irc.messagesCh <- Message{
				Type:    "users",
				Channel: channel,
				Text:    users,
			}
		} else {
			log.Printf("IRC Manager: 353 message too short: %v", parts)
		}
	case "001": // Welcome message - connection successful
		log.Printf("IRC Manager: Successfully registered with server")
		irc.messagesCh <- Message{
			Type: "system",
			Text: "Successfully registered with IRC server",
		}
	default:
		log.Printf("IRC Manager: Unhandled message type: %s", parts[1])
	}
}
