//go:build oxigraph_embedded && darwin && arm64

package oxigraph

// #cgo LDFLAGS: -L${SRCDIR}/lib/darwin_arm64
import "C"
