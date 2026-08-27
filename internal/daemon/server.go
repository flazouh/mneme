package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/flazouh/mneme/internal/archive"
	"github.com/flazouh/mneme/internal/engine"
	"github.com/flazouh/mneme/internal/errcat"
	"github.com/flazouh/mneme/internal/memory"
	"golang.org/x/sys/unix"
)

type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	ID       string          `json:"id"`
	OK       bool            `json:"ok"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Category string `json:"category"`
	Message  string `json:"message"`
}

type Server struct {
	Engine  *engine.Engine
	Socket  string
	Lock    string
	Logger  *slog.Logger
	Archive *archive.Worker

	mu     sync.Mutex
	ln     net.Listener
	lockF  *os.File
	cancel context.CancelFunc
}

func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.Socket), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Lock), 0o700); err != nil {
		return err
	}
	lockF, err := os.OpenFile(s.Lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := unix.Flock(int(lockF.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lockF.Close()
		return errcat.Wrap(errcat.Conflict, "another mneme daemon holds the lock", err)
	}
	s.lockF = lockF
	_ = os.Remove(s.Socket)
	ln, err := net.Listen("unix", s.Socket)
	if err != nil {
		_ = s.release()
		return err
	}
	if err := os.Chmod(s.Socket, 0o600); err != nil {
		_ = ln.Close()
		_ = s.release()
		return err
	}
	s.ln = ln
	ctx, s.cancel = context.WithCancel(ctx)
	s.log().Info("mneme listening", "socket", s.Socket)

	go s.archiveLoop(ctx)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return s.shutdown()
			}
			s.log().Error("accept", "err", err)
			continue
		}
		go s.handle(ctx, conn)
	}
}

func (s *Server) shutdown() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
	_ = os.Remove(s.Socket)
	return s.release()
}

func (s *Server) release() error {
	if s.lockF != nil {
		_ = unix.Flock(int(s.lockF.Fd()), unix.LOCK_UN)
		err := s.lockF.Close()
		s.lockF = nil
		return err
	}
	return nil
}

func (s *Server) archiveLoop(ctx context.Context) {
	if s.Archive == nil || s.Archive.Repo == "" {
		return
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = s.Archive.Tick(context.Background())
			return
		case <-t.C:
			if err := s.Archive.Tick(ctx); err != nil {
				s.log().Error("archive tick", "err", err)
			}
		}
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	w := bufio.NewWriter(conn)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytesTrim(line)) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = writeRes(w, Response{OK: false, Error: &RPCError{Category: string(errcat.InvalidInput), Message: err.Error()}})
			continue
		}
		res := s.dispatch(ctx, req)
		if err := writeRes(w, res); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(ctx context.Context, req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	var (
		payload any
		err     error
	)
	switch req.Method {
	case "ping":
		payload = map[string]string{"status": "ok"}
	case "add":
		var p engine.AddRequest
		if err = json.Unmarshal(req.Params, &p); err == nil {
			rec, gated, addErr := s.Engine.Add(ctx, p)
			err = addErr
			payload = map[string]any{"record": rec, "gate": gated}
		}
	case "pack":
		var p engine.PackRequest
		if err = json.Unmarshal(req.Params, &p); err == nil {
			payload, err = s.Engine.Pack(ctx, p)
		}
	case "get":
		var p struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(req.Params, &p); err == nil {
			payload, err = s.Engine.Get(ctx, p.ID)
		}
	case "lifecycle":
		var p struct {
			ID        string           `json:"id"`
			Lifecycle memory.Lifecycle `json:"lifecycle"`
		}
		if err = json.Unmarshal(req.Params, &p); err == nil {
			err = s.Engine.Lifecycle(ctx, p.ID, p.Lifecycle)
			payload = map[string]string{"id": p.ID, "lifecycle": string(p.Lifecycle)}
		}
	case "usefulness":
		var p struct {
			ID     string `json:"id"`
			Useful bool   `json:"useful"`
		}
		if err = json.Unmarshal(req.Params, &p); err == nil {
			err = s.Engine.Usefulness(ctx, p.ID, p.Useful)
			payload = map[string]any{"id": p.ID, "useful": p.Useful}
		}
	case "health":
		payload = s.Engine.Health(ctx)
	default:
		err = errcat.New(errcat.InvalidInput, "unknown method "+req.Method)
	}
	if err != nil {
		return Response{
			ID: req.ID,
			OK: false,
			Error: &RPCError{
				Category: string(categoryOf(err)),
				Message:  err.Error(),
			},
		}
	}
	raw, _ := json.Marshal(payload)
	return Response{ID: req.ID, OK: true, Result: raw}
}

func categoryOf(err error) errcat.Category {
	if c := errcat.CategoryOf(err); c != "" {
		return c
	}
	return errcat.ServiceUnavailable
}

func writeRes(w *bufio.Writer, res Response) error {
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(raw, '\n')); err != nil {
		return err
	}
	return w.Flush()
}

func bytesTrim(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}

func (s *Server) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

type Client struct {
	Socket string
}

func (c Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return nil, errcat.Wrap(errcat.ServiceUnavailable, "mneme daemon is not running", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	}
	req := Request{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		req.Params = raw
	}
	enc, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(enc, '\n')); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !sc.Scan() {
		if sc.Err() != nil {
			return nil, sc.Err()
		}
		return nil, errors.New("empty daemon response")
	}
	var res Response
	if err := json.Unmarshal(sc.Bytes(), &res); err != nil {
		return nil, err
	}
	if !res.OK {
		msg := "daemon error"
		cat := errcat.ServiceUnavailable
		if res.Error != nil {
			msg = res.Error.Message
			if res.Error.Category != "" {
				cat = errcat.Category(res.Error.Category)
			}
		}
		return nil, errcat.New(cat, msg)
	}
	return res.Result, nil
}
