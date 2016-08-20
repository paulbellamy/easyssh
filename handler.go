package ssh

import "golang.org/x/crypto/ssh"

var DefaultHandler = HandlerFunc(func(p *Permissions, c Channel, r <-chan *Request) {
	ssh.DiscardRequests(unwrapRequests(r))
})

// Handler handles ssh connections
type Handler interface {
	ServeSSH(*Permissions, Channel, <-chan *Request) // TODO: Finish filling this in
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as handlers. If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler that calls f.
type HandlerFunc func(*Permissions, Channel, <-chan *Request)

// ServeSSH calls f(p, c, r).
func (f HandlerFunc) ServeSSH(p *Permissions, c Channel, r <-chan *Request) {
	f(p, c, r)
}
