// The main package for the c-shared build of the _wsgo extension module.
// The Python entry point is PyInit__wsgo in internal/wsgo/module.c.
package main

import (
	_ "github.com/jonny5532/wsgo/internal/wsgo"
)

func main() {}
