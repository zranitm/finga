package transport

import (
	"fmt"
	"reflect"

	clientv1 "finga/api/gen/client/v1"
	model "finga/internal/model/account"
)

// Encoders turn what the application pushes to a connection into the client protocol's messages. Each module's handler registers the encoders of the pushes it owns, and the account's registers the Welcome's; the wiring calls them all before the server starts.
type Encoders struct {
	byType  map[reflect.Type]func(any) any
	welcome []func(model.Welcome, *clientv1.Welcome) // each module's sections of the Welcome
}

// NewEncoders is an empty set of encoders.
func NewEncoders() *Encoders {
	return &Encoders{byType: map[reflect.Type]func(any) any{}}
}

// Encode registers f as the encoder of every push of type T; a second encoder for T is an error, so no push is encoded by whichever handler registered last.
func Encode[T any](e *Encoders, f func(T) any) error {
	t := reflect.TypeFor[T]()
	if _, dup := e.byType[t]; dup {
		return fmt.Errorf("transport: a second encoder for the push %s", t)
	}
	e.byType[t] = func(p any) any { return f(p.(T)) }
	return nil
}

// WelcomeSection registers a module's part of the Welcome: f fills the fields of out that the module owns. The sections run in the order they were registered.
func (e *Encoders) WelcomeSection(f func(w model.Welcome, out *clientv1.Welcome)) {
	e.welcome = append(e.welcome, f)
}

// welcomeOf is the Welcome message each module's section filled.
func (e *Encoders) welcomeOf(w model.Welcome) *clientv1.Welcome {
	out := &clientv1.Welcome{}
	for _, fill := range e.welcome {
		fill(w, out)
	}
	return out
}

// body is p's message, and false when no handler registered an encoder for its type.
func (e *Encoders) body(p any) (any, bool) {
	f, ok := e.byType[reflect.TypeOf(p)]
	if !ok {
		return nil, false
	}
	return f(p), true
}

// Complete reports an error if no module registered a section of the Welcome: a server whose Hello could not be answered must not start.
func (e *Encoders) Complete() error {
	if len(e.welcome) == 0 {
		return fmt.Errorf("transport: no section of the Welcome")
	}
	return nil
}
