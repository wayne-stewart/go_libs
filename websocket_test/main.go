package main

import (
	"fmt"
	"net/http"

	"github.com/wayne-stewart/go_libs/websocket"
)

func main() {
	fmt.Println("Starting Test Server")

	chats := []*websocket.WebSocket{}

	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", 302)
	})

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/index.html")
	})

	http.HandleFunc("GET /chat", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/chat.html")
	})

	http.HandleFunc("GET /chat-ws", func(w http.ResponseWriter, r *http.Request) {

		// PrintRequestHeaders(r)

		ws, err := websocket.Upgrade(w, r)
		if err != nil {
			fmt.Println(err.Error())
			http.Error(w, "Could not upgrade to WebSocket", http.StatusInternalServerError)
			return
		}

		for _, other_ws := range chats {
			other_ws.SendText("A new user has entered the chat!")
		}

		ws.ReceiveTextHandler = (func(ws *websocket.WebSocket, message string) {
			for _, other_ws := range chats {
				other_ws.SendText(message)
			}
		})

		ws.ClosedHandler = (func(ws *websocket.WebSocket) {
			for _, other_ws := range chats {
				other_ws.SendText("A user has left the chat.")
			}
		})

		chats = append(chats, ws)

		ws.SendText("Welcome to the Chat!")
	})

	http.ListenAndServe(":8080", nil)
}
