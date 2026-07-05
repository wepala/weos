//go:build !oxigraph_embedded

// Stub for builds without the `oxigraph_embedded` tag: the embedded backend
// is compiled out (no CGO, no vendored liboxigraph_ffi.a). NewEmbeddedStore
// reports it is unavailable so the provider falls back to the nop store — a
// pure-Go build still runs, with the knowledge graph simply off. The
// signature matches embedded.go so the provider compiles unchanged.

package oxigraph

import (
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// EmbeddedAvailable reports whether this binary was built with embedded
// support. Always false here; embedded.go's version returns true.
func EmbeddedAvailable() bool { return false }

// NewEmbeddedStore always fails in a no-embedded build. The provider treats
// the error like an open failure and degrades to nop.
func NewEmbeddedStore(_ string, _ entities.Logger) (repositories.KnowledgeGraphStore, error) {
	return nil, fmt.Errorf(
		"oxigraph embedded: this binary was not built with the 'oxigraph_embedded' tag")
}
