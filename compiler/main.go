// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compiler

import (
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

	input io.Reader
	output io.Writer
}

func NewCompiler(in io.Reader, out io.Writer) (*Compiler, error) {
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
		input: in,
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

	if types.IsInterface(t) {
		sig = fmt.Sprintf("std::shared_ptr<%s>", sig)
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

		case *types.Slice, *types.Chan, *types.Interface, *types.Named:
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

func (c *Compiler) genFuncProto(name string, sig *types.Signature, out func(name, retType, params string)) (err error) {
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

		retType = fmt.Sprintf("std::pair<%s>", strings.Join(mult, ", "))
	}

	out(name, retType, strings.Join(params, ", "))

	return nil
}

func (c *Compiler) genInterface(name string, iface *types.Interface) (err error) {
	fmt.Fprintf(c.output, "struct %s {\n", name)

	for m := iface.NumMethods(); m > 0; m-- {
		meth := iface.Method(m - 1)
		sig := meth.Type().(*types.Signature)

		err = c.genFuncProto(meth.Name(), sig, func(name, retType, params string) {
			fmt.Fprintf(c.output, "virtual %s %s(%s) = 0;\n", retType, name, params)
		})
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(c.output, "};\n")
	return err
}

func (c *Compiler) genStruct(name string, s *types.Struct, n *types.Named) (err error) {
	fmt.Fprintf(c.output, "struct %s", name)

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

	mset := types.NewMethodSet(n)
	for i := 0; i < mset.Len(); i++ {
		sel := mset.At(i)
		switch sel.Kind() {
		default:
			return fmt.Errorf("Kind %d not supported here", sel.Kind())

		case types.MethodVal:
			f := sel.Obj().(*types.Func)
			sig := f.Type().(*types.Signature)

			err = c.genFuncProto(f.Name(), sig, func(name, retType, params string) {
				if _, virtual := ifaceMeths[f.Name()]; virtual {
					fmt.Fprintf(c.output, "virtual %s %s(%s) override;\n", retType, name, params)
				} else {
					fmt.Fprintf(c.output, "%s %s(%s);\n", retType, name, params)
				}
			})
			if err != nil {
				return err
			}
		}
	}

	fmt.Fprintf(c.output, "};\n")

	return nil
}

func (c *Compiler) genBasicType(name string, b *types.Basic) (err error) {
	fmt.Fprintf(c.output, "struct %s {\n", name)

	typ, err := c.toTypeSig(b.Underlying())
	if err != nil {
		return fmt.Errorf("Could not determine underlying type: %s", err)
	}

	nilValue, err := c.toNilVal(b.Underlying())
	if err != nil {
		return fmt.Errorf("Could not determine nil value for type %s: %s", typ, err)
	}

	fmt.Fprintf(c.output, "%s Value{%s};\n", typ, nilValue)
	fmt.Fprintf(c.output, "%s() { return Value; }\n", typ)
	fmt.Fprintf(c.output, "%s(%s v) : Value{v} {}\n", name, typ)
	fmt.Fprintf(c.output, "%s operator=(%s v) { return Value = v; }\n", typ, typ)
	fmt.Fprintf(c.output, "};\n")

	return nil
}

func (c *Compiler) genNamedType(name string, n *types.Named) (err error) {
	switch t := n.Underlying().(type) {
	case *types.Interface:
		if err = c.genInterface(name, t); err != nil {
			return err
		}
	case *types.Struct:
		if err = c.genStruct(name, t, n); err != nil {
			return err
		}
	case *types.Basic:
		if err = c.genBasicType(name, t); err != nil {
			return err
		}
	default:
		return fmt.Errorf("What to do with the named type %v?", reflect.TypeOf(t))
	}

	return nil
}

func (c *Compiler) genPrototype(name string, sig *types.Signature) error {
	return c.genFuncProto(name, sig, func(name, retType, params string) {
		fmt.Fprintf(c.output, "%s %s(%s);\n", retType, name, params)
	})
}

func (c *Compiler) genVar(v *types.Var, mainBlock bool) error {
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
			fmt.Fprint(c.output, "static ")
		}
		fmt.Fprintf(c.output, "%s %s;\n", typ, v.Name())
	} else {
		fmt.Fprintf(c.output, "%s %s{%s};\n", typ, v.Name(), nilVal)
	}

	return nil
}

func (c *Compiler) genConst(k *types.Const, mainBlock bool) error {
	typ, err := c.toTypeSig(k.Type())
	if err != nil {
		return fmt.Errorf("Couldn't get type signature for variable: %s", err)
	}

	if mainBlock {
		if !k.Exported() {
			fmt.Fprint(c.output, "static ")
		}
		fmt.Fprintf(c.output, "constexpr %s %s{%s};\n", typ, k.Name(), k.Val())
	} else {
		fmt.Fprintf(c.output, "constexpr %s %s{%s};\n", typ, k.Name(), k.Val())
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
			if err = c.genVar(t, mainBlock); err != nil {
				return err
			}
		case *types.Const:
			if err = c.genConst(t, mainBlock); err != nil {
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

func (c *Compiler) genExpr(x ast.Expr) (string, error) {
	switch x := x.(type) {
	default:
		return "", fmt.Errorf("Couldn't generate expression with type: %s", reflect.TypeOf(x))

	case *ast.CallExpr:
		fun, err := c.genExpr(x.Fun)
		if err != nil {
			return "", err
		}

		var args []string
		for _, arg := range x.Args {
			argExp, err := c.genExpr(arg)
			if err != nil {
				return "", err
			}

			args = append(args, argExp)
		}
		if x.Ellipsis.IsValid() {
			// TODO
		}

		return fmt.Sprintf("%s(%s)", fun, strings.Join(args, ", ")), nil

	case *ast.SelectorExpr:
		var obj types.Object
		obj, ok := c.inf.Uses[x.Sel]
		if !ok {
			return "", fmt.Errorf("Sel not found for X: %s", x)
		}

		if pkg := obj.Pkg(); pkg != nil {
			return fmt.Sprintf("%s::%s", pkg.Name(), x.Sel.Name), nil
		}

		return "", fmt.Errorf("Universe/label")

	case *ast.BasicLit:
		// TODO might need some conversion
		return x.Value, nil

	case *ast.Ident:
		return x.Name, nil
	}
	return "", nil
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

func (c *Compiler) genComment(comment *ast.Comment) error {
	fmt.Fprintf(c.output, "/* %s */", comment.Text)
	return nil
}

func (c *Compiler) genFuncDecl(f *ast.FuncDecl) (err error) {
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
	err = c.genFuncProto(name, sig, func(name, retType, params string) {
		if recv != nil {
			typ := recv.Type().(*types.Named).Obj().Name()
			name = fmt.Sprintf("%s::%s", typ, name)
		}
		fmt.Fprintf(c.output, "%s %s(%s)\n", retType, name, params)
	})
	if err != nil {
		return err
	}

	fmt.Println("{")

	err = c.genScopeVars(f.Type, func(name string) bool {
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
	})
	if err != nil {
		return err
	}

	if err = c.genBlockStmt(f.Body); err != nil {
		return err
	}

	fmt.Println("}")

	return err
}

func (c *Compiler) genAssignStmt(a *ast.AssignStmt) (err error) {
	if len(a.Lhs) == 1 {
		if err = c.walk(a.Lhs[0]); err != nil {
			return err
		}
	} else {
		fmt.Fprint(c.output, "std::tie(")
		for i, e := range a.Lhs {
			if err = c.walk(e); err != nil {
				return err
			}
			if i < len(a.Lhs)-1 {
				fmt.Fprint(c.output, ", ")
			}
		}
		fmt.Fprint(c.output, ")")
	}

	switch a.Tok {
	case token.ADD_ASSIGN:
		fmt.Fprint(c.output, " += ")
	case token.SUB_ASSIGN:
		fmt.Fprint(c.output, " -= ")
	case token.MUL_ASSIGN:
		fmt.Fprint(c.output, " *= ")
	case token.QUO_ASSIGN:
		fmt.Fprint(c.output, " *= ")
	case token.REM_ASSIGN:
		fmt.Fprint(c.output, " %= ")
	case token.AND_ASSIGN:
		fmt.Fprint(c.output, " &= ")
	case token.OR_ASSIGN:
		fmt.Fprint(c.output, " |= ")
	case token.XOR_ASSIGN:
		fmt.Fprint(c.output, " ^= ")
	case token.SHL_ASSIGN:
		fmt.Fprint(c.output, " <<= ")
	case token.SHR_ASSIGN:
		fmt.Fprint(c.output, " >>= ")
	case token.AND_NOT_ASSIGN:
		fmt.Fprint(c.output, " &= ~(")
	case token.ASSIGN, token.DEFINE:
		fmt.Fprint(c.output, " = ")
	default:
		return fmt.Errorf("Unknown assignment token")
	}

	for _, e := range a.Rhs {
		if err = c.walk(e); err != nil {
			return err
		}
	}

	if a.Tok == token.AND_NOT_ASSIGN {
		fmt.Fprint(c.output, ")")
	}

	return nil
}

func (c *Compiler) genIdent(i *ast.Ident) error {
	fmt.Fprint(c.output, i.Name)
	return nil
}

func (c *Compiler) genCallExpr(call *ast.CallExpr) (err error) {
	if err = c.walk(call.Fun); err != nil {
		return err
	}

	fmt.Fprintf(c.output, "(")
	for i, arg := range call.Args {
		if err = c.walk(arg); err != nil {
			return err
		}
		if i != len(call.Args)-1 {
			fmt.Fprintf(c.output, ", ")
		}
	}
	fmt.Fprintf(c.output, ")")

	return nil
}

func (c *Compiler) genSelectorExpr(s *ast.SelectorExpr) error {
	var obj types.Object
	obj, ok := c.inf.Uses[s.Sel]
	if !ok {
		return fmt.Errorf("Sel not found for X: %s", s)
	}

	if pkg := obj.Pkg(); pkg != nil {
		fmt.Fprintf(c.output, "%s::%s", pkg.Name(), s.Sel.Name)
	}

	return nil
}

func (c *Compiler) genBasicLit(b *ast.BasicLit) error {
	switch b.Kind {
	default:
		return fmt.Errorf("Unknown basic literal type: %s", b)

	case token.INT, token.FLOAT, token.CHAR, token.STRING:
		fmt.Fprintf(c.output, "%s", b.Value)
		return nil

	case token.IMAG:
		return fmt.Errorf("Imaginary numbers not supported")
	}
}

func (c *Compiler) genForStmt(f *ast.ForStmt) (err error) {
	fmt.Fprintf(c.output, "{")

	scope, ok := c.inf.Scopes[f]
	if !ok {
		return fmt.Errorf("Could not find scope")
	}
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		v := obj.(*types.Var)
		if err = c.genVar(v, false); err != nil {
			return err
		}
	}

	fmt.Fprintf(c.output, "for (")

	if f.Init != nil {
		if err = c.walk(f.Init); err != nil {
			return err
		}
	}

	fmt.Fprintf(c.output, "; ")
	if f.Cond != nil {
		if err = c.walk(f.Cond); err != nil {
			return err
		}
	}

	fmt.Fprintf(c.output, "; ")
	if f.Post != nil {
		if err = c.walk(f.Post); err != nil {
			return err
		}
	}

	fmt.Fprintf(c.output, ") {")
	if err = c.genScopeVars(f.Body, noNameFilter); err != nil {
		return err
	}
	if err = c.genBlockStmt(f.Body); err != nil {
		return err
	}
	fmt.Fprintf(c.output, "}")

	fmt.Fprintf(c.output, "}")

	return nil
}

func (c *Compiler) genBlockStmt(blk *ast.BlockStmt) (err error) {
	for _, stmt := range blk.List {
		if err = c.walk(stmt); err != nil {
			return err
		}
		switch stmt.(type) {
		default:
			fmt.Println(";")

		case *ast.ForStmt:
		}
	}
	return nil
}

func noNameFilter(name string) bool { return true }

func (c *Compiler) genScopeVars(node ast.Node, filter func(name string) bool) (err error) {
	if scope, ok := c.inf.Scopes[node]; ok {
		for _, name := range scope.Names() {
			if !filter(name) {
				continue
			}
			if err = c.genVar(scope.Lookup(name).(*types.Var), false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Compiler) genExprStmt(e *ast.ExprStmt) error {
	return c.walk(e.X)
}

func (c *Compiler) genBinaryExpr(b *ast.BinaryExpr) (err error) {
	if err = c.walk(b.X); err != nil {
		return err
	}
	fmt.Fprintf(c.output, " %s ", b.Op)
	return c.walk(b.Y)
}

func (c *Compiler) genDeclStmt(d *ast.DeclStmt) error {
	fmt.Fprintf(c.output, "// declstmt %s\n", d)
	return nil
}

func (c *Compiler) genField(f *ast.Field) error {
	fmt.Fprintf(c.output, "// field %s\n", f)
	return nil
}

func (c *Compiler) genReturnStmt(r *ast.ReturnStmt) (err error) {
	fmt.Fprintf(c.output, "return ")

	if len(r.Results) == 1 {
		return c.walk(r.Results[0])
	}

	var types []string
	for _, e := range r.Results {
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

	fmt.Fprintf(c.output, "std::pair<%s>(", strings.Join(types, ", "))
	for i, e := range r.Results {
		if err = c.walk(e); err != nil {
			return err
		}

		if i != len(r.Results)-1 {
			fmt.Fprint(c.output, ", ")
		}
	}
	fmt.Fprintf(c.output, ")")

	return nil
}

func (c *Compiler) genCompositeLit(cl *ast.CompositeLit) (err error) {
	if cl.Type != nil {
		if err = c.walk(cl.Type); err != nil {
			return err
		}
	}

	fmt.Fprintf(c.output, "{")
	for i, elt := range cl.Elts {
		if err = c.walk(elt); err != nil {
			return err
		}
		if i < len(cl.Elts)-1 {
			fmt.Fprint(c.output, ", ")
		}
	}
	fmt.Fprintf(c.output, "}")
	return nil
}

func (c *Compiler) genParenExpr(p *ast.ParenExpr) (err error) {
	fmt.Fprint(c.output, "(")
	if err = c.walk(p.X); err != nil {
		return err
	}
	fmt.Fprint(c.output, ")")
	return nil
}

func (c *Compiler) genIncDecStmt(p *ast.IncDecStmt) (err error) {
	if err = c.walk(p.X); err != nil {
		return err
	}

	switch p.Tok {
	default:
		return fmt.Errorf("Unknown inc/dec token")

	case token.INC:
		fmt.Fprintf(c.output, "++")

	case token.DEC:
		fmt.Fprintf(c.output, "--")
	}

	return nil
}

func (c *Compiler) genCommentGroup(g *ast.CommentGroup) (err error) {
	for _, comment := range g.List {
		if err = c.walk(comment); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) walk(node ast.Node) error {
	switch n := node.(type) {
	default:
		return fmt.Errorf("Unknown node type: %s\n", reflect.TypeOf(n))

	case *ast.IncDecStmt:
		return c.genIncDecStmt(n)

	case *ast.ParenExpr:
		return c.genParenExpr(n)

	case *ast.Comment:
		return c.genComment(n)

	case *ast.CommentGroup:
		return c.genCommentGroup(n)

	case *ast.FuncDecl:
		return c.genFuncDecl(n)

	case *ast.AssignStmt:
		return c.genAssignStmt(n)

	case *ast.Ident:
		return c.genIdent(n)

	case *ast.CallExpr:
		return c.genCallExpr(n)

	case *ast.SelectorExpr:
		return c.genSelectorExpr(n)

	case *ast.BasicLit:
		return c.genBasicLit(n)

	case *ast.ForStmt:
		return c.genForStmt(n)

	case *ast.ExprStmt:
		return c.genExprStmt(n)

	case *ast.BinaryExpr:
		return c.genBinaryExpr(n)

	case *ast.DeclStmt:
		return c.genDeclStmt(n)

	case *ast.Field:
		return c.genField(n)

	case *ast.ReturnStmt:
		return c.genReturnStmt(n)

	case *ast.CompositeLit:
		return c.genCompositeLit(n)

	case *ast.GenDecl:
		return nil
	}
}

func (c *Compiler) gen() (err error) {
	for _, decl := range c.ast.Decls {
		err = c.walk(ast.Node(decl))
		if err != nil {
			return err
		}
	}
	return nil
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
	return c.gen()
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