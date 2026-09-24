package echo

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/protocol"
)

// Serve keeps one identity per accepted connection, never per client-provided session.
func Serve(ctx context.Context, ln *quic.Listener, log *slog.Logger) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() { defer wg.Done(); handle(ctx, conn, log) }()
	}
}

func handle(ctx context.Context, conn *quic.Conn, log *slog.Logger) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		conn.CloseWithError(1, "random failed")
		return
	}
	connectionID := hex.EncodeToString(id[:])
	log = log.With("connection_id", connectionID)
	log.Info("connection_accepted", "peer", conn.RemoteAddr().String())
	stop := context.AfterFunc(ctx, func() { conn.CloseWithError(0, "server stopping") })
	defer stop()
	defer conn.CloseWithError(0, "echo finished")
	defer log.Info("connection_closed")
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		log.Info("accept_stream_failed", "error", err)
		return
	}
	log.Info("stream_opened", "stream_id", stream.StreamID())
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 1024), 4096)
	enc := json.NewEncoder(stream)
	peer := ""
	for {
		stream.SetReadDeadline(time.Now().Add(90 * time.Second))
		if !scanner.Scan() {
			break
		}
		var frame protocol.Frame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return
		}
		frame.ConnectionID = connectionID
		frame.StreamID = int64(stream.StreamID())
		frame.Peer = conn.RemoteAddr().String()
		if frame.Peer != peer {
			log.Info("peer_observed", "old_peer", peer, "peer", frame.Peer, "seq", frame.Seq)
			peer = frame.Peer
		}
		stream.SetWriteDeadline(time.Now().Add(90 * time.Second))
		if err := enc.Encode(frame); err != nil {
			log.Info("write_failed", "error", err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		log.Info("read_finished", "error", err)
	}
}
