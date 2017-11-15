// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compiler

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/types"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/loader"
)

type Compiler struct {
	gen *cppGen

	conf    loader.Config
	program *loader.Program

	outDir string

	symFilter symbolFilter
}

type symbolFilter struct {
	generated map[*types.Scope]map[string]struct{}
}

func newSymbolFilter() symbolFilter {
	return symbolFilter{generated: make(map[*types.Scope]map[string]struct{})}
}

func (sf *symbolFilter) once(scope *types.Scope, symbol string) bool {
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

func NewCompiler(args []string, outDir string) (*Compiler, error) {
	comp := Compiler{
		outDir: outDir,
		conf: loader.Config{
			TypeChecker: types.Config{
				IgnoreFuncBodies: true,
				Importer:         importer.Default(),
			},
			AllowErrors: false,
		},
		symFilter: newSymbolFilter(),
	}

	_, err := comp.conf.FromArgs(args[1:], false)
	if err != nil {
		return nil, errors.Wrapf(err, "could not create program loader: %s", err)
	}

	comp.program, err = comp.conf.Load()
	if err != nil {
		return nil, errors.Wrapf(err, "could not load program: %s", err)
	}

	return &comp, nil
}

func (c *Compiler) genFile(name string, pkg *loader.PackageInfo, ast *ast.File, out func(*cppGen) error) error {
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return out(&cppGen{
		fset:                    c.program.Fset,
		ast:                     ast,
		pkg:                     pkg.Pkg,
		inf:                     pkg.Info,
		output:                  f,
		symFilter:               &c.symFilter,
		typeAssertFuncGenerated: make(map[string]struct{}),
	})
}

func (c *Compiler) genPackage(pkg *loader.PackageInfo) error {
	genImpl := func(gen *cppGen) error { return gen.GenerateImpl() }
	genHdr := func(gen *cppGen) error { return gen.GenerateHdr() }

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
			return fmt.Errorf("package %s is not complete", pkg.Pkg.Name())
		}

		if err := c.genPackage(pkg); err != nil {
			return fmt.Errorf("could not generate code for package %s: %s", pkg.Pkg.Name(), err)
		}
	}

	return nil
}
