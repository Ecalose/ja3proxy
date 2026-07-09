package ja3proxy

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
)

func junction(destConn net.Conn, clientConn net.Conn) {
	pipeConns(destConn, clientConn, destConn, clientConn)
}

func pipeConns(destConn net.Conn, clientConn net.Conn, destWriter io.Writer, clientWriter io.Writer) {
	chDone := make(chan struct{}, 2)

	go func() {
		copyAndClose(destWriter, clientConn, destConn, "client_to_dest")
		chDone <- struct{}{}
	}()

	go func() {
		copyAndClose(clientWriter, destConn, clientConn, "dest_to_client")
		chDone <- struct{}{}
	}()

	// wait for both copy ops to complete
	<-chDone
	<-chDone
}

func copyAndClose(dst io.Writer, src io.Reader, closeConn io.Closer, direction string) {
	defer closeConn.Close()

	if _, err := io.Copy(dst, src); err != nil {
		if isExpectedCopyError(err) {
			slog.Debug("proxy pipe closed", "component", "pipe", "direction", direction, "err", err)
			return
		}
		slog.Warn("proxy pipe failed", "component", "pipe", "direction", direction, "err", err)
	}
}

func isExpectedCopyError(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, net.ErrClosed) ||
		strings.Contains(err.Error(), "use of closed network connection")
}
