package quic

import "context"

type retireTransportRequest struct {
	transport *Transport
	result    chan error
}

// RetireTransport detaches this connection from an inactive transport, allowing
// its owner to close the socket without destroying the migrated connection.
// The caller must first close any Path using the transport and never reuse it.
func (c *Conn) RetireTransport(ctx context.Context, t *Transport) error {
	req := retireTransportRequest{transport: t, result: make(chan error, 1)}
	select {
	case c.retireTransport <- req:
	case <-c.Context().Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.result:
		return err
	case <-c.Context().Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
