package wsconn_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"finga/pkg/wsconn"
)

type ping struct {
	ClientTime int64 `json:"client_time"`
}

type input struct {
	Left  bool `json:"left,omitempty"`
	Right bool `json:"right,omitempty"`
}

type empty struct{}

// blob is a large payload, for filling socket buffers.
type blob struct {
	Data string `json:"data"`
}

func newCodec() *wsconn.JSONCodec {
	c := wsconn.NewJSONCodec()
	c.Register("Ping", ping{})
	c.Register("Input", input{})
	c.Register("Attack", empty{})
	c.Register("Blob", blob{})
	return c
}

func canon(t *testing.T, b []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("not JSON: %s", b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func TestEncode(t *testing.T) {
	c := newCodec()
	tests := []struct {
		name string
		msg  wsconn.Message
		want string
	}{
		{"type and data", wsconn.Message{Type: "Ping", Data: ping{ClientTime: 5}}, `{"t":"Ping","d":{"client_time":5}}`},
		{"type inferred from the data's type", wsconn.Message{Data: input{Left: true}}, `{"t":"Input","d":{"left":true}}`},
		{"seq is written when set", wsconn.Message{Type: "Input", Seq: 12, Data: input{Right: true}}, `{"t":"Input","seq":12,"d":{"right":true}}`},
		{"req and tick are written when set", wsconn.Message{Type: "Ping", ReqID: "r-1", Tick: 81234, Data: ping{}}, `{"t":"Ping","req":"r-1","tick":81234,"d":{"client_time":0}}`},
		{"a message with no fields has an empty object", wsconn.Message{Type: "Attack", Data: empty{}}, `{"t":"Attack","d":{}}`},
		{"nil data is an empty object", wsconn.Message{Type: "Attack"}, `{"t":"Attack","d":{}}`},
		{"a pointer to a registered type works too", wsconn.Message{Data: &ping{ClientTime: 1}}, `{"t":"Ping","d":{"client_time":1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Encode(tt.msg)
			if err != nil {
				t.Fatal(err)
			}
			if canon(t, got) != canon(t, []byte(tt.want)) {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
	t.Run("an unregistered type cannot be encoded", func(t *testing.T) {
		if _, err := c.Encode(wsconn.Message{Data: struct{ X int }{1}}); !errors.Is(err, wsconn.ErrUnknownType) {
			t.Fatalf("err = %v", err)
		}
		if _, err := c.Encode(wsconn.Message{Type: "Nope", Data: ping{}}); !errors.Is(err, wsconn.ErrUnknownType) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDecode(t *testing.T) {
	c := newCodec()
	tests := []struct {
		name    string
		raw     string
		want    wsconn.Message
		wantErr error
	}{
		{"type and data", `{"t":"Ping","d":{"client_time":5}}`, wsconn.Message{Type: "Ping", Data: ping{ClientTime: 5}}, nil},
		{"envelope fields", `{"t":"Input","seq":7,"req":"r-9","tick":3,"d":{"left":true}}`, wsconn.Message{Type: "Input", Seq: 7, ReqID: "r-9", Tick: 3, Data: input{Left: true}}, nil},
		{"missing d is the zero value", `{"t":"Attack"}`, wsconn.Message{Type: "Attack", Data: empty{}}, nil},
		{"unknown fields in d are ignored, for forward compatibility", `{"t":"Ping","d":{"client_time":1,"extra":true}}`, wsconn.Message{Type: "Ping", Data: ping{ClientTime: 1}}, nil},
		{"unknown type", `{"t":"Teleport","d":{}}`, wsconn.Message{}, wsconn.ErrUnknownType},
		{"missing type", `{"d":{}}`, wsconn.Message{}, wsconn.ErrUnknownType},
		{"not JSON", `{"t":`, wsconn.Message{}, wsconn.ErrMalformed},
		{"wrong field type", `{"t":"Ping","d":{"client_time":"soon"}}`, wsconn.Message{}, wsconn.ErrMalformed},
		{"d is not an object", `{"t":"Ping","d":[1]}`, wsconn.Message{}, wsconn.ErrMalformed},
		{"empty frame", ``, wsconn.Message{}, wsconn.ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Decode([]byte(tt.raw))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	c := newCodec()
	if got := c.Types(); !reflect.DeepEqual(got, []string{"Attack", "Blob", "Input", "Ping"}) {
		t.Fatalf("Types = %v", got)
	}
	t.Run("registering a name twice panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		c.Register("Ping", ping{})
	})
	t.Run("registering a type under two names panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		c.Register("Ping2", ping{})
	})
	t.Run("a pointer type registers as its element", func(t *testing.T) {
		c2 := wsconn.NewJSONCodec()
		c2.Register("Ping", &ping{})
		b, err := c2.Encode(wsconn.Message{Data: ping{ClientTime: 2}})
		if err != nil || canon(t, b) != `{"d":{"client_time":2},"t":"Ping"}` {
			t.Fatalf("%s, %v", b, err)
		}
	})
}
