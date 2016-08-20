package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"runtime"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var (
	// ServerContextKey is a context key. It can be used in handlers with
	// context.WithValue to access the server that started the handler. The
	// associated value will be of type *Server.
	ServerContextKey = &contextKey{"http-server"}

	// LocalAddrContextKey is a context key. It can be used in handlers with
	// context.WithValue to access the address the local address the connection
	// arrived on. The associated value will be of type net.Addr.
	LocalAddrContextKey = &contextKey{"local-addr"}

	// ErrServerHasNoHostKeys is returned when you try to start a server without adding any host keys
	ErrServerHasNoHostKeys = errors.New("server has no host keys")
)

// contextKey is a value for use with context.WithValue. It's used as
// a pointer so it fits in an interface{} without allocation.
type contextKey struct {
	name string
}

// Server is an SSH server
type Server struct {
	Addr    string  // TCP address to listen on, ":ssh" if empty
	Handler Handler // handler to invoke, DefaultHandler if nil

	// ConnState specifies an optional callback function that is
	// called when a client connection changes state. See the
	// ConnState type and associated constants for details.
	ConnState func(net.Conn, ConnState)

	// ErrorLog specifies an optional logger for errors accepting
	// connections and unexpected behavior from handlers.
	// If nil, logging goes to os.Stderr via the log package's
	// standard logger.
	ErrorLog *log.Logger

	// HostKey count to track the number of added host keys.
	//
	// This sucks a bit, but there's no way to get the number of host keys from
	// the ServerConfig directly.
	HostKeyCount int
	*ssh.ServerConfig
}

func (srv *Server) AddHostKey(r io.Reader) error {
	privateBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		return err
	}
	srv.HostKeyCount++
	cfg := srv.ServerConfig
	cfg.AddHostKey(private)
	return nil
}

// ListenAndServe listens on the TCP network address srv.Addr and then calls
// Serve to handle requests on incoming connections. Accepted connections are
// configured to enable TCP keep-alives. If srv.Addr is blank, ":ssh" is used.
// ListenAndServe always returns a non-nil error.
func (srv *Server) ListenAndServe() error {
	addr := srv.Addr
	if addr == "" {
		addr = ":ssh"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(tcpKeepAliveListener{ln.(*net.TCPListener)})
}

// Serve accepts incoming connections on the Listener l, creating a new service
// goroutine for each. The service goroutines read requests and then call
// srv.Handler to reply to them.
//
// Serve always returns a non-nil error.
func (srv *Server) Serve(l net.Listener) error {
	defer l.Close()
	var tempDelay time.Duration

	if srv.HostKeyCount == 0 {
		return ErrServerHasNoHostKeys
	}

	baseCtx := context.Background()
	ctx := context.WithValue(baseCtx, ServerContextKey, srv)
	ctx = context.WithValue(ctx, LocalAddrContextKey, l.Addr())
	for {
		rw, e := l.Accept()
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				srv.logf("ssh: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		c := srv.newConn(rw)
		c.setState(c.rwc, StateNew) // before Serve can return
		go c.serve(ctx)
	}
}

// debugServerConnections controls whether all server connections are wrapped
// with a verbose logging wrapper.
const debugServerConnections = false

// Create new connection from rwc.
func (srv *Server) newConn(rwc net.Conn) *conn {
	c := &conn{
		server: srv,
		rwc:    rwc,
	}
	if debugServerConnections {
		c.rwc = newLoggingConn("server", c.rwc)
	}
	return c
}

func (srv *Server) logf(format string, args ...interface{}) {
	if srv.ErrorLog != nil {
		srv.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// loggingConn is used for debugging.
type loggingConn struct {
	name string
	net.Conn
}

var (
	uniqNameMu   sync.Mutex
	uniqNameNext = make(map[string]int)
)

func newLoggingConn(baseName string, c net.Conn) net.Conn {
	uniqNameMu.Lock()
	defer uniqNameMu.Unlock()
	uniqNameNext[baseName]++
	return &loggingConn{
		name: fmt.Sprintf("%s-%d", baseName, uniqNameNext[baseName]),
		Conn: c,
	}
}

func (c *loggingConn) Write(p []byte) (n int, err error) {
	log.Printf("%s.Write(%d) = ....", c.name, len(p))
	n, err = c.Conn.Write(p)
	log.Printf("%s.Write(%d) = %d, %v", c.name, len(p), n, err)
	return
}

func (c *loggingConn) Read(p []byte) (n int, err error) {
	log.Printf("%s.Read(%d) = ....", c.name, len(p))
	n, err = c.Conn.Read(p)
	log.Printf("%s.Read(%d) = %d, %v", c.name, len(p), n, err)
	return
}

func (c *loggingConn) Close() (err error) {
	log.Printf("%s.Close() = ...", c.name)
	err = c.Conn.Close()
	log.Printf("%s.Close() = %v", c.name, err)
	return
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe and ListenAndServeTLS so
// dead TCP connections (e.g. closing laptop mid-download) eventually
// go away.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

type conn struct {
	server     *Server
	rwc        net.Conn
	remoteAddr string
}

func (c *conn) setState(nc net.Conn, state ConnState) {
	if hook := c.server.ConnState; hook != nil {
		hook(nc, state)
	}
}

// Serve a new connection
func (c *conn) serve(ctx context.Context) {
	c.remoteAddr = c.rwc.RemoteAddr().String()
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			c.server.logf("ssh: panic serving %v: %v\n%s", c.remoteAddr, err, buf)
		}
		c.close()
	}()

	// Before use, a handshake must be performed on the incoming net.Conn.
	c.setState(c.rwc, StateHandshake)
	sConn, chans, reqs, err := ssh.NewServerConn(c.rwc, c.server.ServerConfig)
	if err != nil {
		// TODO: handle error
		if err != io.EOF {
			c.server.logf("ssh: Handshake error: %v", err)
		}
		return
	}
	c.setState(c.rwc, StateActive)

	// The incoming Request channel must be serviced.
	go ssh.DiscardRequests(reqs) // TODO: Handle these

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			// TODO: Figure out what to do here. Are there errors which are like EOF?
			return
		}
		ctx, cancelCtx := context.WithCancel(ctx)
		var permissions *Permissions
		if sConn.Permissions != nil {
			permissions = &Permissions{*sConn.Permissions}
		}
		go serverHandler{c.server}.ServeSSH(
			permissions,
			Channel{
				Channel:   channel,
				ctx:       ctx,
				cancelCtx: cancelCtx,
			},
			wrapRequests(requests),
		)
	}
}

func (c *conn) close() {
	c.rwc.Close()
	c.setState(c.rwc, StateClosed)
}

// serverHandler delegates to either the server's Handler or DefaultHandler,
// and also cancels the context when finished.
type serverHandler struct {
	srv *Server
}

func (sh serverHandler) ServeSSH(p *Permissions, c Channel, reqs <-chan *Request) {
	handler := sh.srv.Handler
	if handler == nil {
		handler = DefaultHandler
	}
	handler.ServeSSH(p, c, reqs)
	c.cancelCtx()
}
