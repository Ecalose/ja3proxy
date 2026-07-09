package main

import (
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	trafficSessionLimit = 200
	trafficEventLimit   = 100

	trafficStateActive = "active"
	trafficStateClosed = "closed"
	trafficStateFailed = "failed"
)

type trafficDirection int

const (
	trafficDirectionUpload trafficDirection = iota
	trafficDirectionDownload
)

type TrafficSessionInfo struct {
	Protocol   string
	Target     string
	ClientAddr string
	SNI        string
}

type TrafficSessionSnapshot struct {
	ID            uint64
	Protocol      string
	Target        string
	ClientAddr    string
	SNI           string
	State         string
	Error         string
	StartedAt     time.Time
	UpdatedAt     time.Time
	ClosedAt      time.Time
	UploadBytes   int64
	DownloadBytes int64
}

type TrafficEventSnapshot struct {
	Time      time.Time
	SessionID uint64
	Level     string
	Message   string
	Protocol  string
	Target    string
	Error     string
}

type TrafficSnapshot struct {
	CapturedAt         time.Time
	StartedAt          time.Time
	ActiveSessions     int
	TotalSessions      uint64
	TotalUploadBytes   int64
	TotalDownloadBytes int64
	Sessions           []TrafficSessionSnapshot
	Events             []TrafficEventSnapshot
}

type trafficSession struct {
	TrafficSessionSnapshot
}

type TrafficMonitor struct {
	mu                 sync.Mutex
	startedAt          time.Time
	nextID             uint64
	totalSessions      uint64
	totalUploadBytes   int64
	totalDownloadBytes int64
	sessions           map[uint64]*trafficSession
	order              []uint64
	events             []TrafficEventSnapshot
}

func NewTrafficMonitor() *TrafficMonitor {
	return &TrafficMonitor{
		startedAt: time.Now(),
		sessions:  make(map[uint64]*trafficSession),
	}
}

func (monitor *TrafficMonitor) StartSession(info TrafficSessionInfo) *TrafficSessionHandle {
	if monitor == nil {
		return nil
	}

	now := time.Now()
	monitor.mu.Lock()
	defer monitor.mu.Unlock()

	monitor.nextID++
	id := monitor.nextID
	monitor.totalSessions++
	monitor.sessions[id] = &trafficSession{
		TrafficSessionSnapshot: TrafficSessionSnapshot{
			ID:         id,
			Protocol:   info.Protocol,
			Target:     info.Target,
			ClientAddr: info.ClientAddr,
			SNI:        info.SNI,
			State:      trafficStateActive,
			StartedAt:  now,
			UpdatedAt:  now,
		},
	}
	monitor.order = append(monitor.order, id)
	monitor.addEventLocked(TrafficEventSnapshot{
		Time:      now,
		SessionID: id,
		Level:     "info",
		Message:   "session opened",
		Protocol:  info.Protocol,
		Target:    info.Target,
	})
	monitor.pruneLocked()

	return &TrafficSessionHandle{
		monitor: monitor,
		id:      id,
	}
}

func (monitor *TrafficMonitor) RecordEvent(level string, message string, info TrafficSessionInfo, err error) {
	if monitor == nil {
		return
	}

	event := TrafficEventSnapshot{
		Time:     time.Now(),
		Level:    level,
		Message:  message,
		Protocol: info.Protocol,
		Target:   info.Target,
	}
	if err != nil {
		event.Error = err.Error()
	}

	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	monitor.addEventLocked(event)
}

func (monitor *TrafficMonitor) Snapshot() TrafficSnapshot {
	if monitor == nil {
		now := time.Now()
		return TrafficSnapshot{
			CapturedAt: now,
			StartedAt:  now,
		}
	}

	monitor.mu.Lock()
	defer monitor.mu.Unlock()

	now := time.Now()
	snapshot := TrafficSnapshot{
		CapturedAt:         now,
		StartedAt:          monitor.startedAt,
		TotalSessions:      monitor.totalSessions,
		TotalUploadBytes:   monitor.totalUploadBytes,
		TotalDownloadBytes: monitor.totalDownloadBytes,
		Sessions:           make([]TrafficSessionSnapshot, 0, len(monitor.sessions)),
		Events:             append([]TrafficEventSnapshot(nil), monitor.events...),
	}
	for _, session := range monitor.sessions {
		sessionSnapshot := session.TrafficSessionSnapshot
		if sessionSnapshot.State == trafficStateActive {
			snapshot.ActiveSessions++
		}
		snapshot.Sessions = append(snapshot.Sessions, sessionSnapshot)
	}
	sort.Slice(snapshot.Sessions, func(i, j int) bool {
		left := snapshot.Sessions[i]
		right := snapshot.Sessions[j]
		if left.State == trafficStateActive && right.State != trafficStateActive {
			return true
		}
		if left.State != trafficStateActive && right.State == trafficStateActive {
			return false
		}
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.ID > right.ID
	})

	return snapshot
}

func (monitor *TrafficMonitor) ResetClosed() {
	if monitor == nil {
		return
	}

	monitor.mu.Lock()
	defer monitor.mu.Unlock()

	keptOrder := monitor.order[:0]
	for _, id := range monitor.order {
		session, ok := monitor.sessions[id]
		if !ok {
			continue
		}
		if session.State != trafficStateActive {
			delete(monitor.sessions, id)
			continue
		}
		keptOrder = append(keptOrder, id)
	}
	monitor.order = keptOrder
	monitor.events = nil
}

type TrafficSessionHandle struct {
	monitor *TrafficMonitor
	id      uint64
}

func (handle *TrafficSessionHandle) AddUpload(n int) {
	handle.addBytes(trafficDirectionUpload, n)
}

func (handle *TrafficSessionHandle) AddDownload(n int) {
	handle.addBytes(trafficDirectionDownload, n)
}

func (handle *TrafficSessionHandle) Finish() {
	handle.close(trafficStateClosed, nil)
}

func (handle *TrafficSessionHandle) Fail(err error) {
	handle.close(trafficStateFailed, err)
}

func (handle *TrafficSessionHandle) addBytes(direction trafficDirection, n int) {
	if handle == nil || handle.monitor == nil || n <= 0 {
		return
	}

	monitor := handle.monitor
	monitor.mu.Lock()
	defer monitor.mu.Unlock()

	session, ok := monitor.sessions[handle.id]
	if !ok {
		return
	}
	now := time.Now()
	session.UpdatedAt = now
	switch direction {
	case trafficDirectionUpload:
		session.UploadBytes += int64(n)
		monitor.totalUploadBytes += int64(n)
	case trafficDirectionDownload:
		session.DownloadBytes += int64(n)
		monitor.totalDownloadBytes += int64(n)
	}
}

func (handle *TrafficSessionHandle) close(state string, err error) {
	if handle == nil || handle.monitor == nil {
		return
	}

	now := time.Now()
	monitor := handle.monitor
	monitor.mu.Lock()
	defer monitor.mu.Unlock()

	session, ok := monitor.sessions[handle.id]
	if !ok || session.State != trafficStateActive {
		return
	}
	session.State = state
	session.UpdatedAt = now
	session.ClosedAt = now
	if err != nil {
		session.Error = err.Error()
	}

	event := TrafficEventSnapshot{
		Time:      now,
		SessionID: handle.id,
		Level:     "info",
		Message:   "session closed",
		Protocol:  session.Protocol,
		Target:    session.Target,
	}
	if state == trafficStateFailed {
		event.Level = "warn"
		event.Message = "session failed"
		event.Error = session.Error
	}
	monitor.addEventLocked(event)
	monitor.pruneLocked()
}

func (monitor *TrafficMonitor) addEventLocked(event TrafficEventSnapshot) {
	monitor.events = append(monitor.events, event)
	if len(monitor.events) > trafficEventLimit {
		copy(monitor.events, monitor.events[len(monitor.events)-trafficEventLimit:])
		monitor.events = monitor.events[:trafficEventLimit]
	}
}

func (monitor *TrafficMonitor) pruneLocked() {
	for len(monitor.order) > trafficSessionLimit {
		pruned := false
		for i, id := range monitor.order {
			session, ok := monitor.sessions[id]
			if !ok || session.State != trafficStateActive {
				delete(monitor.sessions, id)
				monitor.order = append(monitor.order[:i], monitor.order[i+1:]...)
				pruned = true
				break
			}
		}
		if !pruned {
			return
		}
	}
}

type trafficReadConn struct {
	net.Conn
	session   *TrafficSessionHandle
	direction trafficDirection
}

func (conn *trafficReadConn) Read(p []byte) (int, error) {
	n, err := conn.Conn.Read(p)
	switch conn.direction {
	case trafficDirectionUpload:
		conn.session.AddUpload(n)
	case trafficDirectionDownload:
		conn.session.AddDownload(n)
	}
	return n, err
}

func wrapTrafficTunnel(session *TrafficSessionHandle, destConn net.Conn, clientConn net.Conn) (net.Conn, net.Conn) {
	if session == nil {
		return destConn, clientConn
	}
	return &trafficReadConn{
			Conn:      destConn,
			session:   session,
			direction: trafficDirectionDownload,
		}, &trafficReadConn{
			Conn:      clientConn,
			session:   session,
			direction: trafficDirectionUpload,
		}
}

func connRemoteAddr(conn net.Conn) string {
	if conn == nil || conn.RemoteAddr() == nil {
		return ""
	}
	return conn.RemoteAddr().String()
}

type trafficReadCloser struct {
	io.ReadCloser
	session *TrafficSessionHandle
}

func (reader *trafficReadCloser) Read(p []byte) (int, error) {
	n, err := reader.ReadCloser.Read(p)
	reader.session.AddUpload(n)
	return n, err
}

type trafficResponseWriter struct {
	http.ResponseWriter
	session *TrafficSessionHandle
}

func (writer *trafficResponseWriter) Write(p []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(p)
	writer.session.AddDownload(n)
	return n, err
}
