// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compiler

import (
	"fmt"
	"go/importer"
	"go/types"
	"golang.org/x/tools/go/loader"
	"os"
	"path/filepath"
)

type Compiler struct {
	gen *CppGen

	conf    loader.Config
	program *loader.Program

	outDir string

	symbolFilter SymbolFilter
}

type SymbolFilter struct {
	generated map[*types.Scope]map[string]struct{}
}

func NewSymbolFilter() SymbolFilter {
	return SymbolFilter{generated: make(map[*types.Scope]map[string]struct{})}
}

func (sf *SymbolFilter) MarkGenerated(scope *types.Scope, symbol string) {
	sf.generated[scope][symbol] = struct{}{}
}

func (sf *SymbolFilter) Generated(scope *types.Scope, symbol string) bool {
	if s, ok := sf.generated[scope]; ok {
		_, ok = s[symbol]
		return ok
	}
	sf.generated[scope] = make(map[string]struct{})
	return false
}

func New(args []string, outDir string) (*Compiler, error) {
	compiler := Compiler{
		outDir: outDir,
		conf: loader.Config{
			TypeChecker: types.Config{
				IgnoreFuncBodies: true,
				Importer:         importer.Default(),
			},
			AllowErrors: false,
		},
		symbolFilter: NewSymbolFilter(),
	}

	_, err := compiler.conf.FromArgs(args[1:], false)
	if err != nil {
		return nil, fmt.Errorf("Could not create program loader: %s", err)
	}

	compiler.program, err = compiler.conf.Load()
	if err != nil {
		return nil, fmt.Errorf("Could not load program: %s", err)
	}

	return &compiler, nil
}

func (c *Compiler) genPackage(pkg *loader.PackageInfo) error {
	name := filepath.Join(c.outDir, fmt.Sprintf("%s.cpp", pkg.Pkg.Name()))
	out, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	fmt.Printf("Generating: %s\n", name)

	for _, ast := range pkg.Files {
		gen := CppGen{
			fset:   c.program.Fset,
			ast:    ast,
			pkg:    pkg.Pkg,
			inf:    pkg.Info,
			output: out,
		}
		if err := gen.Generate(); err != nil {
			return err
		}
	}
	return nil
}

func clearDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer dir.Close()

	entries, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err = os.RemoveAll(filepath.Join(path, entry)); err != nil {
			return err
		}
	}

	return nil
}

func (c *Compiler) Compile() error {
	if err := clearDirectory(c.outDir); err != nil {
		return err
	}
	os.Remove(c.outDir)
	if err := os.Mkdir(c.outDir, 0755); err != nil {
		return err
	}

	for _, pkg := range c.program.AllPackages {
		if pkg.Pkg.Name() == "runtime" {
			continue
		}

		if !pkg.Pkg.Complete() {
			return fmt.Errorf("Package %s is not complete", pkg.Pkg.Name())
		}

		if err := c.genPackage(pkg); err != nil {
			return fmt.Errorf("Could not generate code for package %s: %s", pkg.Pkg.Name(), err)
		}
	}

	return nil
}
