// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"reflect"
	"strings"
)

type Compiler struct {
	fset *token.FileSet
	ast  *ast.File

	pkg *types.Package
	inf types.Info

	initPkgs []string

	input  io.Reader
	output io.Writer

	recvs VarStack

	idents int
}

type VarStack struct {
	vars  []*types.Var
	count int
}

func (s *VarStack) Push(v *types.Var) {
	s.vars = append(s.vars[:s.count], v)
	s.count++
}

func (s *VarStack) Pop() *types.Var {
	if s.count == 0 {
		return nil
	}
	s.count--
	return s.vars[s.count]
}

func (s *VarStack) Curr() *types.Var { return s.vars[s.count-1] }

func (s *VarStack) Lookup(name string) *types.Var {
	for cur := s.count - 1; cur >= 0; cur-- {
		if v := s.vars[cur]; v != nil && name == v.Name() {
			return s.vars[cur]
		}
	}
	return nil
}

func New(in io.Reader, out io.Writer) (*Compiler, error) {
	c := Compiler{
		fset: token.NewFileSet(),
		inf: types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
		},
		input:  in,
		output: out,
	}

	f, err := parser.ParseFile(c.fset, "input.go", in, 0)
	if err != nil {
		return nil, err
	}

	conf := types.Config{
		Importer:         importer.Default(),
		IgnoreFuncBodies: false,
	}
	pkg, err := conf.Check("test/input", c.fset, []*ast.File{f}, &c.inf)
	if err != nil {
		return nil, err
	}

	c.ast = f
	c.pkg = pkg

	return &c, nil
}

type basicTypeInfo struct {
	nilVal string
	typ    string
}

var basicTypeToCpp map[types.BasicKind]basicTypeInfo
var goTypeToBasic map[string]types.BasicKind

func init() {
	basicTypeToCpp = map[types.BasicKind]basicTypeInfo{
		types.Bool:          {"false", "bool"},
		types.UntypedBool:   {"false", "bool"},
		types.Int:           {"0", "int"},
		types.UntypedInt:    {"0", "int"},
		types.Int8:          {"0", "int8_t"},
		types.Int16:         {"0", "int16_t"},
		types.Int32:         {"0", "int32_t"},
		types.Int64:         {"0", "int64_t"},
		types.Uint:          {"0", "unsigned int"},
		types.Uint8:         {"0", "uint8_t"},
		types.Uint16:        {"0", "uint16_t"},
		types.Uint32:        {"0", "uint32_t"},
		types.Uint64:        {"0", "uint64_t"},
		types.Uintptr:       {"0", "uintptr_t"},
		types.Float32:       {"0", "float"},
		types.UntypedFloat:  {"0", "float"},
		types.Float64:       {"0", "double"},
		types.String:        {"0", "std::string"},
		types.UntypedString: {"0", "std::string"},
		types.UnsafePointer: {"0", "void*"},
	}
	goTypeToBasic = map[string]types.BasicKind{
		"bool":    types.Bool,
		"int":     types.Int,
		"int8":    types.Int8,
		"int16":   types.Int16,
		"int32":   types.Int32,
		"int64":   types.Int64,
		"uint":    types.Uint,
		"uint8":   types.Uint8,
		"uint16":  types.Uint16,
		"uint32":  types.Uint32,
		"uint64":  types.Uint64,
		"uintptr": types.Uintptr,
		"float32": types.Float32,
		"float64": types.Float64,
		"string":  types.String,
	}
}

func (c *Compiler) newIdent() (ret string) {
	ret = fmt.Sprintf("_ident_%d_", c.idents)
	c.idents++
	return
}

func (c *Compiler) toTypeSig(t types.Type) (string, error) {
	f := func(t types.Type) (string, error) {
		switch typ := t.(type) {
		default:
			return "", fmt.Errorf("Unknown type: %s", reflect.TypeOf(typ))

		case *types.Chan:
			elemTyp, err := c.toTypeSig(typ.Elem())
			if err != nil {
				return "", nil
			}

			var dirMod string
			switch typ.Dir() {
			case types.SendRecv:
				dirMod = "true, true"
			case types.SendOnly:
				dirMod = "true, false"
			case types.RecvOnly:
				dirMod = "false, true"
			}

			return fmt.Sprintf("moku::channel<%s, %s>", elemTyp, dirMod), nil

		case *types.Map:
			k, err := c.toTypeSig(typ.Key())
			if err != nil {
				return "", err
			}

			v, err := c.toTypeSig(typ.Elem())
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("std::map<%s, %s>", k, v), nil

		case *types.Slice:
			s, err := c.toTypeSig(typ.Elem())
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("moku::slice<%s>", s), nil

		case *types.Array:
			s, err := c.toTypeSig(typ.Elem())
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("std::vector<%s>", s), nil

		case *types.Pointer:
			s, err := c.toTypeSig(typ.Elem())
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("%s*", s), nil

		case *types.Interface:
			if typ.Empty() {
				return "moku::empty_interface", nil
			}

			return c.toTypeSig(typ.Underlying())

		case *types.Named:
			switch typ.Obj().Name() {
			case "error":
				return "moku::error", nil
			default:
				return typ.Obj().Name(), nil
			}

		case *types.Basic:
			if v, ok := basicTypeToCpp[typ.Kind()]; ok {
				return v.typ, nil
			}

			return "", fmt.Errorf("Unsupported basic type: %s", typ)

		case *types.Tuple:
			var r []string

			items := typ.Len()
			for i := 0; i < items; i++ {
				s, err := c.toTypeSig(typ.At(i).Type())
				if err != nil {
					return "", err
				}

				r = append(r, s)
			}

			return strings.Join(r, ", "), nil

		case *types.Signature:
			var retType []string
			if r := typ.Results(); r != nil {
				s, err := c.toTypeSig(r)
				if err != nil {
					return "", err
				}
				retType = append(retType, s)
			} else {
				retType = append(retType, "void")
			}

			var paramTypes []string
			if p := typ.Params(); p != nil {
				s, err := c.toTypeSig(p)
				if err != nil {
					return "", err
				}
				paramTypes = append(paramTypes, s)
			}

			p := strings.Join(paramTypes, ", ")
			if len(retType) == 1 {
				r := retType[0]
				return fmt.Sprintf("std::function<%s(%s)>", r, p), nil
			}

			r := strings.Join(retType, ", ")
			return fmt.Sprintf("std::function<std::tuple<%s>(%s)>", r, p), nil
		}
	}

	sig, err := f(t)
	if err != nil {
		return sig, err
	}

	return sig, nil
}

func (c *Compiler) toNilVal(t types.Type) (string, error) {
	f := func(t types.Type) (string, error) {
		switch typ := t.(type) {
		case *types.Basic:
			if v, ok := basicTypeToCpp[typ.Kind()]; ok {
				return v.nilVal, nil
			}
		case *types.Pointer, *types.Signature:
			return "std::nullptr", nil

		case *types.Slice, *types.Map, *types.Chan,
			*types.Interface, *types.Named, *types.Array:

			return "", nil
		}

		err := fmt.Errorf("Unknown nil value for type %s", reflect.TypeOf(t))
		return "", err
	}

	nilVal, err := f(t)
	if err != nil {
		return nilVal, err
	}

	if types.IsInterface(t) {
		return "", nil
	}

	return nilVal, err
}

func (c *Compiler) genFuncProto(name string, sig *types.Signature, out func(name, retType, params string) error) (err error) {
	sigParm := sig.Params()
	var params []string
	for p := 0; p < sigParm.Len(); p++ {
		parm := sigParm.At(p)
		typ, err := c.toTypeSig(parm.Type())
		if err != nil {
			return err
		}

		params = append(params, fmt.Sprintf("%s %s", typ, parm.Name()))
	}

	res := sig.Results()
	var retType string
	switch res.Len() {
	case 0:
		retType = "void"
	case 1:
		s, err := c.toTypeSig(res.At(0).Type())
		if err != nil {
			return err
		}
		retType = s
	default:
		var mult []string

		for r := 0; r < res.Len(); r++ {
			s, err := c.toTypeSig(res.At(r).Type())
			if err != nil {
				return err
			}

			mult = append(mult, s)
		}

		retType = fmt.Sprintf("std::tuple<%s>", strings.Join(mult, ", "))
	}

	return out(name, retType, strings.Join(params, ", "))
}

func (c *Compiler) genInterface(name string, iface *types.Interface) (err error) {
	fmt.Fprintf(c.output, "struct %s {\n", name)

	for m := iface.NumMethods(); m > 0; m-- {
		meth := iface.Method(m - 1)
		sig := meth.Type().(*types.Signature)

		err = c.genFuncProto(meth.Name(), sig, func(name, retType, params string) error {
			fmt.Fprintf(c.output, "virtual %s %s(%s) = 0;\n", retType, name, params)
			return nil
		})
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(c.output, "};\n")
	return err
}

func (c *Compiler) genIfaceForType(n *types.Named, out func(ifaces []string) error) (err error) {
	// FIXME: this is highly inneficient and won't scale at all
	var ifaces []string
	ifaceMeths := make(map[string]struct{})
	for k, v := range c.inf.Types {
		if _, ok := k.(*ast.InterfaceType); !ok {
			continue
		}

		iface := v.Type.(*types.Interface)
		if !types.Implements(n, iface) {
			continue
		}

		for _, typ := range c.inf.Defs {
			if def, ok := typ.(*types.TypeName); ok {
				if !types.IsInterface(def.Type()) {
					continue
				}
				if !types.Implements(def.Type(), iface) {
					continue
				}

				for i := 0; i < iface.NumMethods(); i++ {
					ifaceMeths[iface.Method(i).Name()] = struct{}{}
				}

				derived := fmt.Sprintf("public %s", def.Name())
				ifaces = append(ifaces, derived)
				break
			}
		}
	}

	if err = out(ifaces); err != nil {
		return err
	}

	mset := types.NewMethodSet(n)
	for i := 0; i < mset.Len(); i++ {
		sel := mset.At(i)
		switch sel.Kind() {
		default:
			return fmt.Errorf("Kind %d not supported here", sel.Kind())

		case types.MethodVal:
			f := sel.Obj().(*types.Func)
			sig := f.Type().(*types.Signature)

			err = c.genFuncProto(f.Name(), sig, func(name, retType, params string) error {
				if _, virtual := ifaceMeths[f.Name()]; virtual {
					fmt.Fprintf(c.output, "virtual %s %s(%s) override;\n", retType, name, params)
				} else {
					fmt.Fprintf(c.output, "%s %s(%s);\n", retType, name, params)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Compiler) genStruct(name string, s *types.Struct, n *types.Named) (err error) {
	fmt.Fprintf(c.output, "struct %s", name)
	defer fmt.Fprintf(c.output, "};\n")

	return c.genIfaceForType(n, func(ifaces []string) error {
		if ifaces != nil {
			fmt.Fprintf(c.output, " : %s", strings.Join(ifaces, ", "))
		}

		fmt.Fprint(c.output, " {\n")
		numFields := s.NumFields()
		for f := 0; f < numFields; f++ {
			f := s.Field(f)

			typ, err := c.toTypeSig(f.Type())
			if err != nil {
				return fmt.Errorf("Couldn't generate field: %s", err)
			}

			nilVal, err := c.toNilVal(f.Type())
			if err != nil {
				return fmt.Errorf("Couldn't determine nil value for %s: %s", name, err)
			}

			if nilVal != "" {
				fmt.Fprintf(c.output, "%s %s{%s};\n", typ, f.Name(), nilVal)
				continue
			}

			fmt.Fprintf(c.output, "%s %s;\n", typ, f.Name())
		}
		return nil
	})
}

func (c *Compiler) genBasicType(name string, b *types.Basic, n *types.Named) (err error) {
	fmt.Fprintf(c.output, "struct %s", name)
	defer fmt.Fprintf(c.output, "};\n")

	return c.genIfaceForType(n, func(ifaces []string) error {
		typ, err := c.toTypeSig(b.Underlying())
		if err != nil {
			return fmt.Errorf("Could not determine underlying type: %s", err)
		}

		nilValue, err := c.toNilVal(b.Underlying())
		if err != nil {
			return fmt.Errorf("Could not determine nil value for type %s: %s", typ, err)
		}

		base := []string{fmt.Sprintf("moku::basic<%s>", typ)}
		for _, iface := range ifaces {
			base = append(base, iface)
		}

		fmt.Fprintf(c.output, ": %s {\n", strings.Join(base, ", "))
		fmt.Fprintf(c.output, "%s() : moku::basic<%s>{%s} {}\n", name, typ, nilValue)

		return nil
	})
}

func (c *Compiler) genNamedType(name string, n *types.Named) (err error) {
	switch t := n.Underlying().(type) {
	default:
		return fmt.Errorf("What to do with the named type %v?", reflect.TypeOf(t))

	case *types.Interface:
		return c.genInterface(name, t)

	case *types.Struct:
		return c.genStruct(name, t, n)

	case *types.Basic:
		return c.genBasicType(name, t, n)
	}
}

func (c *Compiler) genPrototype(name string, sig *types.Signature) error {
	return c.genFuncProto(name, sig, func(name, retType, params string) error {
		fmt.Fprintf(c.output, "%s %s(%s);\n", retType, name, params)
		return nil
	})
}

func (c *Compiler) genVar(gen *nodeGen, v *types.Var, mainBlock bool) error {
	typ, err := c.toTypeSig(v.Type())
	if err != nil {
		return fmt.Errorf("Couldn't get type signature for variable: %s", err)
	}

	nilVal, err := c.toNilVal(v.Type())
	if err != nil {
		return fmt.Errorf("Couldn't get nil value for variable: %s", err)
	}

	if mainBlock {
		if !v.Exported() {
			fmt.Fprint(gen.out, "static ")
		}
		fmt.Fprintf(gen.out, "%s %s;\n", typ, v.Name())
	} else {
		fmt.Fprintf(gen.out, "%s %s{%s};\n", typ, v.Name(), nilVal)
	}

	return nil
}

func (c *Compiler) genConst(gen *nodeGen, k *types.Const, mainBlock bool) error {
	typ, err := c.toTypeSig(k.Type())
	if err != nil {
		return fmt.Errorf("Couldn't get type signature for variable: %s", err)
	}

	if mainBlock {
		if !k.Exported() {
			fmt.Fprint(gen.out, "static ")
		}
		fmt.Fprintf(gen.out, "constexpr %s %s{%s};\n", typ, k.Name(), k.Val())
	} else {
		fmt.Fprintf(gen.out, "constexpr %s %s{%s};\n", typ, k.Name(), k.Val())
	}

	return nil
}

func (c *Compiler) genNamespace(p *types.Package, mainBlock bool) (err error) {
	if !mainBlock {
		fmt.Fprintf(c.output, "namespace %s {\n", p.Name())
	}

	genTypeProto := func(name string, obj types.Object) error {
		switch t := obj.Type().(type) {
		default:
			return nil
		case *types.Named:
			return c.genNamedType(name, t)

		case *types.Signature:
			return c.genPrototype(name, t)
		}
	}

	s := p.Scope()
	for _, name := range s.Names() {
		obj := s.Lookup(name)

		if mainBlock && name == "main" {
			name = "_main"
		}

		switch t := obj.(type) {
		case *types.Func:
			if t.Name() == "init" {
				c.initPkgs = append(c.initPkgs, p.Name())
			}
			if err = genTypeProto(name, obj); err != nil {
				return err
			}
		case *types.TypeName:
			if err = genTypeProto(name, obj); err != nil {
				return err
			}
		case *types.Var:
			gen := nodeGen{out: c.output}
			if err = c.genVar(&gen, t, mainBlock); err != nil {
				return err
			}
		case *types.Const:
			gen := nodeGen{out: c.output}
			if err = c.genConst(&gen, t, mainBlock); err != nil {
				return err
			}
		default:
			return fmt.Errorf("Don't know how to generate: %s", reflect.TypeOf(t))
		}
	}

	if !mainBlock {
		fmt.Fprintf(c.output, "} // namespace %s\n", p.Name())
	}

	return nil
}

func (c *Compiler) genImports() (err error) {
	var genImport func(p *types.Package) error

	genImport = func(p *types.Package) error {
		for _, pkg := range p.Imports() {
			if err = genImport(pkg); err != nil {
				return err
			}
		}
		c.genNamespace(p, false)
		return nil
	}

	for _, pkg := range c.pkg.Imports() {
		if err = genImport(pkg); err != nil {
			return err
		}
	}

	return nil
}

func (c *Compiler) genMapType(m *ast.MapType) (string, error) {
	k, err := c.genExpr(m.Key)
	if err != nil {
		return "", err
	}
	v, err := c.genExpr(m.Value)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("std::map<%s, %s>", k, v), nil
}

func (c *Compiler) genCallExpr(ce *ast.CallExpr) (string, error) {
	fun, err := c.genExpr(ce.Fun)
	if err != nil {
		return "", err
	}

	var args []string
	for _, arg := range ce.Args {
		argExp, err := c.genExpr(arg)
		if err != nil {
			return "", err
		}

		args = append(args, argExp)
	}
	if ce.Ellipsis.IsValid() {
		// TODO
	}

	return fmt.Sprintf("%s(%s)", fun, strings.Join(args, ", ")), nil
}

func (c *Compiler) genBasicLit(b *ast.BasicLit) (string, error) {
	switch b.Kind {
	default:
		return "", fmt.Errorf("Unknown basic literal type: %+v", b)

	case token.INT, token.FLOAT, token.CHAR, token.STRING:
		return b.Value, nil

	case token.IMAG:
		return "", fmt.Errorf("Imaginary numbers not supported")
	}
}

func (c *Compiler) genIdent(i *ast.Ident) (string, error) {
	if this := c.recvs.Lookup(i.Name); this != nil {
		return "this", nil
	}
	if basicTyp, ok := goTypeToBasic[i.Name]; ok {
		return basicTypeToCpp[basicTyp].typ, nil
	}
	return i.Name, nil
}

func (c *Compiler) genStarExpr(s *ast.StarExpr) (string, error) {
	return c.genUnaryExpr(&ast.UnaryExpr{X: s.X, Op: token.MUL})
}

func (c *Compiler) genExpr(x ast.Expr) (string, error) {
	switch x := x.(type) {
	default:
		return "", fmt.Errorf("Couldn't generate expression with type: %s", reflect.TypeOf(x))

	case *ast.StarExpr:
		return c.genStarExpr(x)

	case *ast.FuncLit:
		return c.genFuncLit(x)

	case *ast.CompositeLit:
		return c.genCompositeLit(x)

	case *ast.BinaryExpr:
		return c.genBinaryExpr(x)

	case *ast.CallExpr:
		return c.genCallExpr(x)

	case *ast.SelectorExpr:
		return c.genSelectorExpr(x)

	case *ast.ParenExpr:
		return c.genParenExpr(x)

	case *ast.SliceExpr:
		return c.genSliceExpr(x)

	case *ast.IndexExpr:
		return c.genIndexExpr(x)

	case *ast.UnaryExpr:
		return c.genUnaryExpr(x)

	case *ast.ArrayType:
		return c.genArrayType(x)

	case *ast.MapType:
		return c.genMapType(x)

	case *ast.BasicLit:
		return c.genBasicLit(x)

	case *ast.Ident:
		return c.genIdent(x)
	}
}

func (c *Compiler) genInit() bool {
	for ident, _ := range c.inf.Defs {
		if ident.Name == "init" {
			fmt.Fprintf(c.output, "void init();\n")
			return true
		}
	}

	return false
}

func (c *Compiler) genMain() (err error) {
	hasInit := c.genInit()

	fmt.Fprintf(c.output, "int main() {\n")

	for _, pkg := range c.initPkgs {
		fmt.Fprintf(c.output, "%s::init();\n", pkg)
	}

	if hasInit {
		fmt.Fprintf(c.output, "init();\n")
	}

	for _, init := range c.inf.InitOrder {
		if len(init.Lhs) == 1 {
			fmt.Fprintf(c.output, "%s", init.Lhs[0].Name())
		} else {
			var tie []string

			for _, lhs := range init.Lhs {
				tie = append(tie, lhs.Name())
			}

			fmt.Fprintf(c.output, "std::tie(%s)", strings.Join(tie, ", "))
		}

		expr, err := c.genExpr(init.Rhs)
		if err != nil {
			return fmt.Errorf("Couldn't write initialization code: %s", err)
		}

		fmt.Fprintf(c.output, "= %s;\n", expr)
	}

	fmt.Fprintf(c.output, "_main();\n")
	fmt.Fprintf(c.output, "return 0;\n")
	fmt.Fprintf(c.output, "}\n")

	return nil
}

type nodeGen struct {
	out      io.Writer
	hasDefer bool
}

func (c *Compiler) genComment(gen *nodeGen, comment *ast.Comment) error {
	fmt.Fprintf(gen.out, "/* %s */", comment.Text)
	return nil
}

func (c *Compiler) genFuncDecl(gen *nodeGen, f *ast.FuncDecl) (err error) {
	var typ types.Object
	typ, ok := c.inf.Defs[f.Name]
	if !ok {
		return fmt.Errorf("Could not find type for func %s", f.Name.Name)
	}

	name := f.Name.Name
	if name == "main" {
		name = "_main"
	}

	fun := typ.(*types.Func)
	sig := fun.Type().(*types.Signature)
	recv := sig.Recv()
	err = c.genFuncProto(name, sig, func(name, retType, params string) (err error) {
		if recv != nil {
			var typ string
			switch t := recv.Type().(type) {
			case *types.Named:
				typ = t.Obj().Name()
			case *types.Pointer:
				if typ, err = c.toTypeSig(t.Elem()); err != nil {
					return err
				}
			}
			name = fmt.Sprintf("%s::%s", typ, name)
		}
		fmt.Fprintf(gen.out, "%s %s(%s)\n", retType, name, params)
		return nil
	})
	if err != nil {
		return err
	}

	c.recvs.Push(recv)
	defer c.recvs.Pop()

	filt := func(name string) bool {
		if recv != nil && recv.Name() == name {
			return false
		}

		parms := sig.Params()
		for p := 0; p < parms.Len(); p++ {
			if parms.At(p).Name() == name {
				return false
			}
		}

		return true
	}
	if err = c.genScopeAndBody(gen, f.Body, f.Type, true, filt); err != nil {
		return err
	}

	return err
}

func (c *Compiler) genAssignStmt(gen *nodeGen, a *ast.AssignStmt) (err error) {
	if len(a.Lhs) == 1 {
		if err = c.walk(gen, a.Lhs[0]); err != nil {
			return err
		}
	} else {
		fmt.Fprint(gen.out, "std::tie(")
		for i, e := range a.Lhs {
			if err = c.walk(gen, e); err != nil {
				return err
			}
			if i < len(a.Lhs)-1 {
				fmt.Fprint(gen.out, ", ")
			}
		}
		fmt.Fprint(gen.out, ")")
	}

	var tupleOk bool
	switch a.Tok {
	case token.ADD_ASSIGN:
		fmt.Fprint(gen.out, " += ")
	case token.SUB_ASSIGN:
		fmt.Fprint(gen.out, " -= ")
	case token.MUL_ASSIGN:
		fmt.Fprint(gen.out, " *= ")
	case token.QUO_ASSIGN:
		fmt.Fprint(gen.out, " *= ")
	case token.REM_ASSIGN:
		fmt.Fprint(gen.out, " %= ")
	case token.AND_ASSIGN:
		fmt.Fprint(gen.out, " &= ")
	case token.OR_ASSIGN:
		fmt.Fprint(gen.out, " |= ")
	case token.XOR_ASSIGN:
		fmt.Fprint(gen.out, " ^= ")
	case token.SHL_ASSIGN:
		fmt.Fprint(gen.out, " <<= ")
	case token.SHR_ASSIGN:
		fmt.Fprint(gen.out, " >>= ")
	case token.AND_NOT_ASSIGN:
		fmt.Fprint(gen.out, " &= ~(")
		defer fmt.Fprint(gen.out, ")")
	case token.ASSIGN, token.DEFINE:
		fmt.Fprint(gen.out, " = ")
		tupleOk = true
	default:
		return fmt.Errorf("Unknown assignment token")
	}

	if len(a.Rhs) == 1 {
		return c.walk(gen, a.Rhs[0])
	}

	if !tupleOk {
		return fmt.Errorf("Rhs incompatible with Lhs")
	}

	var types []string
	for _, e := range a.Rhs {
		typ, ok := c.inf.Types[e]
		if !ok {
			return fmt.Errorf("Couldn't determine type of expression")
		}

		ctyp, err := c.toTypeSig(typ.Type)
		if err != nil {
			return fmt.Errorf("Couldn't get type signature: %s", err)
		}

		types = append(types, ctyp)
	}

	fmt.Fprintf(gen.out, "std::tuple<%s>(", strings.Join(types, ", "))
	for i, e := range a.Rhs {
		if err = c.walk(gen, e); err != nil {
			return err
		}
		if i < len(a.Rhs)-1 {
			fmt.Fprint(gen.out, ", ")
		}
	}
	fmt.Fprint(gen.out, ")")

	return nil
}

func (c *Compiler) genSelectorExpr(s *ast.SelectorExpr) (string, error) {
	var obj types.Object
	obj, ok := c.inf.Uses[s.Sel]
	if !ok {
		return "", fmt.Errorf("Sel not found for X: %s", s)
	}

	switch t := s.X.(type) {
	default:
		return "", fmt.Errorf("Unknown type for left-side of selector: %s", reflect.TypeOf(t))

	case *ast.CallExpr:
		lhs, err := c.genCallExpr(t)
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.%s", lhs, s.Sel.Name), nil

	case *ast.SelectorExpr:
		lhs, err := c.genSelectorExpr(t)
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.%s", lhs, s.Sel.Name), nil

	case *ast.Ident:
		if pkg := obj.Pkg(); pkg != nil && pkg.Name() == t.Name {
			return fmt.Sprintf("%s::%s", pkg.Name(), s.Sel.Name), nil
		}
		if this := c.recvs.Lookup(t.Name); this != nil {
			return fmt.Sprintf("this->%s", s.Sel.Name), nil
		}
		return fmt.Sprintf("%s.%s", t.Name, s.Sel.Name), nil
	}
}

func (c *Compiler) genForStmt(gen *nodeGen, f *ast.ForStmt) (err error) {
	scope, ok := c.inf.Scopes[f]
	if !ok {
		return fmt.Errorf("Could not find scope")
	}

	if len(scope.Names()) > 0 {
		fmt.Fprintf(gen.out, "{")
		defer fmt.Fprintf(gen.out, "}")
	}

	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		v := obj.(*types.Var)
		if err = c.genVar(gen, v, false); err != nil {
			return err
		}
	}

	fmt.Fprintf(gen.out, "for (")

	if f.Init != nil {
		if err = c.walk(gen, f.Init); err != nil {
			return err
		}
	}

	fmt.Fprintf(gen.out, "; ")
	if f.Cond != nil {
		if err = c.walk(gen, f.Cond); err != nil {
			return err
		}
	}

	fmt.Fprintf(gen.out, "; ")
	if f.Post != nil {
		if err = c.walk(gen, f.Post); err != nil {
			return err
		}
	}

	fmt.Fprintf(gen.out, ")")

	filt := func(name string) bool { return true }
	if err = c.genScopeAndBody(gen, f.Body, f.Body, true, filt); err != nil {
		return err
	}

	return nil
}

func (c *Compiler) genBlockStmt(gen *nodeGen, blk *ast.BlockStmt) (err error) {
	for _, stmt := range blk.List {
		if err = c.walk(gen, stmt); err != nil {
			return err
		}
		switch stmt.(type) {
		default:
			fmt.Fprintln(gen.out, ";")

		case *ast.ForStmt, *ast.DeclStmt, *ast.IfStmt, *ast.RangeStmt:
		}
	}
	return nil
}

func (c *Compiler) genScopeAndBody(gen *nodeGen, block *ast.BlockStmt, scope ast.Node, newScope bool, filter func(name string) bool) (err error) {
	if newScope {
		fmt.Fprint(gen.out, "{")
		defer fmt.Fprintln(gen.out, "}")
	}

	blockGen := nodeGen{out: new(bytes.Buffer)}
	if err = c.genBlockStmt(&blockGen, block); err != nil {
		return err
	}

	varGen := nodeGen{out: new(bytes.Buffer), hasDefer: blockGen.hasDefer}
	if err = c.genScopeVars(&varGen, scope, filter); err != nil {
		return err
	}

	fmt.Fprintln(gen.out, varGen.out.(*bytes.Buffer).String())
	fmt.Fprintln(gen.out, blockGen.out.(*bytes.Buffer).String())

	return nil
}

func (c *Compiler) genScopeVars(gen *nodeGen, node ast.Node, filter func(name string) bool) (err error) {
	if _, ok := node.(*ast.FuncType); ok && gen.hasDefer {
		fmt.Fprintf(c.output, "moku::defer _defer_;\n")
	}

	if scope, ok := c.inf.Scopes[node]; ok {
		for _, name := range scope.Names() {
			if !filter(name) {
				continue
			}
			switch ref := scope.Lookup(name).(type) {
			case *types.Var:
				if err = c.genVar(gen, ref, false); err != nil {
					return err
				}
			case *types.Const:
				if err = c.genConst(gen, ref, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Compiler) genExprStmt(gen *nodeGen, e *ast.ExprStmt) error {
	return c.walk(gen, e.X)
}

func (c *Compiler) genBinaryExpr(b *ast.BinaryExpr) (s string, err error) {
	x, err := c.genExpr(b.X)
	if err != nil {
		return "", err
	}
	y, err := c.genExpr(b.Y)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s %s", x, b.Op, y), nil
}

func (c *Compiler) genField(gen *nodeGen, f *ast.Field) error {
	fmt.Fprintf(gen.out, "// field %+v\n", f)
	return nil
}

func (c *Compiler) genReturnStmt(gen *nodeGen, r *ast.ReturnStmt) (err error) {
	fmt.Fprintf(gen.out, "return ")

	if len(r.Results) == 1 {
		return c.walk(gen, r.Results[0])
	}

	fmt.Fprintf(gen.out, "{")
	for i, e := range r.Results {
		if err = c.walk(gen, e); err != nil {
			return err
		}

		if i != len(r.Results)-1 {
			fmt.Fprint(gen.out, ", ")
		}
	}
	fmt.Fprintf(gen.out, "}")

	return nil
}

func (c *Compiler) genCompositeLit(cl *ast.CompositeLit) (str string, err error) {
	var typ string
	if cl.Type != nil {
		if typ, err = c.genExpr(cl.Type); err != nil {
			return "", err
		}
	}

	var elts []string
	for _, e := range cl.Elts {
		if elt, err := c.genExpr(e); err == nil {
			elts = append(elts, elt)
			continue
		}
		return "", err
	}

	return fmt.Sprintf("%s{%s}", typ, strings.Join(elts, ", ")), nil
}

func (c *Compiler) genParenExpr(p *ast.ParenExpr) (s string, err error) {
	if expr, err := c.genExpr(p.X); err == nil {
		return fmt.Sprintf("(%s)", expr), nil
	}
	return "", err
}

func (c *Compiler) genIncDecStmt(gen *nodeGen, p *ast.IncDecStmt) (err error) {
	if err = c.walk(gen, p.X); err != nil {
		return err
	}

	switch p.Tok {
	default:
		return fmt.Errorf("Unknown inc/dec token")

	case token.INC:
		fmt.Fprintf(gen.out, "++")

	case token.DEC:
		fmt.Fprintf(gen.out, "--")
	}

	return nil
}

func (c *Compiler) genCommentGroup(gen *nodeGen, g *ast.CommentGroup) (err error) {
	for _, comment := range g.List {
		if err = c.walk(gen, comment); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) genLabeledStmt(gen *nodeGen, l *ast.LabeledStmt) (err error) {
	if err = c.walk(gen, l.Label); err != nil {
		return err
	}
	fmt.Fprintf(gen.out, ":\n")
	return nil
}

func (c *Compiler) genBranchStmt(gen *nodeGen, b *ast.BranchStmt) (err error) {
	switch b.Tok {
	case token.GOTO:
		if b.Label == nil {
			return fmt.Errorf("Goto without label")
		}
		fmt.Fprintf(gen.out, "goto ")
		if err = c.walk(gen, b.Label); err != nil {
			return err
		}
	case token.BREAK:
		if b.Label != nil {
			return fmt.Errorf("Break with labels not supported yet")
		}
		fmt.Fprintf(gen.out, "break")
	case token.CONTINUE:
		if b.Label != nil {
			return fmt.Errorf("Continue with labels not supported yet")
		}
		fmt.Fprintf(gen.out, "continue")
	case token.FALLTHROUGH:
		return fmt.Errorf("Fallthrough not supported yet")
	}
	return nil
}

func (c *Compiler) genArrayType(a *ast.ArrayType) (s string, err error) {
	typ, err := c.genExpr(a.Elt)
	if err != nil {
		return "", err
	}

	if a.Len == nil {
		return fmt.Sprintf("moku::slice<%s>", typ), nil
	}

	return fmt.Sprintf("std::vector<%s>", typ), nil
}

func (c *Compiler) genIndexExpr(i *ast.IndexExpr) (s string, err error) {
	expr, err := c.genExpr(i.X)
	if err != nil {
		return "", nil
	}

	index, err := c.genExpr(i.Index)
	if err != nil {
		return "", nil
	}

	return fmt.Sprintf("%s[%s]", expr, index), nil
}

func (c *Compiler) genDeferStmt(gen *nodeGen, d *ast.DeferStmt) (err error) {
	fmt.Fprintf(gen.out, "_defer_.Push([=]() mutable {")

	if err = c.walk(gen, d.Call); err != nil {
		return err
	}

	fmt.Fprintf(gen.out, "; })")

	gen.hasDefer = true

	return nil
}

func (c *Compiler) genSliceExpr(s *ast.SliceExpr) (str string, err error) {
	var args []string

	arg, err := c.genExpr(s.X)
	if err != nil {
		return "", err
	}
	args = append(args, arg)

	if s.Low != nil {
		arg, err := c.genExpr(s.Low)
		if err != nil {
			return "", err
		}
		args = append(args, arg)
	}

	if s.High != nil {
		arg, err := c.genExpr(s.High)
		if err != nil {
			return "", err
		}
		args = append(args, arg)
	}

	if s.Max != nil {
		arg, err := c.genExpr(s.Max)
		if err != nil {
			return "", err
		}
		args = append(args, arg)
	}

	typ, ok := c.inf.Types[s.X]
	if !ok {
		return "", fmt.Errorf("Couldn't determine type of expression")
	}
	ctyp, err := c.toTypeSig(typ.Type)
	if err != nil {
		return "", fmt.Errorf("Couldn't get type signature: %s", err)
	}

	return fmt.Sprintf("moku::slice_expr<%s>(%s)", ctyp, strings.Join(args, ", ")), nil
}

func (c *Compiler) genIfStmt(gen *nodeGen, i *ast.IfStmt) (err error) {
	if i.Init != nil {
		fmt.Fprint(gen.out, "{")
		defer fmt.Fprint(gen.out, "}")

		blk := ast.BlockStmt{List: []ast.Stmt{i.Init}}
		filt := func(name string) bool { return true }
		if err = c.genScopeAndBody(gen, &blk, i, false, filt); err != nil {
			return err
		}
	}

	fmt.Fprintf(gen.out, "if (")
	if err = c.walk(gen, i.Cond); err != nil {
		return err
	}
	fmt.Fprintf(gen.out, ") {")
	if err = c.genBlockStmt(gen, i.Body); err != nil {
		return err
	}

	if i.Else != nil {
		fmt.Fprintf(gen.out, "} else {")
		if err = c.walk(gen, i.Else); err != nil {
			return err
		}
	}
	fmt.Fprintf(gen.out, "}")
	return nil
}

func (c *Compiler) genRangeStmt(gen *nodeGen, r *ast.RangeStmt) (err error) {
	getRangeFunc := func() (string, string) {
		var keyIdent, valIdent string

		switch k := r.Key.(type) {
		case *ast.Ident:
			keyIdent = k.Name
		default:
			keyIdent = "_"
		}
		switch v := r.Value.(type) {
		case *ast.Ident:
			valIdent = v.Name
		default:
			valIdent = "_"
		}

		switch {
		case keyIdent == "_" && valIdent == "_":
			return fmt.Sprintf("auto %s", c.newIdent()), "moku::range_void"
		case keyIdent == "_":
			return valIdent, "moku::range_value"
		case valIdent == "_":
			return keyIdent, "moku::range_key"
		default:
			return fmt.Sprintf("std::tie(%s, %s)", keyIdent, valIdent), "moku::range_key_value"
		}
	}

	typ, ok := c.inf.Types[r.X]
	if !ok {
		return fmt.Errorf("Couldn't determine type of range expression")
	}
	ctyp, err := c.toTypeSig(typ.Type)
	if err != nil {
		return fmt.Errorf("Couldn't get type signature: %s", err)
	}
	rangeExp, err := c.genExpr(r.X)
	if err != nil {
		return fmt.Errorf("Couldn't convert expression to string: %s", err)
	}

	if r.Tok == token.DEFINE {
		fmt.Fprintf(gen.out, "{")
		defer fmt.Fprintf(gen.out, "}")

		filt := func(n string) bool { return true }
		if err = c.genScopeVars(gen, r, filt); err != nil {
			return err
		}
	}

	lhs, rangeFunc := getRangeFunc()
	fmt.Fprintf(gen.out, "for (%s : %s<%s>(%s)) {", lhs, rangeFunc, ctyp, rangeExp)

	if err = c.genBlockStmt(gen, r.Body); err != nil {
		return fmt.Errorf("Couldn't create range for body: %s", err)
	}

	fmt.Fprintf(gen.out, "}\n")

	return nil
}

func (c *Compiler) genUnaryExpr(u *ast.UnaryExpr) (s string, err error) {
	if expr, err := c.genExpr(u.X); err == nil {
		return fmt.Sprintf("%s%s", u.Op, expr), nil
	}
	return "", err
}

func (c *Compiler) genFuncLit(f *ast.FuncLit) (str string, err error) {
	typ, ok := c.inf.Types[f]
	if !ok {
		return "", fmt.Errorf("Couldn't find function literal scope")
	}

	litGen := nodeGen{out: new(bytes.Buffer)}

	out := func(_, retType, params string) error {
		fmt.Fprintf(litGen.out, "[=](%s) mutable -> %s", params, retType)
		return nil
	}
	if err = c.genFuncProto("", typ.Type.(*types.Signature), out); err != nil {
		return "", err
	}

	fmt.Fprint(litGen.out, "{")
	if err = c.genBlockStmt(&litGen, f.Body); err != nil {
		return "", err
	}
	fmt.Fprint(litGen.out, "}")

	return litGen.out.(*bytes.Buffer).String(), nil
}

func (c *Compiler) walk(gen *nodeGen, node ast.Node) error {
	switch n := node.(type) {
	default:
		return fmt.Errorf("Unknown node type: %s\n", reflect.TypeOf(n))

	case ast.Expr:
		out, err := c.genExpr(n)
		if err != nil {
			return err
		}

		fmt.Fprint(gen.out, out)
		return nil

	case *ast.BlockStmt:
		return c.genBlockStmt(gen, n)

	case *ast.RangeStmt:
		return c.genRangeStmt(gen, n)

	case *ast.IfStmt:
		return c.genIfStmt(gen, n)

	case *ast.DeferStmt:
		return c.genDeferStmt(gen, n)

	case *ast.IncDecStmt:
		return c.genIncDecStmt(gen, n)

	case *ast.Comment:
		return c.genComment(gen, n)

	case *ast.CommentGroup:
		return c.genCommentGroup(gen, n)

	case *ast.FuncDecl:
		return c.genFuncDecl(gen, n)

	case *ast.AssignStmt:
		return c.genAssignStmt(gen, n)

	case *ast.ForStmt:
		return c.genForStmt(gen, n)

	case *ast.ExprStmt:
		return c.genExprStmt(gen, n)

	case *ast.Field:
		return c.genField(gen, n)

	case *ast.ReturnStmt:
		return c.genReturnStmt(gen, n)

	case *ast.LabeledStmt:
		return c.genLabeledStmt(gen, n)

	case *ast.BranchStmt:
		return c.genBranchStmt(gen, n)

	case *ast.GenDecl, *ast.DeclStmt:
		return nil
	}
}

func (c *Compiler) Compile() (err error) {
	if err = c.genImports(); err != nil {
		return err
	}
	if err = c.genNamespace(c.pkg, true); err != nil {
		return err
	}
	if err = c.genMain(); err != nil {
		return err
	}

	gen := nodeGen{out: c.output}
	for _, decl := range c.ast.Decls {
		if err = c.walk(&gen, ast.Node(decl)); err != nil {
			return err
		}
	}

	return nil
}

func (c *Compiler) DebugTypeSystem() {
	fmt.Println("----- defs ---------------------------")
	for k, v := range c.inf.Defs {
		fmt.Fprintf(c.output, "k<%s>=%+v\n", reflect.TypeOf(k), k)
		fmt.Fprintf(c.output, "v<%s>=%+v\n\n", reflect.TypeOf(v), v)
	}

	fmt.Println("----- types ---------------------------")
	for k, v := range c.inf.Types {
		fmt.Fprintf(c.output, "k<%s>=%+v\n", reflect.TypeOf(k), k)
		fmt.Fprintf(c.output, "v<%s>=%+v\n\n", reflect.TypeOf(v), v)
	}

	fmt.Println("----- uses ---------------------------")
	for k, v := range c.inf.Uses {
		fmt.Fprintf(c.output, "k<%s>=%+v\n", reflect.TypeOf(k), k)
		fmt.Fprintf(c.output, "v<%s>=%+v\n\n", reflect.TypeOf(v), v)
	}

	fmt.Println("----- scopes ---------------------------")
	for k, v := range c.inf.Scopes {
		fmt.Fprintf(c.output, "k<%s>=%+v\n", reflect.TypeOf(k), k)
		fmt.Fprintf(c.output, "v<%s>=%+v\n\n", reflect.TypeOf(v), v)
	}

	fmt.Println("----- initialization -------------------")
	for _, i := range c.inf.InitOrder {
		fmt.Println(i)
	}

	fmt.Println("----- implicits ---------------------------")
	for k, v := range c.inf.Implicits {
		fmt.Fprintf(c.output, "k<%s>=%+v\n", reflect.TypeOf(k), k)
		fmt.Fprintf(c.output, "v<%s>=%+v\n\n", reflect.TypeOf(v), v)
	}

	fmt.Println("----- selections ---------------------------")
	for k, v := range c.inf.Selections {
		fmt.Fprintf(c.output, "k<%s>=%+v\n", reflect.TypeOf(k), k)
		fmt.Fprintf(c.output, "v<%s>=%+v\n\n", reflect.TypeOf(v), v)
	}
}
