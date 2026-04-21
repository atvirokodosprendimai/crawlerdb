package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atvirokodosprendimai/crawlerdb/pkg/uid"
)

const identityFileName = "worker.id"

// Identity manages persistent worker identity.
type Identity struct {
	id   string
	path string
}

// LoadOrCreate reads the worker ID from file, or generates and persists a new one.
func LoadOrCreate(dir string) (*Identity, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create identity dir: %w", err)
	}

	path := filepath.Join(dir, identityFileName)

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return &Identity{id: id, path: path}, nil
		}
	}

	// Generate new ID.
	id := uid.NewID()
	if err := os.WriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write worker ID: %w", err)
	}

	return &Identity{id: id, path: path}, nil
}

// ID returns the persistent worker ID.
func (i *Identity) ID() string {
	return i.id
}

// Path returns the file path where the ID is stored.
func (i *Identity) Path() string {
	return i.path
}
