//go:build wsgo_embed

package wsgo

// The extension module leaves the Python symbols to be resolved when the
// interpreter loads it, so standalone binaries (such as the `go test`
// binaries) must link libpython themselves. Build them with -tags wsgo_embed.

/*
#cgo pkg-config: python3-embed
*/
import "C"
