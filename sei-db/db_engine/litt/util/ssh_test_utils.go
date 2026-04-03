package util

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// SSHTestPortBase is the base port used for SSH testing to avoid port collisions in CI
const SSHTestPortBase = 22022

const containerDataDir = "/mnt/data"
const username = "testuser"

// Global variables for shared SSH test image
var (
	sharedImageName string
	imageMutex      sync.Mutex
)

// getCurrentUserUID returns the current user's UID
func getCurrentUserUID() (int, error) {
	currentUser, err := user.Current()
	if err != nil {
		return 0, fmt.Errorf("failed to get current user: %w", err)
	}
	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil {
		return 0, fmt.Errorf("failed to convert UID to int: %w", err)
	}
	return uid, nil
}

// getCurrentUserGID returns the current user's GID
func getCurrentUserGID() (int, error) {
	currentUser, err := user.Current()
	if err != nil {
		return 0, fmt.Errorf("failed to get current user: %w", err)
	}
	gid, err := strconv.Atoi(currentUser.Gid)
	if err != nil {
		return 0, fmt.Errorf("failed to convert GID to int: %w", err)
	}
	return gid, nil
}

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
	// Create a hash of the test name to get a deterministic port offset
	h := fnv.New32a()
	_, _ = h.Write([]byte(testName))
	hash := h.Sum32()

	// Try multiple ports starting from the hash-based offset
	for i := 0; i < 10; i++ {
		portOffset := int((hash + uint32(i)) % 100)
		port := SSHTestPortBase + portOffset

		// Check if this port is free with a short timeout
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			// Port is free (connection failed)
			return port, nil
		}
		_ = conn.Close()
	}

	// If no port found in the hash range, fall back to free port finder
	return GetFreeSSHTestPort()
}

// SSHTestContainer manages a Docker container with SSH server for testing
type SSHTestContainer struct {
	t           *testing.T
	client      *client.Client
	containerID string
	sshPort     uint64
	tempDir     string
	privateKey  string
	publicKey   string
	host        string
	uid         int
	gid         int
}

// GetSSHPort returns the SSH port of the test container
func (c *SSHTestContainer) GetSSHPort() uint64 {
	return c.sshPort
}

// GetPrivateKeyPath returns the path to the private key file
func (c *SSHTestContainer) GetPrivateKeyPath() string {
	return c.privateKey
}

// GetPublicKeyPath returns the path to the public key file
func (c *SSHTestContainer) GetPublicKeyPath() string {
	return c.publicKey
}

// GetTempDir returns the temporary directory used by the container
func (c *SSHTestContainer) GetTempDir() string {
	return c.tempDir
}

// GetUser returns the SSH user for the test container
func (c *SSHTestContainer) GetUser() string {
	return username
}

// Get the UID of the user inside the container.
func (c *SSHTestContainer) GetUID() int {
	return c.uid
}

// Get the GID of the user inside the container.
func (c *SSHTestContainer) GetGID() int {
	return c.gid
}

// GetHost returns the host address for the SSH connection
func (c *SSHTestContainer) GetHost() string {
	return c.host
}

// GetDataDir returns the path to the container-controlled workspace directory
func (c *SSHTestContainer) GetDataDir() string {
	return containerDataDir
}

// delete the mounted data dir from within the container to avoid permission issues
func (c *SSHTestContainer) cleanupDataDir() error {

	// Create a temporary SSH session for cleanup
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	if err != nil {
		return fmt.Errorf("failed to create logger for cleanup: %w", err)
	}

	session, err := NewSSHSession(
		logger,
		c.GetUser(),
		c.host,
		c.sshPort,
		c.privateKey,
		"",
		false) // Don't log connection errors during cleanup
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	require.NotEqual(c.t, "", containerDataDir,
		"if this is an empty string then we will attempt to 'rm -rf /*'... let's not do that")

	// Remove the entire workspace directory tree from inside the container
	// This ensures container-owned files are removed by the container user
	cleanupCmd := fmt.Sprintf("rm -rf %s/*", containerDataDir)
	stdout, stderr, err := session.Exec(cleanupCmd)
	if err != nil {
		return fmt.Errorf("failed to cleanup workspace: %w\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	return nil
}

// Cleanup removes the Docker container and cleans up resources
func (c *SSHTestContainer) Cleanup() {
	err := c.cleanupDataDir()
	require.NoError(c.t, err)

	// Use a context with timeout for cleanup operations
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop and remove container with timeout
	stopTimeout := 10 // seconds
	err = c.client.ContainerStop(ctx, c.containerID, container.StopOptions{
		Timeout: &stopTimeout,
	})
	if err != nil {
		// Log the error but continue with removal
		fmt.Printf("Warning: failed to stop container %s: %v\n", c.containerID, err)
	}

	// Remove container even if stop failed
	err = c.client.ContainerRemove(ctx, c.containerID, container.RemoveOptions{
		Force: true, // Force removal even if container is still running
	})
	require.NoError(c.t, err)
}

// GenerateSSHKeyPair creates an RSA key pair for testing
func GenerateSSHKeyPair(privateKeyPath string, publicKeyPath string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Save private key
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	privateKeyFile, err := os.Create(privateKeyPath)
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

	// Save public key
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to create SSH public key: %w", err)
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)
	err = os.WriteFile(publicKeyPath, publicKeyBytes, 0644)
	if err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	return nil
}

// configureContainerSSHKey updates the container's SSH authorized_keys file with the test-specific public key
func configureContainerSSHKey(ctx context.Context, cli *client.Client, containerID string, publicKeyPath string) error {
	publicKeyContent, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key: %w", err)
	}

	// Use base64 encoding to safely pass the SSH key content without shell escaping issues
	// Base64 encoding ensures no shell metacharacters can cause problems
	encodedKey := base64.StdEncoding.EncodeToString(publicKeyContent)

	execConfig := container.ExecOptions{
		Cmd: []string{
			"sh", "-c",
			fmt.Sprintf(
				"echo '%s' | base64 -d > /home/%s/.ssh/authorized_keys && chmod 600 /home/%s/.ssh/authorized_keys",
				encodedKey, username, username),
		},
	}

	// Create the exec instance
	execIDResp, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec instance: %w", err)
	}

	// Start the exec instance with Detach: false to ensure it blocks until completion
	err = cli.ContainerExecStart(ctx, execIDResp.ID, container.ExecStartOptions{
		Detach: false, // Explicitly set to false to block until completion
	})
	if err != nil {
		return fmt.Errorf("failed to start exec instance: %w", err)
	}

	// With Detach: false, ContainerExecStart should block until completion.
	// However, to be absolutely certain, we'll add a brief polling loop.
	for i := 0; i < 10; i++ { // Max 10 attempts with 100ms intervals = 1 second max wait
		execInspect, err := cli.ContainerExecInspect(ctx, execIDResp.ID)
		if err != nil {
			return fmt.Errorf("failed to inspect exec instance: %w", err)
		}

		// If the command is no longer running, we can check the exit code
		if !execInspect.Running {
			// Check if the command was successful
			if execInspect.ExitCode != 0 {
				return fmt.Errorf("SSH key configuration command failed with exit code %d", execInspect.ExitCode)
			}
			return nil // Success!
		}

		// Brief sleep before checking again
		time.Sleep(10 * time.Millisecond)
	}

	// If still running after polling, something is wrong
	return fmt.Errorf("SSH key configuration command is still running after timeout")
}

// WaitForSSH waits for the SSH server to be ready
func WaitForSSH(t *testing.T, sshPort uint64, privateKeyPath string) {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	// Use a context with timeout to prevent indefinite hanging
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			require.Fail(t, "SSH server did not become ready within 30 seconds")
			return
		case <-ticker.C:
			session, err := NewSSHSession(
				logger,
				username,
				"localhost",
				sshPort,
				privateKeyPath,
				"",
				false)
			if err == nil {
				_ = session.Close()
				return
			}
			// Continue trying on error
		}
	}
}

// getOrBuildSharedSSHImage returns the name of the shared SSH test image.
// If the image doesn't exist, it builds it. This method is thread-safe.
func getOrBuildSharedSSHImage(ctx context.Context, cli *client.Client, t *testing.T) (string, error) {
	imageMutex.Lock()
	defer imageMutex.Unlock()

	// If we already have a cached image name, verify it still exists
	if sharedImageName != "" {
		_, err := cli.ImageInspect(ctx, sharedImageName)
		if err == nil {
			return sharedImageName, nil
		}
		// Image no longer exists, reset and rebuild
		sharedImageName = ""
	}

	// Get current user's UID/GID for the shared image
	uid, err := getCurrentUserUID()
	if err != nil {
		return "", fmt.Errorf("failed to get current user UID: %w", err)
	}
	gid, err := getCurrentUserGID()
	if err != nil {
		return "", fmt.Errorf("failed to get current user GID: %w", err)
	}

	// Generate a unique image name based on UID/GID and current time to avoid conflicts
	imageName := fmt.Sprintf("ssh-test-shared:%d-%d-%d", uid, gid, time.Now().Unix())

	// Create a temporary directory for building the image
	tempDir := t.TempDir()
	privateKeyPath := filepath.Join(tempDir, "shared_ssh_key")
	publicKeyPath := filepath.Join(tempDir, "shared_ssh_key.pub")

	// Generate SSH key pair for the shared image
	err = GenerateSSHKeyPair(privateKeyPath, publicKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to generate SSH key pair: %w", err)
	}

	publicKeyContent, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read public key: %w", err)
	}

	// Build the shared image
	t.Logf("Building shared SSH test Docker image: %s", imageName)
	err = BuildSSHTestImage(ctx, cli, tempDir, imageName, string(publicKeyContent), uid, gid)
	if err != nil {
		return "", fmt.Errorf("failed to build shared SSH image: %w", err)
	}

	// Cache the image name for future use
	sharedImageName = imageName
	return sharedImageName, nil
}

// SetupSSHTestContainer creates and starts a Docker container with SSH server
// If dataDir is not empty, it will be mounted in the container at /mnt/data
func SetupSSHTestContainer(t *testing.T, dataDir string) *SSHTestContainer {
	// Use a longer timeout for the entire setup process to handle slow CI environments
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Get current user's UID/GID
	uid, err := getCurrentUserUID()
	require.NoError(t, err)
	gid, err := getCurrentUserGID()
	require.NoError(t, err)

	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)

	// Generate SSH key pair for this specific test
	tempDir := t.TempDir()
	privateKeyPath := filepath.Join(tempDir, "test_ssh_key")
	publicKeyPath := filepath.Join(tempDir, "test_ssh_key.pub")

	err = GenerateSSHKeyPair(privateKeyPath, publicKeyPath)
	require.NoError(t, err)

	// Get or build the shared SSH test image
	imageName, err := getOrBuildSharedSSHImage(ctx, cli, t)
	require.NoError(t, err)

	if dataDir != "" {
		// we have to grant broad permissions here because the container may have a different UID
		err = os.Chmod(dataDir, 0777)
		require.NoError(t, err, "failed to set permissions on data directory")
	}

	// Start container and configure it with the test-specific SSH key
	containerID, sshPort, err := StartSSHContainer(ctx, cli, imageName, dataDir, t.Name())
	require.NoError(t, err)

	// Configure the container to use the test-specific SSH key
	err = configureContainerSSHKey(ctx, cli, containerID, publicKeyPath)
	require.NoError(t, err)

	// Wait for SSH to be ready
	WaitForSSH(t, sshPort, privateKeyPath)

	return &SSHTestContainer{
		t:           t,
		client:      cli,
		containerID: containerID,
		sshPort:     sshPort,
		tempDir:     tempDir,
		privateKey:  privateKeyPath,
		publicKey:   publicKeyPath,
		host:        "localhost",
		uid:         uid,
		gid:         gid,
	}
}

// BuildSSHTestImage builds the SSH test image with the provided public key and user IDs
func BuildSSHTestImage(
	ctx context.Context,
	cli *client.Client,
	tempDir string,
	imageName string,
	publicKey string,
	uid int,
	gid int,
) error {

	// Get the Dockerfile path
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("failed to get current file path")
	}
	dockerfilePath := filepath.Join(filepath.Dir(currentFile), "testdata", "ssh-test.Dockerfile")

	// Create build context directory
	buildContext := filepath.Join(tempDir, "docker_build")
	err := os.MkdirAll(buildContext, 0755)
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}

	// Copy Dockerfile to build context
	dockerfileContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return fmt.Errorf("failed to read Dockerfile: %w", err)
	}

	// Copy start.sh script to build context
	startScriptPath := filepath.Join(filepath.Dir(currentFile), "testdata", "start.sh")
	startScriptContent, err := os.ReadFile(startScriptPath)
	if err != nil {
		return fmt.Errorf("failed to read start.sh script: %w", err)
	}
	err = os.WriteFile(filepath.Join(buildContext, "start.sh"), startScriptContent, 0755)
	if err != nil {
		return fmt.Errorf("failed to copy start.sh to build context: %w", err)
	}

	// Add the public key setup to the Dockerfile
	publicKeySetup := fmt.Sprintf(
		"\n# Add test SSH public key\n"+
			"RUN echo '%s' > /home/testuser/.ssh/authorized_keys\n"+
			"RUN chmod 600 /home/testuser/.ssh/authorized_keys\n"+
			"RUN chown %d:%d /home/testuser/.ssh/authorized_keys\n", strings.TrimSpace(publicKey), uid, gid)
	modifiedDockerfile := string(dockerfileContent) + publicKeySetup

	err = os.WriteFile(filepath.Join(buildContext, "Dockerfile"), []byte(modifiedDockerfile), 0644)
	if err != nil {
		return fmt.Errorf("failed to write modified Dockerfile: %w", err)
	}

	// Create tar archive for build context
	buildCtx, err := ArchiveDirectory(buildContext)
	if err != nil {
		return fmt.Errorf("failed to create build context archive: %w", err)
	}
	defer func() { _ = buildCtx.Close() }()

	// Build the image with optimized settings for CI
	buildOptions := types.ImageBuildOptions{
		Tags:        []string{imageName},
		Dockerfile:  "Dockerfile",
		Remove:      true,
		ForceRemove: true,
		NoCache:     false, // Allow caching to speed up builds
		BuildArgs: map[string]*string{
			"USER_UID": &[]string{strconv.Itoa(uid)}[0],
			"USER_GID": &[]string{strconv.Itoa(gid)}[0],
		},
	}

	response, err := cli.ImageBuild(ctx, buildCtx, buildOptions)
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	// Read build output with proper error handling for timeouts
	// Create a buffer to capture build output for debugging on failure
	var buildOutput strings.Builder
	reader := io.TeeReader(response.Body, &buildOutput)

	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		// Include build output in error for debugging
		buildOutputStr := buildOutput.String()
		if len(buildOutputStr) > 1000 {
			buildOutputStr = buildOutputStr[:1000] + "... (truncated)"
		}
		return fmt.Errorf("failed to read build response: %w\nBuild output: %s", err, buildOutputStr)
	}

	// After the build finishes, verify the image actually exists
	_, err = cli.ImageInspect(ctx, imageName)
	if err != nil {
		buildOutputStr := buildOutput.String()
		if len(buildOutputStr) > 2000 {
			buildOutputStr = buildOutputStr[:2000] + "... (truncated)"
		}
		return fmt.Errorf("docker image build failed - image not found after build: %w\nBuild output: %s",
			err, buildOutputStr)
	}

	return nil
}

// StartSSHContainer starts the SSH container and returns container ID and SSH port
// If dataDir is not empty, it will be mounted at /mnt/data in the container
func StartSSHContainer(
	ctx context.Context,
	cli *client.Client,
	imageName string,
	dataDir string,
	testName string,
) (string, uint64, error) {

	// Get a unique port for this test based on test name hash
	sshPort, err := GetUniqueSSHTestPort(testName)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get unique SSH port: %w", err)
	}

	containerConfig := &container.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			"22/tcp": struct{}{},
		},
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"22/tcp": []nat.PortBinding{
				{
					HostIP:   "127.0.0.1",
					HostPort: strconv.Itoa(sshPort), // Use custom port to avoid collisions in CI
				},
			},
		},
		Mounts: func() []mount.Mount {
			var mounts []mount.Mount
			if dataDir != "" {
				mounts = append(mounts, mount.Mount{
					Type:   mount.TypeBind,
					Source: dataDir,
					Target: "/mnt/data",
				})
			}
			return mounts
		}(),
	}

	// Create a container name that includes the test name for easier debugging
	containerName := fmt.Sprintf("ssh-test-%s-%d",
		strings.ReplaceAll(testName, "/", "-"), time.Now().Unix())

	resp, err := cli.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		nil,
		nil,
		containerName)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create container: %w", err)
	}

	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("failed to start container: %w", err)
	}

	// Use the custom SSH port (convert to uint64 for compatibility)
	return resp.ID, uint64(sshPort), nil
}

// ArchiveDirectory creates a tar.gz archive of a directory for Docker build context
func ArchiveDirectory(srcDir string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	go func() {
		defer func() { _ = pw.Close() }()

		gw := gzip.NewWriter(pw)
		defer func() { _ = gw.Close() }()

		tw := tar.NewWriter(gw)
		defer func() { _ = tw.Close() }()

		_ = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(srcDir, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path: %w", err)
			}

			// Skip the root directory itself
			if relPath == "." {
				return nil
			}

			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return fmt.Errorf("failed to create tar header: %w", err)
			}
			header.Name = relPath

			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header for %s: %w", relPath, err)
			}

			if info.IsDir() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", path, err)
			}
			defer func() { _ = file.Close() }()

			_, err = io.Copy(tw, file)
			if err != nil {
				return fmt.Errorf("failed to copy file %s to tar: %w", path, err)
			}
			return nil
		})
	}()

	return pr, nil
}
