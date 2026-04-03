package util

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHTestPortBase is the base port used for SSH testing to avoid port collisions in CI
const SSHTestPortBase = 22022

// GetFreeSSHTestPort returns a free port starting from SSHTestPortBase
func GetFreeSSHTestPort() (int, error) {
	// Try ports starting from the base port
	for port := SSHTestPortBase; port < SSHTestPortBase+100; port++ {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			continue // Port is in use, try next one
		}
		_ = listener.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port found in range %d-%d", SSHTestPortBase, SSHTestPortBase+100)
}

// GetUniqueSSHTestPort returns a unique port based on test name hash to avoid collisions
func GetUniqueSSHTestPort(testName string) (int, error) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(testName))
	hash := h.Sum32()

	for i := 0; i < 10; i++ {
		portOffset := int((hash + uint32(i)) % 100) //nolint:gosec
		port := SSHTestPortBase + portOffset

		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return port, nil
		}
		_ = conn.Close()
	}

	return GetFreeSSHTestPort()
}

// GenerateSSHKeyPair creates an RSA key pair for testing
func GenerateSSHKeyPair(privateKeyPath string, publicKeyPath string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	privateKeyFile, err := os.Create(privateKeyPath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to create private key file: %w", err)
	}
	defer func() { _ = privateKeyFile.Close() }()

	err = pem.Encode(privateKeyFile, privateKeyPEM)
	if err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}

	err = os.Chmod(privateKeyPath, 0600)
	if err != nil {
		return fmt.Errorf("failed to set private key permissions: %w", err)
	}

	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to create SSH public key: %w", err)
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)
	err = os.WriteFile(publicKeyPath, publicKeyBytes, 0644) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	return nil
}

// Docker-based SSH test infrastructure (SSHTestContainer, SetupSSHTestContainer,
// BuildSSHTestImage, StartSSHContainer, WaitForSSH, ArchiveDirectory, etc.) has been
// removed to avoid pulling in github.com/docker/docker as a dependency.
// Re-enable if Docker-based SSH integration testing becomes important in this repo.
