//go:build oxigraph_embedded && linux && arm64

package oxigraph

// #cgo LDFLAGS: -L${SRCDIR}/lib/linux_arm64
import "C"
