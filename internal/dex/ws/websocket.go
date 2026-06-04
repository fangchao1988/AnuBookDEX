package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// WebSocket GUID for handshake
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// Frame opcodes
	opText   = 1
	opClose  = 8
	opPing   = 9
	opPong   = 10

	// Max frame size
	maxFrameSize = 65536
)

var (
	errCloseSent = errors.New("close sent")
)

// WSConn is a minimal WebSocket connection (server-side only)
type WSConn struct {
	conn   net.Conn
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex // protects writer
}

// upgradeHTTP upgrades an HTTP connection to WebSocket
func upgradeHTTP(w http.ResponseWriter, r *http.Request) (*WSConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, fmt.Errorf("missing Upgrade: websocket header")
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("missing Sec-WebSocket-Key header")
	}

	// Compute accept key: base64(sha1(key + GUID))
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Hijack the connection
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("server does not support hijacking")
	}
	netConn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	// Write upgrade response
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"

	if _, err := bufrw.WriteString(resp); err != nil {
		netConn.Close()
		return nil, err
	}
	if err := bufrw.Flush(); err != nil {
		netConn.Close()
		return nil, err
	}

	return &WSConn{
		conn:   netConn,
		reader: bufrw.Reader,
		writer: netConn,
	}, nil
}

// ReadMessage reads a complete text message (handles fragmented frames)
func (c *WSConn) ReadMessage() ([]byte, error) {
	var payload []byte
	for {
		opcode, data, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opText:
			payload = append(payload, data...)
			return payload, nil // final frame, return
		case opClose:
			// Echo close frame back
			c.writeFrame(opClose, data)
			return nil, io.EOF
		case opPing:
			c.writeFrame(opPong, data)
			continue
		case opPong:
			continue
		default:
			continue
		}
	}
}

// WriteMessage writes a text message
func (c *WSConn) WriteMessage(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeFrame(opText, data)
}

// WritePing sends a ping frame
func (c *WSConn) WritePing() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeFrame(opPing, nil)
}

// Close sends a close frame and closes the connection
func (c *WSConn) Close() error {
	c.mu.Lock()
	c.writeFrame(opClose, nil)
	c.mu.Unlock()
	return c.conn.Close()
}

// SetReadDeadline sets the read deadline
func (c *WSConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline
func (c *WSConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// readFrame reads a single WebSocket frame
func (c *WSConn) readFrame() (opcode byte, payload []byte, err error) {
	// Read first 2 bytes
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.reader, header); err != nil {
		return 0, nil, err
	}

	opcode = header[0] & 0x0F
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7F)

	// Extended payload length
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(c.reader, ext); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(c.reader, ext); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}

	if length > maxFrameSize {
		return 0, nil, fmt.Errorf("frame too large: %d", length)
	}

	// Read mask key (if masked)
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}

	// Read payload
	payload = make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			return 0, nil, err
		}
	}

	// Unmask (client->server frames are always masked per RFC 6455)
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return opcode, payload, nil
}

// writeFrame writes a single WebSocket frame (server->client, never masked)
func (c *WSConn) writeFrame(opcode byte, payload []byte) error {
	// Frame header
	frame := []byte{0x80 | opcode} // FIN + opcode

	length := len(payload)
	switch {
	case length <= 125:
		frame = append(frame, byte(length))
	case length <= 65535:
		frame = append(frame, 126)
		frame = append(frame, byte(length>>8), byte(length))
	default:
		frame = append(frame, 127)
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(length))
		frame = append(frame, b...)
	}

	frame = append(frame, payload...)

	_, err := c.writer.Write(frame)
	return err
}
