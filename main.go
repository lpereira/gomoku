// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"log"
	"os"
	"github.com/lpereira/gomoku/compiler"
)

func main() {
	debugType := flag.Bool("debugtype", false, "Prints type system debugging information")
	flag.Parse()

	c, err := compiler.New(os.Stdin, os.Stdout)
	if err != nil {
		log.Fatalf("Could not create compiler: %s", err)
	}

	if *debugType {
		c.DebugTypeSystem()
		return
	}

	if err = c.Compile(); err != nil {
		log.Fatalf("Compilation failure: %s", err)
	}
}
