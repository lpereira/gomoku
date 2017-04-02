// Copyright 2017 Leandro A. F. Pereira. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"log"
	"reflect"
	"strings"
)

const input = `
// Copyright © 2016 Alan A. A. Donovan & Brian W. Kernighan.
// License: https://creativecommons.org/licenses/by-nc-sa/4.0/

// See page 58.
//!+

// Surface computes an SVG rendering of a 3-D surface function.
package main

import (
	"fmt"
	"math"
)

const (
	width, height = 600, 320            // canvas size in pixels
	cells         = 100                 // number of grid cells
	xyrange       = 30.0                // axis ranges (-xyrange..+xyrange)
	xyscale       = width / 2 / xyrange // pixels per x or y unit
	zscale        = height * 0.4        // pixels per z unit
	angle         = math.Pi / 6         // angle of x, y axes (=30°)
)

var sin30, cos30 = math.Sin(angle), math.Cos(angle) // sin(30°), cos(30°)

func main() {
	fmt.Printf("<svg xmlns='http://www.w3.org/2000/svg' "+
		"style='stroke: grey; fill: white; stroke-width: 0.7' "+
		"width='%d' height='%d'>", width, height)
	for i := 0; i < cells; i++ {
		for j := 0; j < cells; j++ {
			ax, ay := corner(i+1, j)
			bx, by := corner(i, j)
			cx, cy := corner(i, j+1)
			dx, dy := corner(i+1, j+1)
			fmt.Printf("<polygon points='%g,%g %g,%g %g,%g %g,%g'/>\n",
				ax, ay, bx, by, cx, cy, dx, dy)
		}
	}
	fmt.Println("</svg>")
}

func corner(i, j int) (float64, float64) {
	// Find point (x,y) at corner of cell (i,j).
	x := xyrange * (float64(i)/cells - 0.5)
	y := xyrange * (float64(j)/cells - 0.5)

	// Compute surface height z.
	z := f(x, y)

	// Project (x,y,z) isometrically onto 2-D SVG canvas (sx,sy).
	sx := width/2 + (x-y)*cos30*xyscale
	sy := height/2 + (x+y)*sin30*xyscale - z*zscale
	return sx, sy
}

func f(x, y float64) float64 {
	r := math.Hypot(x, y) // distance from (0,0)
	return math.Sin(r) / r
}

//!-

`

type compiler struct {
	fset *token.FileSet
	ast  *ast.File

	pkg *types.Package
	inf types.Info

	initPkgs []string
}

func newCompiler(s string) (*compiler, error) {
	c := compiler{
		fset: token.NewFileSet(),
		inf: types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
		},
	}

	f, err := parser.ParseFile(c.fset, "input.go", s, 0)
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

func (c *compiler) toTypeSig(t types.Type) (string, error) {
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

func (c *compiler) toNilVal(t types.Type) (string, error) {
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

func (c *compiler) genFuncProto(name string, sig *types.Signature, out func(name, retType, params string)) {
	sigParm := sig.Params()
	var params []string
	for p := 0; p < sigParm.Len(); p++ {
		parm := sigParm.At(p)
		typ, err := c.toTypeSig(parm.Type())
		if err != nil {
			log.Fatalf("%s", err)
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
			log.Fatalf("%s", err)
		}
		retType = s
	default:
		var mult []string

		for r := 0; r < res.Len(); r++ {
			s, err := c.toTypeSig(res.At(r).Type())
			if err != nil {
				log.Fatalf("%s", err)
			}

			mult = append(mult, s)
		}

		retType = fmt.Sprintf("std::pair<%s>", strings.Join(mult, ", "))
	}

	out(name, retType, strings.Join(params, ", "))
}

func (c *compiler) genInterface(name string, iface *types.Interface) {
	fmt.Printf("struct %s {\n", name)

	for m := iface.NumMethods(); m > 0; m-- {
		meth := iface.Method(m - 1)
		sig := meth.Type().(*types.Signature)

		c.genFuncProto(meth.Name(), sig, func(name, retType, params string) {
			fmt.Printf("virtual %s %s(%s) = 0;\n", retType, name, params)
		})
	}

	fmt.Printf("};\n")
}

func (c *compiler) genStruct(name string, s *types.Struct, n *types.Named) {
	fmt.Printf("struct %s", name)

	// FIXME: this is highly inneficient and won't scale at all
	var ifaces []string
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

				derived := fmt.Sprintf("public %s", def.Name())
				ifaces = append(ifaces, derived)
				break
			}
		}
	}
	if ifaces != nil {
		fmt.Printf(" : %s", strings.Join(ifaces, ", "))
	}

	fmt.Print(" {\n")
	numFields := s.NumFields()
	for f := 0; f < numFields; f++ {
		f := s.Field(f)

		typ, err := c.toTypeSig(f.Type())
		if err != nil {
			log.Fatalf("Couldn't generate field: %s", err)
		}

		nilVal, err := c.toNilVal(f.Type())
		if err != nil {
			log.Fatalf("Couldn't determine nil value for %s: %s", name, err)
		}

		if nilVal != "" {
			fmt.Printf("%s %s{%s};\n", typ, f.Name(), nilVal)
			continue
		}

		fmt.Printf("%s %s;\n", typ, f.Name())
	}

	fmt.Printf("};\n")
}

func (c *compiler) genBasicType(name string, b *types.Basic) {
	fmt.Printf("struct %s {\n", name)

	typ, err := c.toTypeSig(b.Underlying())
	if err != nil {
		log.Fatalf("Could not determine underlying type: %s", err)
	}

	nilValue, err := c.toNilVal(b.Underlying())
	if err != nil {
		log.Fatalf("Could not determine nil value for type %s: %s", typ, err)
	}

	fmt.Printf("%s Value{%s};\n", typ, nilValue)
	fmt.Printf("%s() { return Value; }\n", typ)
	fmt.Printf("%s(%s v) : Value{v} {}\n", name, typ)
	fmt.Printf("%s operator=(%s v) { return Value = v; }\n", typ, typ)
	fmt.Printf("};\n")
}

func (c *compiler) genNamedType(name string, n *types.Named) {
	switch t := n.Underlying().(type) {
	case *types.Interface:
		c.genInterface(name, t)
	case *types.Struct:
		c.genStruct(name, t, n)
	case *types.Basic:
		c.genBasicType(name, t)
	default:
		log.Fatalf("What to do with the named type %v?", reflect.TypeOf(t))
	}
}

func (c *compiler) genPrototype(name string, sig *types.Signature) {
	c.genFuncProto(name, sig, func(name, retType, params string) {
		fmt.Printf("%s %s(%s);\n", retType, name, params)
	})
}

func (c *compiler) genVar(v *types.Var, mainBlock bool) {
	typ, err := c.toTypeSig(v.Type())
	if err != nil {
		log.Fatalf("Couldn't get type signature for variable: %s", err)
	}

	nilVal, err := c.toNilVal(v.Type())
	if err != nil {
		log.Fatalf("Couldn't get nil value for variable: %s", err)
	}

	if mainBlock {
		if !v.Exported() {
			fmt.Print("static ")
		}
		fmt.Printf("%s %s;\n", typ, v.Name())
	} else {
		fmt.Printf("%s %s{%s};\n", typ, v.Name(), nilVal)
	}
}

func (c *compiler) genConst(k *types.Const, mainBlock bool) {
	typ, err := c.toTypeSig(k.Type())
	if err != nil {
		log.Fatalf("Couldn't get type signature for variable: %s", err)
	}

	if mainBlock {
		if !k.Exported() {
			fmt.Print("static ")
		}
		fmt.Printf("constexpr %s %s{%s};\n", typ, k.Name(), k.Val())
	} else {
		fmt.Printf("constexpr %s %s{%s};\n", typ, k.Name(), k.Val())
	}
}

func (c *compiler) genNamespace(p *types.Package, mainBlock bool) {
	if !mainBlock {
		fmt.Printf("namespace %s {\n", p.Name())
	}

	genTypeProto := func(name string, obj types.Object) {
		switch t := obj.Type().(type) {
		case *types.Named:
			c.genNamedType(name, t)
		case *types.Signature:
			c.genPrototype(name, t)
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
			genTypeProto(name, obj)
		case *types.TypeName:
			genTypeProto(name, obj)
		case *types.Var:
			c.genVar(t, mainBlock)
		case *types.Const:
			c.genConst(t, mainBlock)
		default:
			log.Fatalf("Don't know how to generate: %s", reflect.TypeOf(t))
		}
	}

	if !mainBlock {
		fmt.Printf("} // namespace %s\n", p.Name())
	}
}

func (c *compiler) genImports() {
	var genImport func(p *types.Package)

	genImport = func(p *types.Package) {
		for _, pkg := range p.Imports() {
			genImport(pkg)
		}
		c.genNamespace(p, false)
	}

	for _, pkg := range c.pkg.Imports() {
		genImport(pkg)
	}
}

func (c *compiler) genExpr(x ast.Expr) (string, error) {
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

func (c *compiler) genInit() bool {
	for ident, _ := range c.inf.Defs {
		if ident.Name == "init" {
			fmt.Printf("void init();\n")
			return true
		}
	}

	return false
}

func (c *compiler) genMain() {
	hasInit := c.genInit()

	fmt.Printf("int main() {\n")

	for _, pkg := range c.initPkgs {
		fmt.Printf("%s::init();\n", pkg)
	}

	if hasInit {
		fmt.Printf("init();\n")
	}

	for _, init := range c.inf.InitOrder {
		if len(init.Lhs) == 1 {
			fmt.Printf("%s", init.Lhs[0].Name())
		} else {
			var tie []string

			for _, lhs := range init.Lhs {
				tie = append(tie, lhs.Name())
			}

			fmt.Printf("std::tie(%s)", strings.Join(tie, ", "))
		}

		expr, err := c.genExpr(init.Rhs)
		if err != nil {
			log.Fatalf("Couldn't write initialization code: %s", err)
		}

		fmt.Printf("= %s;\n", expr)
	}

	fmt.Printf("_main();\n")
	fmt.Printf("return 0;\n")
	fmt.Printf("}\n")
}

func (c *compiler) genComment(comment *ast.Comment) {
	fmt.Printf("/* %s */", comment.Text)
}

func isParam(sig *types.Signature, name string) bool {
	parms := sig.Params()
	for p := 0; p < parms.Len(); p++ {
		if parms.At(p).Name() == name {
			return true
		}
	}
	return false
}

func (c *compiler) genFuncDecl(f *ast.FuncDecl) {
	var typ types.Object
	typ, ok := c.inf.Defs[f.Name]
	if !ok {
		log.Fatalf("Could not find type for func %s", f.Name.Name)
	}

	scope, ok := c.inf.Scopes[f.Type]
	if !ok {
		log.Fatalf("Could not find scope for %s", f.Name.Name)
	}

	name := f.Name.Name
	if name == "main" {
		name = "_main"
	}

	fun := typ.(*types.Func)
	sig := fun.Type().(*types.Signature)
	c.genFuncProto(name, sig, func(name, retType, params string) {
		fmt.Printf("%s %s(%s)\n", retType, name, params)
	})

	fmt.Println("{")

	for _, name := range scope.Names() {
		if isParam(sig, name) {
			continue
		}

		obj := scope.Lookup(name)

		v := obj.(*types.Var)
		c.genVar(v, false)
	}

	for _, stmt := range f.Body.List {
		c.walk(stmt)
		fmt.Println(";")
	}

	fmt.Println("}")
}

func (c *compiler) genAssignStmt(a *ast.AssignStmt) {
	if len(a.Lhs) == 1 {
		c.walk(a.Lhs[0])
	} else {
		fmt.Print("std::tie(")
		for i, e := range a.Lhs {
			c.walk(e)
			if i < len(a.Lhs)-1 {
				fmt.Print(", ")
			}
		}
		fmt.Print(")")
	}

	switch a.Tok {
	case token.ADD_ASSIGN:
		fmt.Print(" += ")
	case token.SUB_ASSIGN:
		fmt.Print(" -= ")
	case token.MUL_ASSIGN:
		fmt.Print(" *= ")
	case token.QUO_ASSIGN:
		fmt.Print(" *= ")
	case token.REM_ASSIGN:
		fmt.Print(" %= ")
	case token.AND_ASSIGN:
		fmt.Print(" &= ")
	case token.OR_ASSIGN:
		fmt.Print(" |= ")
	case token.XOR_ASSIGN:
		fmt.Print(" ^= ")
	case token.SHL_ASSIGN:
		fmt.Print(" <<= ")
	case token.SHR_ASSIGN:
		fmt.Print(" >>= ")
	case token.AND_NOT_ASSIGN:
		fmt.Print(" &= ~(")
	case token.ASSIGN, token.DEFINE:
		fmt.Print(" = ")
	default:
		log.Fatalf("Unknown assignment token")
	}

	for _, e := range a.Rhs {
		c.walk(e)
	}

	if a.Tok == token.AND_NOT_ASSIGN {
		fmt.Print(")")
	}
}

func (c *compiler) genIdent(i *ast.Ident) {
	fmt.Print(i.Name)
}

func (c *compiler) genCallExpr(call *ast.CallExpr) {
	c.walk(call.Fun)

	fmt.Printf("(")
	for i, arg := range call.Args {
		c.walk(arg)
		if i != len(call.Args)-1 {
			fmt.Printf(", ")
		}
	}
	fmt.Printf(")")
}

func (c *compiler) genSelectorExpr(s *ast.SelectorExpr) {
	var obj types.Object
	obj, ok := c.inf.Uses[s.Sel]
	if !ok {
		log.Fatalf("Sel not found for X: %s", s)
	}

	if pkg := obj.Pkg(); pkg != nil {
		fmt.Printf("%s::%s", pkg.Name(), s.Sel.Name)
	}
}

func (c *compiler) genBasicLit(b *ast.BasicLit) {
	switch b.Kind {
	case token.INT, token.FLOAT, token.CHAR, token.STRING:
		fmt.Printf("%s", b.Value)
	case token.IMAG:
		log.Fatalf("Imaginary numbers not supported")
	}
}

func (c *compiler) genForStmt(f *ast.ForStmt) {
	fmt.Printf("{")

	scope, ok := c.inf.Scopes[f]
	if !ok {
		log.Fatalf("Could not find scope")
	}
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		v := obj.(*types.Var)
		c.genVar(v, false)
	}

	fmt.Printf("for (")

	if f.Init != nil {
		c.walk(f.Init)
	}

	fmt.Printf("; ")
	if f.Cond != nil {
		c.walk(f.Cond)
	}

	fmt.Printf("; ")
	if f.Post != nil {
		c.walk(f.Post)
	}

	fmt.Printf(") {")
	for _, s := range f.Body.List {
		c.walk(s)
		fmt.Println(";")
	}

	fmt.Printf("}")

	fmt.Printf("}")

}

func (c *compiler) genExprStmt(e *ast.ExprStmt) {
	c.walk(e.X)
}

func (c *compiler) genBinaryExpr(b *ast.BinaryExpr) {
	c.walk(b.X)
	fmt.Printf(" %s ", b.Op)
	c.walk(b.Y)
}

func (c *compiler) genDeclStmt(d *ast.DeclStmt) {
	fmt.Printf("// declstmt %s\n", d)
}

func (c *compiler) genField(f *ast.Field) {
	fmt.Printf("// field %s\n", f)
}

func (c *compiler) genReturnStmt(r *ast.ReturnStmt) {
	fmt.Printf("return ")

	if len(r.Results) == 1 {
		c.walk(r.Results[0])
		return
	}

	var types []string
	for _, e := range r.Results {
		typ, ok := c.inf.Types[e]
		if !ok {
			log.Fatalf("Couldn't determine type of expression")
		}

		ctyp, err := c.toTypeSig(typ.Type)
		if err != nil {
			log.Fatalf("Couldn't get type signature: %s", err)
		}

		types = append(types, ctyp)
	}

	fmt.Printf("std::pair<%s>(", strings.Join(types, ", "))
	for i, e := range r.Results {
		c.walk(e)

		if i != len(r.Results)-1 {
			fmt.Print(", ")
		}
	}
	fmt.Printf(")")
}

func (c *compiler) genCompositeLit(cl *ast.CompositeLit) {
	if cl.Type != nil {
		c.walk(cl.Type)
	}

	fmt.Printf("{")
	for i, elt := range cl.Elts {
		c.walk(elt)
		if i < len(cl.Elts)-1 {
			fmt.Print(", ")
		}
	}
	fmt.Printf("}")
}

func (c *compiler) genParenExpr(p *ast.ParenExpr) {
	fmt.Print("(")
	c.walk(p.X)
	fmt.Print(")")
}

func (c *compiler) genIncDecStmt(p *ast.IncDecStmt) {
	c.walk(p.X)

	switch p.Tok {
	default:
		log.Fatalf("Unknown inc/dec token")
	case token.INC:
		fmt.Printf("++")
	case token.DEC:
		fmt.Printf("--")
	}
}

func (c *compiler) walk(node ast.Node) {
	switch n := node.(type) {
	default:
		log.Fatalf("Unknown node type: %s\n", reflect.TypeOf(n))

	case *ast.IncDecStmt:
		c.genIncDecStmt(n)

	case *ast.ParenExpr:
		c.genParenExpr(n)

	case *ast.Comment:
		c.genComment(n)

	case *ast.CommentGroup:
		for _, comment := range n.List {
			c.walk(comment)
		}

	case *ast.FuncDecl:
		c.genFuncDecl(n)

	case *ast.AssignStmt:
		c.genAssignStmt(n)

	case *ast.Ident:
		c.genIdent(n)

	case *ast.CallExpr:
		c.genCallExpr(n)

	case *ast.SelectorExpr:
		c.genSelectorExpr(n)

	case *ast.BasicLit:
		c.genBasicLit(n)

	case *ast.ForStmt:
		c.genForStmt(n)

	case *ast.ExprStmt:
		c.genExprStmt(n)

	case *ast.BinaryExpr:
		c.genBinaryExpr(n)

	case *ast.DeclStmt:
		c.genDeclStmt(n)

	case *ast.Field:
		c.genField(n)

	case *ast.ReturnStmt:
		c.genReturnStmt(n)

	case *ast.CompositeLit:
		c.genCompositeLit(n)

	case *ast.GenDecl:
	}
}

func (c *compiler) gen() {
	for _, decl := range c.ast.Decls {
		c.walk(ast.Node(decl))
	}
}

func main() {
	c, err := newCompiler(input)
	if err != nil {
		log.Fatalf("Couldn't parse program: %s", err)
	}

	c.genImports()
	c.genNamespace(c.pkg, true)
	c.genMain()

	c.gen()

	/*
		fmt.Println("----- types ---------------------------")
		for k, v := range c.inf.Types {
			fmt.Printf("k<%s>=%+v\n", reflect.TypeOf(k), k)
			fmt.Printf("v<%s>=%+v\n\n", reflect.TypeOf(v), v)
		}
		fmt.Println("----- defs ---------------------------")
		for k, v := range c.inf.Defs {
			fmt.Printf("k<%s>=%+v\n", reflect.TypeOf(k), k)
			fmt.Printf("v<%s>=%+v\n\n", reflect.TypeOf(v), v)
		}
		fmt.Println("----- uses ---------------------------")
		for k, v := range c.inf.Uses {
			fmt.Printf("k<%s>=%+v\n", reflect.TypeOf(k), k)
			fmt.Printf("v<%s>=%+v\n\n", reflect.TypeOf(v), v)
		}
		fmt.Println("----- scopes ---------------------------")
		for k, v := range c.inf.Scopes {
			fmt.Printf("k<%s>=%+v\n", reflect.TypeOf(k), k)
			fmt.Printf("v<%s>=%+v\n\n", reflect.TypeOf(v), v)
		}
		fmt.Println("----- initialization -------------------")
		for _, i := range c.inf.InitOrder {
			fmt.Println(i)
		}
		fmt.Println("----- implicits ---------------------------")
		for k, v := range c.inf.Implicits {
			fmt.Printf("k<%s>=%+v\n", reflect.TypeOf(k), k)
			fmt.Printf("v<%s>=%+v\n\n", reflect.TypeOf(v), v)
		}
		fmt.Println("----- selections ---------------------------")
		for k, v := range c.inf.Selections {
			fmt.Printf("k<%s>=%+v\n", reflect.TypeOf(k), k)
			fmt.Printf("v<%s>=%+v\n\n", reflect.TypeOf(v), v)
		}
	*/
}
