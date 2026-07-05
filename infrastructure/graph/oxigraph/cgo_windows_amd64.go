//go:build oxigraph_embedded && windows && amd64

package oxigraph

// #cgo LDFLAGS: -L${SRCDIR}/lib/windows_amd64
import "C"
