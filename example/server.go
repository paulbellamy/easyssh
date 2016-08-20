// A simple public chat server, over ssh. Messages from one client are sent to
// all other connected clients.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/paulbellamy/easyssh"
)

func main() {
	addr := flag.String("addr", ":12345", "address to listen on")
	flag.Parse()

	// Configure the server
	server := &easyssh.Server{
		Addr:    *addr,
		Handler: &topic{clients: make(map[int]easyssh.Channel)},

		// ConnState specifies an optional callback function that is
		// called when a client connection changes state. See the
		// ConnState type and associated constants for details.
		ConnState: func(conn net.Conn, state easyssh.ConnState) {
			log.Printf("[ConnState] %v: %s", conn.RemoteAddr(), state)
		},

		ServerConfig: easyssh.PublicConfig(),
	}

	// Generate a random key for now
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	privKeyPath := filepath.Join(tempDir, "key")
	out, err := exec.Command("ssh-keygen", "-f", privKeyPath, "-t", "rsa", "-N", "").CombinedOutput()
	if err != nil {
		log.Fatalf("Fail to generate private key: %v - %q", err, out)
	}
	privKeyFile, err := os.Open(privKeyPath)
	if err != nil {
		log.Fatal(err)
	}
	defer privKeyFile.Close()
	server.AddHostKey(privKeyFile)

	// Start the server
	log.Println("Listening on:", *addr)
	log.Fatal(server.ListenAndServe())
}

// Topic relays all messages from connected clients to all other clients
type topic struct {
	clients map[int]easyssh.Channel
	sync.RWMutex
}

func (t *topic) ServeSSH(p *easyssh.Permissions, c easyssh.Channel, r <-chan *easyssh.Request) {
	t.Lock()
	id := len(t.clients)
	t.clients[id] = c
	t.Unlock()

	defer func() {
		t.Lock()
		delete(t.clients, id)
		t.Unlock()
	}()

	go func() {
		defer c.Close()
		scanner := bufio.NewScanner(c)
		fmt.Fprintf(c, "> ")
		for scanner.Scan() {
			t.RLock()
			for otherID, client := range t.clients {
				if id != otherID {
					client.Write(scanner.Bytes())
					fmt.Fprintln(client)
				}
			}
			t.RUnlock()
			fmt.Fprintf(c, "> ")
		}
	}()

	for req := range r {
		ok := false
		switch req.Type {
		case "shell":
			ok = true
			if len(req.Payload) > 0 {
				// We don't accept any
				// commands, only the
				// default shell.
				ok = false
			}
		}
		req.Reply(ok, nil)
	}
}
