package main

import (
	"encoding/json"
	"syscall/js"
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

type IRCClient struct {
	ws       js.Value
	messages []Message
}

var client *IRCClient

func main() {
	c := make(chan struct{}, 0)

	client = &IRCClient{
		messages: make([]Message, 0),
	}

	js.Global().Set("connectToIRC", js.FuncOf(connectToIRC))
	js.Global().Set("joinChannel", js.FuncOf(joinChannel))
	js.Global().Set("sendMessage", js.FuncOf(sendMessage))

	<-c
}

func connectToIRC(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return nil
	}

	server := args[0].String()
	nick := args[1].String()
	port := args[2].String()

	ws := js.Global().Get("WebSocket").New("ws://localhost:8080/ws")
	client.ws = ws

	// Store WebSocket globally for UI access
	js.Global().Set("ircWebSocket", ws)

	ws.Set("onopen", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := Message{
			Type:   "connect",
			Server: server,
			Port:   port,
			Nick:   nick,
		}
		sendWebSocketMessage(msg)
		return nil
	}))

	ws.Set("onmessage", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		data := args[0].Get("data").String()
		var msg Message
		json.Unmarshal([]byte(data), &msg)

		handleMessage(msg)
		return nil
	}))

	ws.Set("onerror", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		appendToChat("system", "WebSocket error occurred")
		return nil
	}))

	return nil
}

func joinChannel(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return nil
	}

	channel := args[0].String()
	msg := Message{
		Type:    "join",
		Channel: channel,
	}
	sendWebSocketMessage(msg)
	return nil
}

func sendMessage(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return nil
	}

	channel := args[0].String()
	text := args[1].String()

	msg := Message{
		Type:    "message",
		Channel: channel,
		Text:    text,
	}
	sendWebSocketMessage(msg)
	return nil
}

func sendWebSocketMessage(msg Message) {
	if client.ws.IsUndefined() {
		return
	}

	data, _ := json.Marshal(msg)
	client.ws.Call("send", string(data))
}

func handleMessage(msg Message) {
	client.messages = append(client.messages, msg)

	switch msg.Type {
	case "connected":
		appendToChat("system", "Connected to IRC server")
	case "reconnected":
		appendToChat("system", "Reconnected to existing IRC session")
	case "state":
		appendToChat("system", "Restored IRC connection: "+msg.Text)
	case "channel_joined":
		appendToChat("system", "Rejoined channel: "+msg.Channel)
	case "message":
		appendToChat(msg.Channel, msg.User+": "+msg.Text)
	case "join":
		appendToChat(msg.Channel, msg.User+" joined the channel")
	case "users":
		appendToChat(msg.Channel, "Users: "+msg.Text)
	case "error":
		appendToChat("system", "Error: "+msg.Text)
	case "disconnected":
		appendToChat("system", "Disconnected from IRC")
	}
}

func appendToChat(channel, text string) {
	doc := js.Global().Get("document")
	chatArea := doc.Call("getElementById", "chat-area")

	div := doc.Call("createElement", "div")

	// Different styling based on message type
	var className string
	if channel == "system" {
		className = "mb-2 p-3 bg-slate-800 border border-slate-600 rounded-md"
	} else {
		className = "mb-2 p-3 bg-slate-700 border border-slate-500 rounded-md hover:bg-slate-600 transition-colors"
	}
	div.Set("className", className)

	channelSpan := doc.Call("createElement", "span")
	if channel == "system" {
		channelSpan.Set("className", "font-semibold text-amber-400")
	} else {
		channelSpan.Set("className", "font-semibold text-blue-400")
	}
	channelSpan.Set("textContent", "["+channel+"] ")

	textSpan := doc.Call("createElement", "span")
	if channel == "system" {
		textSpan.Set("className", "text-gray-200")
	} else {
		textSpan.Set("className", "text-gray-100")
	}
	textSpan.Set("textContent", text)

	div.Call("appendChild", channelSpan)
	div.Call("appendChild", textSpan)
	chatArea.Call("appendChild", div)

	chatArea.Set("scrollTop", chatArea.Get("scrollHeight"))
}
