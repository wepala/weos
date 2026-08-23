//go:build !oxigraph_embedded

// Stub for builds without the `oxigraph_embedded` tag: the embedded backend
// is compiled out (no CGO, no vendored liboxigraph_ffi.a). NewEmbeddedStore
// reports it is unavailable so the provider falls back to the nop store — a
// pure-Go build still runs, with the knowledge graph simply off. The
// signature matches embedded.go so the provider compiles unchanged.

package oxigraph

import (
	"errors"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// ErrEmbeddedUnavailable is what this build returns instead of a store. It is
// a sentinel rather than a fresh fmt.Errorf so callers can errors.Is it, and
// it is typed `error` rather than left to inference so a caller's `err != nil`
// is not statically provable — in a no-embedded build that check IS always
// true, and staticcheck flags it (SA4023), but the check is correct and
// load-bearing in the embedded build compiled from embedded.go.
var ErrEmbeddedUnavailable error = errors.New(
	"oxigraph embedded: this binary was not built with the 'oxigraph_embedded' tag")

// EmbeddedAvailable reports whether this binary was built with embedded
// support. Always false here; embedded.go's version returns true.
func EmbeddedAvailable() bool { return false }

// NewEmbeddedStore always fails in a no-embedded build. The provider treats
// the error like an open failure and degrades to nop.
func NewEmbeddedStore(_ string, _ entities.Logger) (repositories.KnowledgeGraphStore, error) {
	return nil, ErrEmbeddedUnavailable
}
