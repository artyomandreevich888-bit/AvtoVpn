//go:build tools

package tools

// Pin gomobile dependencies so `go mod tidy` doesn't remove them.
import _ "golang.org/x/mobile/bind"
