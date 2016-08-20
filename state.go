package ssh

// A ConnState represents the state of a client connection to a server.
// It's used by the optional Server.ConnState hook.
type ConnState int

const (
	// StateNew represents a new connection that has newly connected. Connections
	// begin at this state and then transition to either StateActive or
	// StateClosed.
	StateNew ConnState = iota

	// StateHandshake represents a connection that is currently performing the
	// handshake.
	StateHandshake

	// StateActive represents a connection that has read 1 or more
	// bytes of a request.
	StateActive

	// StateClosed represents a closed connection.
	// This is a terminal state.
	StateClosed
)

func (s ConnState) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateHandshake:
		return "handshake"
	case StateActive:
		return "active"
	case StateClosed:
		return "closed"
	}
	return "unknown"
}
