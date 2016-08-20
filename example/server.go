package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/paulbellamy/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

func main() {
	addr := flag.String("addr", ":12345", "address to listen on")
	flag.Parse()

	// Configure the server
	server := &ssh.Server{
		Addr:    *addr,
		Handler: ssh.HandlerFunc(handle),

		// ConnState specifies an optional callback function that is
		// called when a client connection changes state. See the
		// ConnState type and associated constants for details.
		ConnState: func(conn net.Conn, state ssh.ConnState) {
			log.Printf("[ConnState] %v: %s", conn.RemoteAddr(), state)
		},

		ServerConfig: &gossh.ServerConfig{
			PasswordCallback: func(c gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
				// Should use constant-time compare (or better, salt+hash) in
				// a production setting.
				if c.User() == "testuser" && string(pass) == "tiger" {
					return nil, nil
				}
				return nil, fmt.Errorf("password rejected for %q", c.User())
			},
		},
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

func handle(p *ssh.Permissions, c ssh.Channel, r <-chan *ssh.Request) {
	term := terminal.NewTerminal(c, "> ")

	go func() {
		defer c.Close()
		for {
			line, err := term.ReadLine()
			if err != nil {
				break
			}
			fmt.Println(line)
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
