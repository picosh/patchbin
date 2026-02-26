package util

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

func CreateTmpDir() string {
	tmp, err := os.MkdirTemp(os.TempDir(), "patchbin*")
	if err != nil {
		panic(err)
	}
	return tmp
}

func CreateCfgFile(dataDir, cfgTmpl string, adminKey UserSSH) string {
	cfgPath := filepath.Join(dataDir, "patchbin.toml")
	cfgFi, err := os.Create(cfgPath)
	if err != nil {
		panic(err)
	}
	_, _ = fmt.Fprintf(cfgFi, cfgTmpl, dataDir, adminKey.Public())
	_ = cfgFi.Close()
	return cfgPath
}

type UserSSH struct {
	username string
	signer   ssh.Signer
}

func NewUserSSH(username string, signer ssh.Signer) *UserSSH {
	return &UserSSH{
		username: username,
		signer:   signer,
	}
}

func (s UserSSH) Public() string {
	pubkey := s.signer.PublicKey()
	return string(ssh.MarshalAuthorizedKey(pubkey))
}

func (s UserSSH) MustCmd(patch []byte, cmd string) string {
	res, err := s.Cmd(patch, cmd)
	if err != nil {
		panic(err)
	}
	return res
}

func (s UserSSH) Cmd(patch []byte, cmd string) (string, error) {
	host := "localhost:2222"

	config := &ssh.ClientConfig{
		User: s.username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(s.signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = client.Close()
	}()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer func() {
		_ = session.Close()
	}()

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return "", err
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return "", err
	}

	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := session.Start(cmd); err != nil {
		return "", err
	}

	if patch != nil {
		_, err = stdinPipe.Write(patch)
		if err != nil {
			return "", err
		}
	}

	_ = stdinPipe.Close()

	var stdoutBuf, stderrBuf strings.Builder
	go func() { _, _ = io.Copy(&stderrBuf, stderrPipe) }()
	_, _ = io.Copy(&stdoutBuf, stdoutPipe)

	err = session.Wait()
	stderr := stderrBuf.String()
	if err != nil {
		return "", fmt.Errorf("ssh command failed: %w (stderr: %s)", err, stderr)
	}

	return stdoutBuf.String(), nil
}

// ParsePRID extracts the PR ID from the output of `pr create`.
// Looks for the URL line: "URL: https://host/prs/123"
func ParsePRID(output string) string {
	re := regexp.MustCompile(`/prs/(\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 2 {
		return "1" // fallback
	}
	return matches[1]
}

func GenerateKeys() (UserSSH, UserSSH) {
	_, adminKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	adminSigner, err := ssh.NewSignerFromKey(adminKey)
	if err != nil {
		panic(err)
	}

	_, userKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	userSigner, err := ssh.NewSignerFromKey(userKey)
	if err != nil {
		panic(err)
	}

	return UserSSH{
			username: "admin",
			signer:   adminSigner,
		}, UserSSH{
			username: "contributor",
			signer:   userSigner,
		}
}
