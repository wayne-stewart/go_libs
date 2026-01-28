/*
WebSocket implementation for Go
Author: Wayne Stewart
License: MIT

https://datatracker.ietf.org/doc/html/rfc6455
*/

package websocket

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

/*  TODO
Implement Ping Keepalive to remove dead connections
Support Origin validation
Support permessage-deflate extension
Support fragmented frames?
*/

var global_id_gen atomic.Int64 = atomic.Int64{}
var global_websocket_count atomic.Int32 = atomic.Int32{}
var global_websockets sync.Map = sync.Map{}
var global_frame_max_size int64 = 1024 * 1024 // 1 MB
var global_max_websockets int32 = 1000

const ERROR_CODE_NORMAL_CLOSURE = uint16(1000)

const OPCODE_CONTINUATION = byte(0x0)
const OPCODE_TEXT = byte(0x1)
const OPCODE_BINARY = byte(0x2)
const OPCODE_CLOSE = byte(0x8)
const OPCODE_PING = byte(0x9)
const OPCODE_PONG = byte(0xA)

type WebSocketReceiveBinaryHandler func(ws *WebSocket, message []byte)
type WebSocketReceiveTextHandler func(ws *WebSocket, message string)
type WebSocketClosedHandler func(ws *WebSocket)

type WebSocket struct {
	ID                   int64
	rw                   *bufio.ReadWriter
	conn                 net.Conn
	protocol             string
	extensions           string
	client_key           string
	is_open              bool
	ReceiveBinaryHandler WebSocketReceiveBinaryHandler
	ReceiveTextHandler   WebSocketReceiveTextHandler
	ClosedHandler        WebSocketClosedHandler
}

func (ws *WebSocket) SendBinary(message []byte) error {
	return sendFrame(ws, OPCODE_BINARY, nil, message)
}

func (ws *WebSocket) SendText(message string) error {
	return sendFrame(ws, OPCODE_TEXT, nil, []byte(message))
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*WebSocket, error) {

	ws, err := makeWebSocket(w, r)
	if err != nil {
		return nil, err
	}

	err = sendWebSocketHandshakeResponse(ws)
	if err != nil {
		return nil, err
	}

	go readLoop(ws)

	return ws, nil
}

func validateProtocolHeader(value string) (string, error) {
	if len(value) > 0 {
		return "", fmt.Errorf("Unsupported WebSocket protocol: %s", value)
	}
	return "", nil
}

func validateExtensionsHeader(value string) (string, error) {
	// if len(value) > 0 {
	// 	return "", fmt.Errorf("Unsupported WebSocket protocol: %s", value)
	// }
	return "", nil // ignore extensions for now
}

func makeStatusCodeBytes(code uint16) []byte {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, code)
	return buf
}

func closeWebSocket(ws *WebSocket, data []byte) {
	if ws.is_open {
		ws.is_open = false
		global_websockets.Delete(ws.ID)
		sendFrame(ws, OPCODE_CLOSE, nil, data)
		ws.conn.Close()
		global_websocket_count.Add(-1)
		if ws.ClosedHandler != nil {
			ws.ClosedHandler(ws)
		}
	}
}

func makeWebSocket(w http.ResponseWriter, r *http.Request) (*WebSocket, error) {
	if r.Method != "GET" {
		return nil, fmt.Errorf("Invalid HTTP method: %s", r.Method)
	}
	if r.ProtoAtLeast(1, 1) == false {
		return nil, errors.New("HTTP version must be at least 1.1")
	}
	header := r.Header.Get("Upgrade")
	if header != "websocket" {
		return nil, fmt.Errorf("Invalid Upgrade header: %s", header)
	}
	header = r.Header.Get("Connection")
	if header != "Upgrade" {
		return nil, fmt.Errorf("Invalid Connection header: %s", header)
	}
	header = r.Header.Get("Sec-Fetch-Mode")
	if header != "websocket" {
		return nil, fmt.Errorf("Invalid Sec-Fetch-Mode header: %s", header)
	}
	header = r.Header.Get("Sec-WebSocket-Version")
	if !strings.Contains(header, "13") {
		return nil, fmt.Errorf("Unsupported WebSocket version: %s", header)
	}

	ws_protocol, err := validateProtocolHeader(r.Header.Get("Sec-WebSocket-Protocol"))
	if err != nil {
		return nil, err
	}

	ws_extensions, err := validateExtensionsHeader(r.Header.Get("Sec-WebSocket-Extensions"))
	if err != nil {
		return nil, err
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil, errors.New("Missing Origin header")
	}

	client_key := r.Header.Get("Sec-WebSocket-Key")
	if client_key == "" {
		return nil, errors.New("Missing Sec-WebSocket-Key header")
	}

	if !incrementGlobalWebsocketCount() {
		return nil, errors.New("Maximum number of WebSocket connections reached")
	}

	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return nil, err
	}

	ws := &WebSocket{
		ID:         global_id_gen.Add(1),
		rw:         rw,
		conn:       conn,
		protocol:   ws_protocol,
		extensions: ws_extensions,
		client_key: client_key,
		is_open:    true,
	}

	global_websockets.Store(ws.ID, ws)
	return ws, nil
}

func incrementGlobalWebsocketCount() bool {
	for {
		count := global_websocket_count.Load()
		if count >= global_max_websockets {
			return false
		}
		if global_websocket_count.CompareAndSwap(count, count+1) {
			return true
		}
	}
}

func maskData(mask_key []byte, data []byte) {
	for i := 0; i < len(data); i++ {
		data[i] ^= mask_key[i%4]
	}
}

func sendWebSocketHandshakeResponse(ws *WebSocket) error {
	acceptKey := ws.client_key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(acceptKey))
	acceptKeyHash := base64.StdEncoding.EncodeToString(h.Sum(nil))
	fmt.Fprintf(ws.rw, "HTTP/1.1 101 Switching Protocols\r\n")
	fmt.Fprintf(ws.rw, "Upgrade: websocket\r\n")
	fmt.Fprintf(ws.rw, "Connection: Upgrade\r\n")
	fmt.Fprintf(ws.rw, "Sec-WebSocket-Accept: %s\r\n", acceptKeyHash)
	if ws.protocol != "" {
		fmt.Fprintf(ws.rw, "Sec-WebSocket-Protocol: %s\r\n", ws.protocol)
	}
	if ws.extensions != "" {
		fmt.Fprintf(ws.rw, "Sec-WebSocket-Extensions: %s\r\n", ws.extensions)
	}
	fmt.Fprintf(ws.rw, "\r\n")
	return ws.rw.Flush()
}

func readLoop(ws *WebSocket) {
	for {
		err := readFrame(ws)
		if err != nil {
			closeWebSocket(ws, makeStatusCodeBytes(ERROR_CODE_NORMAL_CLOSURE))
			return
		}
	}
}

func readFrame(ws *WebSocket) error {
	b1, err := ws.rw.ReadByte()
	if err != nil {
		return err
	}
	if ws.is_open == false {
		return errors.New("WebSocket is closed")
	}
	fin := b1&0x80 == 0x80
	if !fin {
		return errors.New("Fragmented frames are not supported")
	}
	rsv1 := b1&0x40 == 0x40
	rsv2 := b1&0x20 == 0x20
	rsv3 := b1&0x10 == 0x10
	if rsv1 || rsv2 || rsv3 {
		return errors.New("Unsupported RSV bits set")
	}
	opcode := b1 & 0x0F
	is_continuation := opcode == OPCODE_CONTINUATION
	is_text := opcode == OPCODE_TEXT
	is_binary := opcode == OPCODE_BINARY
	is_close := opcode == OPCODE_CLOSE
	is_ping := opcode == OPCODE_PING
	is_pong := opcode == OPCODE_PONG
	if !(is_continuation || is_text || is_binary || is_close || is_ping || is_pong) {
		return fmt.Errorf("Unsupported opcode %d", opcode)
	}

	b2, err := ws.rw.ReadByte()
	if err != nil {
		return err
	}
	is_masked := b2&0x80 == 0x80
	payload_len := int64(b2 & 0x7F)
	mask_key := make([]byte, 4)

	if payload_len == 126 {
		buffer := make([]byte, 2)
		n, err := ws.rw.Read(buffer)
		if err != nil {
			return err
		}
		if n != 2 {
			return errors.New("Could not read extended payload length")
		}
		payload_len = int64(buffer[0])<<8 | int64(buffer[1])
	} else if payload_len == 127 {
		buffer := make([]byte, 8)
		n, err := ws.rw.Read(buffer)
		if err != nil {
			return err
		}
		if n != 8 {
			return errors.New("Could not read extended payload length")
		}
		payload_len = int64(buffer[0])<<56 | int64(buffer[1])<<48 | int64(buffer[2])<<40 | int64(buffer[3])<<32 |
			int64(buffer[4])<<24 | int64(buffer[5])<<16 | int64(buffer[6])<<8 | int64(buffer[7])
	}

	if payload_len > global_frame_max_size {
		return fmt.Errorf("Payload length %d exceeds maximum frame size %d", payload_len, global_frame_max_size)
	}

	if is_masked {
		n, err := ws.rw.Read(mask_key)
		if err != nil {
			return err
		}
		if n != 4 {
			return errors.New("Could not read masking key")
		}
	}

	data := make([]byte, payload_len)
	data_len := int64(0)
	for data_len < payload_len {
		n, err := ws.rw.Read(data[data_len:])
		if err != nil {
			return err
		}
		data_len += int64(n)
	}

	if is_masked {
		maskData(mask_key, data)
	}

	if is_close {
		closeWebSocket(ws, data)
	} else if is_ping {
		sendFrame(ws, OPCODE_PONG, nil, data)
	} else if is_pong {
		// Ignore pong frames for now
		// TODO: Implement ping keepalive
		// Pongs will be used to update last active timestamp
	} else if is_text {
		if ws.ReceiveTextHandler != nil {
			s := string(data)
			ws.ReceiveTextHandler(ws, s)
		}
	} else if is_binary {
		if ws.ReceiveBinaryHandler != nil {
			ws.ReceiveBinaryHandler(ws, data)
		}
	}

	return nil
}

func sendFrame(ws *WebSocket, opcode byte, mask_key []byte, payload []byte) error {
	payload_len := len(payload)
	fin := byte(0x80)
	header_length := 2
	if opcode > 0x0F {
		return fmt.Errorf("Invalid opcode: %d", opcode)
	}
	b1 := fin | opcode
	masked := byte(0x00)
	if len(mask_key) == 4 {
		masked = 0x80
		maskData(mask_key, payload)
		header_length += 4
	}
	b2 := masked
	if payload_len <= 125 {
		b2 |= byte(payload_len)
	} else if payload_len <= 65535 {
		b2 |= 126
		header_length += 2
	} else {
		b2 |= 127
		header_length += 8
	}

	header := make([]byte, header_length)
	header[0] = b1
	header[1] = b2
	if b2&0x7F == 126 {
		header[2] = byte((payload_len >> 8) & 0xFF)
		header[3] = byte(payload_len & 0xFF)
	} else if b2&0x7F == 127 {
		header[2] = byte((payload_len >> 56) & 0xFF)
		header[3] = byte((payload_len >> 48) & 0xFF)
		header[4] = byte((payload_len >> 40) & 0xFF)
		header[5] = byte((payload_len >> 32) & 0xFF)
		header[6] = byte((payload_len >> 24) & 0xFF)
		header[7] = byte((payload_len >> 16) & 0xFF)
		header[8] = byte((payload_len >> 8) & 0xFF)
		header[9] = byte(payload_len & 0xFF)
	}
	if len(mask_key) == 4 {
		copy(header[header_length-4:], mask_key)
	}

	_, err := ws.rw.Write(header)
	if err != nil {
		return err
	}
	_, err = ws.rw.Write(payload)
	if err != nil {
		return err
	}
	err = ws.rw.Flush()
	if err != nil {
		return err
	}

	return nil
}
