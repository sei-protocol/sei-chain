package util

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/require"
)

func TestSSHContainerSelfDestruct(t *testing.T) {
	t.Skip("This test takes 5+ minutes to run - only enable for manual testing")

	ctx := context.Background()

	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)

	// Generate SSH key pair
	tempDir := t.TempDir()
	privateKeyPath := tempDir + "/test_ssh_key"
	publicKeyPath := tempDir + "/test_ssh_key.pub"

	err = GenerateSSHKeyPair(privateKeyPath, publicKeyPath)
	require.NoError(t, err)

	publicKeyContent, err := os.ReadFile(publicKeyPath)
	require.NoError(t, err)

	// Create mount directory for file operations
	mountDir := tempDir + "/ssh_mount"
	err = os.MkdirAll(mountDir, 0755)
	require.NoError(t, err)

	// Build Docker image
	imageName := "ssh-test-selfdestruct:latest"
	// Get current user's UID/GID for the container
	uid, err := getCurrentUserUID()
	require.NoError(t, err)
	gid, err := getCurrentUserGID()
	require.NoError(t, err)
	err = BuildSSHTestImage(ctx, cli, tempDir, imageName, string(publicKeyContent), uid, gid)
	require.NoError(t, err)

	// Start container
	containerID, sshPort, err := StartSSHContainer(ctx, cli, imageName, mountDir, t.Name())
	require.NoError(t, err)

	// Verify container is running
	containerInfo, err := cli.ContainerInspect(ctx, containerID)
	require.NoError(t, err)
	require.True(t, containerInfo.State.Running)

	// Wait for SSH to be ready
	WaitForSSH(t, sshPort, privateKeyPath)

	t.Logf("Container %s is running and SSH is ready. Waiting for self-destruct...", containerID[:12])

	// Wait for 6 minutes (container should self-destruct after 5 minutes)
	timeout := time.After(6 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	containerStopped := false
	for !containerStopped {
		select {
		case <-timeout:
			t.Fatal("Container did not self-destruct within 6 minutes")
		case <-ticker.C:
			containerInfo, err := cli.ContainerInspect(ctx, containerID)
			require.NoError(t, err)

			if !containerInfo.State.Running {
				containerStopped = true
				t.Logf("Container self-destructed successfully")
			} else {
				t.Logf("Container still running...")
			}
		}
	}

	// Clean up the stopped container
	err = cli.ContainerRemove(ctx, containerID, container.RemoveOptions{})
	require.NoError(t, err)
}
