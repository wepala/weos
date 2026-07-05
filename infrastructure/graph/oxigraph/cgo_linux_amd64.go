//go:build oxigraph_embedded && linux && amd64

package oxigraph

// #cgo LDFLAGS: -L${SRCDIR}/lib/linux_amd64
import "C"
