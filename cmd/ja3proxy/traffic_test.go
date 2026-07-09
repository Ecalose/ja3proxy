package main

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestTrafficMonitorTracksSessionBytesAndEvents(t *testing.T) {
	monitor := NewTrafficMonitor()
	session := monitor.StartSession(TrafficSessionInfo{
		Protocol:   "HTTP CONNECT",
		Target:     "example.com:443",
		ClientAddr: "127.0.0.1:50000",
		SNI:        "example.com",
	})

	session.AddUpload(10)
	session.AddDownload(20)

	snapshot := monitor.Snapshot()
	if snapshot.ActiveSessions != 1 {
		t.Fatalf("active sessions = %d, want 1", snapshot.ActiveSessions)
	}
	if snapshot.TotalSessions != 1 {
		t.Fatalf("total sessions = %d, want 1", snapshot.TotalSessions)
	}
	if snapshot.TotalUploadBytes != 10 {
		t.Fatalf("total upload bytes = %d, want 10", snapshot.TotalUploadBytes)
	}
	if snapshot.TotalDownloadBytes != 20 {
		t.Fatalf("total download bytes = %d, want 20", snapshot.TotalDownloadBytes)
	}
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snapshot.Sessions))
	}
	got := snapshot.Sessions[0]
	if got.Protocol != "HTTP CONNECT" {
		t.Fatalf("protocol = %q, want HTTP CONNECT", got.Protocol)
	}
	if got.Target != "example.com:443" {
		t.Fatalf("target = %q, want example.com:443", got.Target)
	}
	if got.UploadBytes != 10 {
		t.Fatalf("upload bytes = %d, want 10", got.UploadBytes)
	}
	if got.DownloadBytes != 20 {
		t.Fatalf("download bytes = %d, want 20", got.DownloadBytes)
	}
	if got.State != trafficStateActive {
		t.Fatalf("state = %q, want active", got.State)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Message != "session opened" {
		t.Fatalf("events = %#v, want session opened event", snapshot.Events)
	}

	session.Finish()
	snapshot = monitor.Snapshot()
	if snapshot.ActiveSessions != 0 {
		t.Fatalf("active sessions after finish = %d, want 0", snapshot.ActiveSessions)
	}
	if snapshot.Sessions[0].State != trafficStateClosed {
		t.Fatalf("state after finish = %q, want closed", snapshot.Sessions[0].State)
	}
	if snapshot.Sessions[0].ClosedAt.IsZero() {
		t.Fatal("closed at is zero")
	}
	if len(snapshot.Events) != 2 || snapshot.Events[1].Message != "session closed" {
		t.Fatalf("events = %#v, want session closed event", snapshot.Events)
	}
}

func TestTrafficMonitorMarksFailedSession(t *testing.T) {
	monitor := NewTrafficMonitor()
	session := monitor.StartSession(TrafficSessionInfo{
		Protocol: "SOCKS5",
		Target:   "example.com:80",
	})

	session.Fail(errors.New("copy failed"))

	snapshot := monitor.Snapshot()
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snapshot.Sessions))
	}
	if snapshot.Sessions[0].State != trafficStateFailed {
		t.Fatalf("state = %q, want failed", snapshot.Sessions[0].State)
	}
	if snapshot.Sessions[0].Error != "copy failed" {
		t.Fatalf("error = %q, want copy failed", snapshot.Sessions[0].Error)
	}
	if len(snapshot.Events) != 2 || snapshot.Events[1].Level != "warn" {
		t.Fatalf("events = %#v, want warning close event", snapshot.Events)
	}
}

func TestTrafficMonitorResetClosedKeepsActiveSessions(t *testing.T) {
	monitor := NewTrafficMonitor()
	closed := monitor.StartSession(TrafficSessionInfo{Protocol: "HTTP", Target: "closed.test"})
	active := monitor.StartSession(TrafficSessionInfo{Protocol: "HTTP", Target: "active.test"})
	closed.Finish()

	monitor.ResetClosed()

	snapshot := monitor.Snapshot()
	if snapshot.ActiveSessions != 1 {
		t.Fatalf("active sessions = %d, want 1", snapshot.ActiveSessions)
	}
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snapshot.Sessions))
	}
	if snapshot.Sessions[0].Target != "active.test" {
		t.Fatalf("target = %q, want active.test", snapshot.Sessions[0].Target)
	}
	active.Finish()
}

func TestWrapTrafficTunnelCountsReadsOnce(t *testing.T) {
	monitor := NewTrafficMonitor()
	session := monitor.StartSession(TrafficSessionInfo{Protocol: "SOCKS5", Target: "example.com:80"})

	destConn, upstreamPeer := net.Pipe()
	clientConn, clientPeer := net.Pipe()
	for _, conn := range []net.Conn{destConn, upstreamPeer, clientConn, clientPeer} {
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
	}

	wrappedDest, wrappedClient := wrapTrafficTunnel(session, destConn, clientConn)
	done := make(chan struct{})
	go func() {
		pipeConns(wrappedDest, wrappedClient, wrappedDest, wrappedClient)
		close(done)
	}()

	payload := []byte("hello")
	writeDone := make(chan error, 1)
	go func() {
		_, err := clientPeer.Write(payload)
		writeDone <- err
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(upstreamPeer, got); err != nil {
		t.Fatalf("read upstream payload: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write client payload: %v", err)
	}

	response := []byte("world")
	writeDone = make(chan error, 1)
	go func() {
		_, err := upstreamPeer.Write(response)
		writeDone <- err
	}()
	got = make([]byte, len(response))
	if _, err := io.ReadFull(clientPeer, got); err != nil {
		t.Fatalf("read client payload: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write upstream payload: %v", err)
	}

	clientPeer.Close()
	upstreamPeer.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipe did not return")
	}

	snapshot := monitor.Snapshot()
	if snapshot.TotalUploadBytes != int64(len(payload)) {
		t.Fatalf("upload bytes = %d, want %d", snapshot.TotalUploadBytes, len(payload))
	}
	if snapshot.TotalDownloadBytes != int64(len(response)) {
		t.Fatalf("download bytes = %d, want %d", snapshot.TotalDownloadBytes, len(response))
	}
}
