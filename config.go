package easyssh

import "golang.org/x/crypto/ssh"

func PublicConfig() *ssh.ServerConfig {
	return &ssh.ServerConfig{
		NoClientAuth: true,
	}
}
