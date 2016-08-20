package ssh

import (
	"context"

	"golang.org/x/crypto/ssh"
)

type Permissions struct {
	ssh.Permissions
}

type Channel struct {
	ssh.Channel
	ctx       context.Context
	cancelCtx context.CancelFunc
}

type Request struct {
	ssh.Request
}

func wrapRequests(requests <-chan *ssh.Request) <-chan *Request {
	wrapped := make(chan *Request)
	go func() {
		for r := range requests {
			wrapped <- &Request{*r}
		}
	}()
	return wrapped
}

func unwrapRequests(requests <-chan *Request) <-chan *ssh.Request {
	unwrapped := make(chan *ssh.Request)
	go func() {
		for r := range requests {
			unwrapped <- &r.Request
		}
	}()
	return unwrapped
}
