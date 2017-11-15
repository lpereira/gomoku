// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"
	"os"

	"github.com/lpereira/gomoku/compiler"
)

func main() {
	c, err := compiler.NewCompiler(os.Args, "outdir/")
	if err != nil {
		log.Fatalf("Could not create compiler: %s", err)
	}

	if err = c.Compile(); err != nil {
		log.Fatalf("Compilation failure: %s", err)
	}
}
