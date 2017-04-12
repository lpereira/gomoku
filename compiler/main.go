// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compiler

import (
	"fmt"
	"go/ast"
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

func (sf *SymbolFilter) Once(scope *types.Scope, symbol string) bool {
	if s, ok := sf.generated[scope]; ok {
		if _, ok = s[symbol]; ok {
			return false
		}
	} else {
		sf.generated[scope] = make(map[string]struct{})
	}

	sf.generated[scope][symbol] = struct{}{}
	return true
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

func (c *Compiler) genFile(name string, pkg *loader.PackageInfo, ast *ast.File, out func(*CppGen) error) error {
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	gen := CppGen{
		fset:         c.program.Fset,
		ast:          ast,
		pkg:          pkg.Pkg,
		inf:          pkg.Info,
		output:       f,
		symbolFilter: &c.symbolFilter,
	}

	if err := out(&gen); err != nil {
		return err
	}

	return nil
}

func (c *Compiler) genPackage(pkg *loader.PackageInfo) error {
	genImpl := func(gen *CppGen) error { return gen.GenerateImpl() }
	genHdr := func(gen *CppGen) error { return gen.GenerateHdr() }

	for _, ast := range pkg.Files {
		name := fmt.Sprintf("%s.cpp", ast.Name.Name)
		err := c.genFile(filepath.Join(c.outDir, name), pkg, ast, genImpl)
		if err != nil {
			return err
		}

		name = fmt.Sprintf("%s.h", ast.Name.Name)
		err = c.genFile(filepath.Join(c.outDir, name), pkg, ast, genHdr)
		if err != nil {
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
