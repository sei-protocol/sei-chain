package util

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecursiveMoveDoNotPreserve(t *testing.T) {
	// Create a small file tree
	root1 := t.TempDir()
	foo := path.Join(root1, "foo")
	bar := path.Join(root1, "bar")
	baz := path.Join(root1, "baz")
	alpha := path.Join(foo, "alpha")
	beta := path.Join(foo, "beta")
	gamma := path.Join(foo, "gamma")

	fileA := path.Join(alpha, "fileA.txt")
	fileB := path.Join(beta, "fileB.txt")
	fileC := path.Join(foo, "fileC.txt")
	fileD := path.Join(bar, "fileD.txt")

	err := EnsureDirectoryExists(foo, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(bar, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(baz, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(alpha, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(beta, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(gamma, false)
	require.NoError(t, err)

	dataA := []byte("This is file A")
	err = os.WriteFile(fileA, dataA, 0644)
	require.NoError(t, err)

	dataB := []byte("This is file B")
	err = os.WriteFile(fileB, dataB, 0644)
	require.NoError(t, err)

	dataC := []byte("This is file C")
	err = os.WriteFile(fileC, dataC, 0644)
	require.NoError(t, err)

	dataD := []byte("This is file D")
	err = os.WriteFile(fileD, dataD, 0644)
	require.NoError(t, err)

	// move the data
	root2 := t.TempDir()
	err = RecursiveMove(root1, root2, false, false)
	require.NoError(t, err)

	// verify that the file tree exists in the new location
	require.NoError(t, ErrIfNotExists(strings.Replace(foo, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(bar, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(baz, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(alpha, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(beta, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(gamma, root1, root2, 1)))

	dataInFileA, err := os.ReadFile(strings.Replace(fileA, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataA, dataInFileA)

	dataInFileB, err := os.ReadFile(strings.Replace(fileB, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataB, dataInFileB)

	dataInFileC, err := os.ReadFile(strings.Replace(fileC, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataC, dataInFileC)

	dataInFileD, err := os.ReadFile(strings.Replace(fileD, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataD, dataInFileD)

	// Original directory should be gone
	require.NoError(t, ErrIfExists(root1))
}

func TestRecursiveMovePreserve(t *testing.T) {
	// Create a small file tree
	root1 := t.TempDir()
	foo := path.Join(root1, "foo")
	bar := path.Join(root1, "bar")
	baz := path.Join(root1, "baz")
	alpha := path.Join(foo, "alpha")
	beta := path.Join(foo, "beta")
	gamma := path.Join(foo, "gamma")

	fileA := path.Join(alpha, "fileA.txt")
	fileB := path.Join(beta, "fileB.txt")
	fileC := path.Join(foo, "fileC.txt")
	fileD := path.Join(bar, "fileD.txt")

	err := EnsureDirectoryExists(foo, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(bar, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(baz, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(alpha, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(beta, false)
	require.NoError(t, err)
	err = EnsureDirectoryExists(gamma, false)
	require.NoError(t, err)

	dataA := []byte("This is file A")
	err = os.WriteFile(fileA, dataA, 0644)
	require.NoError(t, err)

	dataB := []byte("This is file B")
	err = os.WriteFile(fileB, dataB, 0644)
	require.NoError(t, err)

	dataC := []byte("This is file C")
	err = os.WriteFile(fileC, dataC, 0644)
	require.NoError(t, err)

	dataD := []byte("This is file D")
	err = os.WriteFile(fileD, dataD, 0644)
	require.NoError(t, err)

	// move the data
	root2 := t.TempDir()
	err = RecursiveMove(root1, root2, true, false)
	require.NoError(t, err)

	// verify that the file tree exists in the new location
	require.NoError(t, ErrIfNotExists(strings.Replace(foo, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(bar, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(baz, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(alpha, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(beta, root1, root2, 1)))
	require.NoError(t, ErrIfNotExists(strings.Replace(gamma, root1, root2, 1)))

	dataInFileA, err := os.ReadFile(strings.Replace(fileA, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataA, dataInFileA)

	dataInFileB, err := os.ReadFile(strings.Replace(fileB, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataB, dataInFileB)

	dataInFileC, err := os.ReadFile(strings.Replace(fileC, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataC, dataInFileC)

	dataInFileD, err := os.ReadFile(strings.Replace(fileD, root1, root2, 1))
	require.NoError(t, err)
	require.Equal(t, dataD, dataInFileD)

	// Original directory still be present and intact
	require.NoError(t, ErrIfNotExists(foo))
	require.NoError(t, ErrIfNotExists(bar))
	require.NoError(t, ErrIfNotExists(baz))
	require.NoError(t, ErrIfNotExists(alpha))
	require.NoError(t, ErrIfNotExists(beta))
	require.NoError(t, ErrIfNotExists(gamma))

	dataInFileA, err = os.ReadFile(fileA)
	require.NoError(t, err)
	require.Equal(t, dataA, dataInFileA)

	dataInFileB, err = os.ReadFile(fileB)
	require.NoError(t, err)
	require.Equal(t, dataB, dataInFileB)

	dataInFileC, err = os.ReadFile(fileC)
	require.NoError(t, err)
	require.Equal(t, dataC, dataInFileC)

	dataInFileD, err = os.ReadFile(fileD)
	require.NoError(t, err)
	require.Equal(t, dataD, dataInFileD)
}
