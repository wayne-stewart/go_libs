package main

import (
	"fmt"
	"net/http"

	"github.com/wayne-stewart/go_libs/websocket"
)

func main() {
	fmt.Println("Starting Test Server")

	websockets := []*websocket.WebSocket{}

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/index.html")
	})
	http.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {

		// fmt.Println("WebSocket endpoint hit")
		// PrintRequestHeaders(r)

		ws, err := websocket.Upgrade(w, r)
		if err != nil {
			fmt.Println(err.Error())
			http.Error(w, "Could not upgrade to WebSocket", http.StatusInternalServerError)
			return
		}

		ws.ReceiveTextHandler = (func(ws *websocket.WebSocket, message string) {
			for _, other_ws := range websockets {
				other_ws.SendText(message)
			}
			//fmt.Println("Received message:", string(message))
		})

		ws.ClosedHandler = (func(ws *websocket.WebSocket) {
			fmt.Printf("WebSocket %d closed\n", ws.ID)
		})

		websockets = append(websockets, ws)

		ws.SendText("Welcome to the Chat!")
	})

	http.ListenAndServe(":8080", nil)
}
