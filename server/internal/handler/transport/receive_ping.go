package transport

import (
	"time"

	clientv1 "finga/api/gen/client/v1"
)

// ping answers at once, on the read loop.
func (h *Receiver) ping(c *Context, p *clientv1.Ping) (any, error) {
	c.Session.out.Reply("", &clientv1.Pong{ClientTime: p.GetClientTime(), ServerTime: time.Now().UnixMilli()})
	return nil, nil
}
