package main

import (
	"encoding/hex"
	"log/slog"
	"net"
	"unicode"
)

type DebugWriter struct {
	Name string
	Conn net.Conn
}

func (writer DebugWriter) Write(data []byte) (n int, err error) {
	if len(data) == 0 {
		slog.Debug("proxy data", "component", "debug", "peer", writer.Name, "bytes", 0)
		return writer.Conn.Write(data)
	}

	if unicode.IsPrint(rune(data[0])) {
		slog.Debug("proxy data", "component", "debug", "peer", writer.Name, "bytes", len(data), "encoding", "text", "data", string(data))
	} else {
		slog.Debug("proxy data", "component", "debug", "peer", writer.Name, "bytes", len(data), "encoding", "hex", "data", hex.Dump(data))
	}

	return writer.Conn.Write(data)
}

func debugJunction(destConn net.Conn, clientConn net.Conn) {
	destWriter := &DebugWriter{
		Name: clientConn.RemoteAddr().String(),
		Conn: destConn,
	}
	clientWriter := &DebugWriter{
		Name: destConn.RemoteAddr().String(),
		Conn: clientConn,
	}

	pipeConns(destConn, clientConn, destWriter, clientWriter)
}
