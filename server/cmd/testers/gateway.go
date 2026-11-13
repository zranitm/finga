package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/protocol"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

// local reports whether a gateway URL is on this machine: a loopback address, or a single-label name, as a Compose service on the stack's own network is.
func local(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost" || (host != "" && !strings.Contains(host, "."))
}

// gateway is the player's way in: REST for accounts and characters, the WebSocket for play.
type gateway struct {
	url  string
	http *http.Client
}

// statusError is a REST call the gateway answered with an error status.
type statusError struct {
	status int
	body   string
}

func (e statusError) Error() string { return fmt.Sprintf("status %d: %s", e.status, e.body) }

func (g gateway) call(ctx context.Context, method, path, token string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, g.url+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= http.StatusMultipleChoices {
		return statusError{status: res.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// signUp makes the account, or finds it taken: true when this call made it.
func (g gateway) signUp(ctx context.Context, username, password string) (bool, error) {
	err := g.call(ctx, http.MethodPost, "/api/accounts", "", map[string]string{"username": username, "password": password}, nil)
	var se statusError
	if errors.As(err, &se) && se.status == http.StatusConflict {
		return false, nil
	}
	return err == nil, err
}

// login is a session token, waiting out the login throttle a few times.
func (g gateway) login(ctx context.Context, username, password string) (string, error) {
	for wait := 5 * time.Second; ; wait *= 2 {
		var out struct {
			Token string `json:"token"`
		}
		err := g.call(ctx, http.MethodPost, "/api/sessions", "", map[string]string{"username": username, "password": password}, &out)
		var se statusError
		if !errors.As(err, &se) || se.status != http.StatusTooManyRequests || wait > time.Minute {
			return out.Token, err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
	}
}

// character is the account's character of that name, made if it has none: its ID.
func (g gateway) character(ctx context.Context, token, name string) (string, error) {
	var list struct {
		Characters []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"characters"`
	}
	if err := g.call(ctx, http.MethodGet, "/api/characters", token, nil, &list); err != nil {
		return "", err
	}
	for _, c := range list.Characters {
		if c.Name == name {
			return c.ID, nil
		}
	}
	var made struct {
		ID string `json:"id"`
	}
	err := g.call(ctx, http.MethodPost, "/api/characters", token, map[string]string{"name": name}, &made)
	return made.ID, err
}

// session is a character in the world, sending commands one at a time and waiting for each one's answer.
type session struct {
	conn   *websocket.Conn
	codec  *wsconn.ProtoCodec
	msgs   chan wsconn.Message
	closed chan struct{}
	seq    uint32
}

// join enters the world with the character, as a client does, and returns once the Welcome came.
func (g gateway) join(ctx context.Context, token, characterID string) (*session, *clientv1.Welcome, error) {
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(g.url, "http")+"/ws", &websocket.DialOptions{Subprotocols: []string{protocol.SubprotocolProto}})
	if err != nil {
		return nil, nil, err
	}
	conn.SetReadLimit(-1) // a Welcome carries the whole inventory
	s := &session{conn: conn, codec: protocol.ClientCodec(wsconn.Binary), msgs: make(chan wsconn.Message, 1024), closed: make(chan struct{})}
	go s.read()
	if err := s.send(ctx, "", &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: characterID}); err != nil {
		s.close()
		return nil, nil, err
	}
	m, err := s.await(ctx, func(m wsconn.Message) bool { _, ok := m.Data.(*clientv1.Welcome); return ok })
	if err != nil {
		s.close()
		return nil, nil, fmt.Errorf("no Welcome: %w", err)
	}
	return s, m.Data.(*clientv1.Welcome), nil //nolint:forcetypeassert // await matched it
}

func (s *session) read() {
	defer close(s.closed)
	for {
		_, data, err := s.conn.Read(context.Background())
		if err != nil {
			return
		}
		if m, err := s.codec.Decode(data); err == nil {
			select {
			case s.msgs <- m:
			default: // a burst nobody waits for
			}
		}
	}
}

func (s *session) send(ctx context.Context, reqID string, d proto.Message) error {
	s.seq++
	data, err := s.codec.Encode(wsconn.Message{Seq: s.seq, ReqID: reqID, Data: d})
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageBinary, data)
}

// await is the first message that matches, within ten seconds.
func (s *session) await(ctx context.Context, match func(wsconn.Message) bool) (wsconn.Message, error) {
	timeout := time.After(10 * time.Second)
	for {
		select {
		case m := <-s.msgs:
			if match(m) {
				return m, nil
			}
		case <-s.closed:
			return wsconn.Message{}, errors.New("the connection closed")
		case <-timeout:
			return wsconn.Message{}, errors.New("no answer in 10 s")
		case <-ctx.Done():
			return wsconn.Message{}, ctx.Err()
		}
	}
}

// rateLimited is the gateway's RATE_LIMITED, as testdata/errors/codes.json lists it: a command over its group's rate, refused before it went anywhere.
const rateLimited = 100007

// do sends a command and waits for its Response, sending it again after a pause while the gateway says to slow down, as a client would: an error when it is refused, naming the code.
func (s *session) do(ctx context.Context, d proto.Message) error {
	for wait := 250 * time.Millisecond; ; wait *= 2 {
		code, err := s.try(ctx, d)
		switch {
		case err != nil:
			return err
		case code == 0:
			return nil
		case code != rateLimited || wait > 4*time.Second:
			return fmt.Errorf("%T refused with code %d", d, code)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// try sends a command once: its Response's code, 0 when it went through.
func (s *session) try(ctx context.Context, d proto.Message) (int32, error) {
	reqID := uuidv7.NewString()
	if err := s.send(ctx, reqID, d); err != nil {
		return 0, err
	}
	m, err := s.await(ctx, func(m wsconn.Message) bool { _, ok := m.Data.(*clientv1.Response); return ok && m.ReqID == reqID })
	if err != nil {
		return 0, err
	}
	return m.Data.(*clientv1.Response).GetCode(), nil //nolint:forcetypeassert // await matched it
}

func (s *session) close() { _ = s.conn.Close(websocket.StatusNormalClosure, "seeded") }
