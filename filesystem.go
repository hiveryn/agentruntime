package agentruntime

import "os"

// FileSystem is the target environment used to prepare provider configuration.
// Callers supply a remote implementation when launching elsewhere. Paths returned
// by WriteTemp belong to that target, and CleanupPaths must be removed there.
// Implementations must preserve os.ErrNotExist for missing files and must not
// substitute local environment variables or credentials for target values.
type FileSystem interface {
	ReadFile(string) ([]byte, error)
	WriteFile(string, []byte, os.FileMode) error
	MkdirAll(string, os.FileMode) error
	Remove(string) error
	UserHomeDir() (string, error)
	Getenv(string) string
	WriteTemp(string, []byte) (string, error)
}

type LocalFileSystem struct{}

func (LocalFileSystem) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }
func (LocalFileSystem) WriteFile(p string, b []byte, m os.FileMode) error {
	return os.WriteFile(p, b, m)
}
func (LocalFileSystem) MkdirAll(p string, m os.FileMode) error { return os.MkdirAll(p, m) }
func (LocalFileSystem) Remove(p string) error                  { return os.Remove(p) }
func (LocalFileSystem) UserHomeDir() (string, error)           { return os.UserHomeDir() }
func (LocalFileSystem) Getenv(k string) string                 { return os.Getenv(k) }
func (LocalFileSystem) WriteTemp(pattern string, b []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	p := f.Name()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(p)
		return "", err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(p)
		return "", err
	}
	return p, nil
}

// TargetFileSystem defaults to the local OS for existing callers.
func TargetFileSystem(fs ...FileSystem) FileSystem {
	if len(fs) > 0 && fs[0] != nil {
		return fs[0]
	}
	return LocalFileSystem{}
}
