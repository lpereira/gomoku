// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"github.com/lpereira/gomoku/compiler"
	"log"
	"os"
)

func main() {
	c, err := compiler.New(os.Args, "outdir/")
	if err != nil {
		log.Fatalf("Could not create compiler: %s", err)
	}

	if err = c.Compile(); err != nil {
		log.Fatalf("Compilation failure: %s", err)
	}
}
