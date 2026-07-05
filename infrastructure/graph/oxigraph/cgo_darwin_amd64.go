//go:build oxigraph_embedded && darwin && amd64

package oxigraph

// #cgo LDFLAGS: -L${SRCDIR}/lib/darwin_amd64
import "C"
