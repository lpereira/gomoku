# Gomoku

Gomoku is a compiler for programs written in the Go Programming
Language, targeting modern C++.

This is an experiment to determine how well Go will perform on embedded
devices, where a port of existing compilers would prove to be actually
more complicated due to the different expectations from platforms
supported by gc: devices with severe memory restrictions and without
virtual memory.

The bulk of the compiler has been written in a day (April 1st, 2017). 
The lexer, parser, and type system are part of the standard library
already, so it was just the matter of tying everything together.

As one would expect from such a young project, it still doesn't fully
work.  The generated code still doesn't compile: only the input program
itself is generated, plus type information and constants from imported
packages.  (No method from packages are generated yet.)

Any help to move this forward is greatly appreciated.

# Sample code

## surface (from chapter 3 of "The Go Programming Language" by Kernighan and Donovan)

[Full code here](https://gist.github.com/lpereira/8bc64bf9796984b7868b8255d1692d59), excerpt below:

### Go

```Go
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
```

### C++

```C++
std::tuple<double, double> corner(int i, int j) {
  double sx{0};
  double sy{0};
  double x{0};
  double y{0};
  double z{0};
  x = xyrange * (double(i) / cells - 0.5);
  y = xyrange * (double(j) / cells - 0.5);
  z = f(x, y);
  sx = width / 2 + (x - y) * cos30 * xyscale;
  sy = height / 2 + (x + y) * sin30 * xyscale - z * zscale;
  return {sx, sy};
}
```

## samples/interfaces/interfaces.go

### Go

```Go
type Interfacer interface {
        Interface() int
}

type Foo struct {
        SomeInt int
}

func (f Foo) Interface() int {
        var f2 Foo
        return f2.SomeInt * f.ConcreteMethod()
}

func (f Foo) ConcreteMethod() int { return 42 }

func UsingInterfaceType(i Interfacer) int { return i.Interface() * 1234 }
```

### C++

```C++
struct Foo : public Interfacer {
  int SomeInt{0};
  int ConcreteMethod();
  virtual int Interface() override;
};
struct Interfacer { // NB: this is declared in the wrong order
  virtual int Interface() = 0;
};
int UsingInterfaceType(Interfacer i);
int Foo::Interface() {
  Foo f2{};
  return f2.SomeInt * this->ConcreteMethod();
}
int Foo::ConcreteMethod() { return 42; }
int UsingInterfaceType(Interfacer i) { return i.Interface() * 1234; }
```

## defer1 (chapter 5 of the same book)

### Go

```Go
func f(x int) {
        fmt.Printf("f(%d)\n", x+0/x) // panics if x == 0
        defer fmt.Printf("defer %d\n", x)
        f(x - 1)
}
```

### C++

```C++
void f(int x) {
  moku::defer _defer_;

  fmt::Printf("f(%d)\n", x + 0 / x);
  _defer_.Push([]() { fmt::Printf("defer %d\n", x); });
  f(x - 1);
}
```

## gcd

### Go

```Go
func gcd(x, y int) int {
	for y != 0 {
		x, y = y, x%y
	}
	return x
}
```

### C++
```C++
int gcd(int x, int y) {
  for (; y != 0;) {
    std::tie(x, y) = std::tuple<int, int>(y, x % y);
  }
  return x;
}
```

# Usage

The program is parsed from the standard input and written to the standard
output:

    gomoku < samples/interfaces/interfaces.go

The generated code isn't indented, so it's recommended to pipe it through
`clang-format`:

    gomoku < samples/interfaces/interfaces.go | clang-format

To generate type system debugging information instead of C++ code, pass
the -debugtype command line flag.  The output is very crude and only
intended for those developing the compiler itself.

# Moving forward

There are many things that can (and need) to be performed before calling
this even remotely usable.  Here's a short list of tasks and things that
need to be implemented; it's by no means complete, and some of these tasks
are way more challenging than others:

- [ ] Reorganize the package in smaller files
- [ ] Implement unit tests
- [ ] Get pointer vs. value semantics as correct as possible
- [ ] Implement type conversion
- [ ] Implement basic Go data types (arrays, slices, and maps)
- [x] Closures / anonymous functions
- [x] Deferred statements
- [x] Range-based loops
- [ ] Switch statement
- [ ] Write a basic standard library for embedded devices
- [ ] Memory management with garbage collection
- [ ] Perform escape analysis to determine where to allocate things
- [ ] Channels (including select statement)
- [ ] Goroutines

# FAQ

## What's with the name?

Go is one of the oldest board games that are still played today.  Gomoku
is a newer game, but is played with the same board and pieces.  The rules
are different, but for the untrained eye, they look exactly the same.

The parallels with the Go programming language and this compiler were too
good to not make the pun.

## Why not write an SSA backend for gc?

That would be indeed a fun project -- but wouldn't solve the problem I'm
interested in solving.  The compiler would still generate code that's
suitable for a different kind of platform, and this would essentially
just take care of the instruction set.  As it turns out, some of the
platforms I'm interested are x86 microcontrollers, and most of their
ISA is already supported by the existing gc backends.

## Why not use the golang.org/x/tools/go/ssa package instead?

I haven't looked at that package close enough.  It does state two things,
though: it's not intended for machine code generation and has an interface
that's likely to change.  Having said that, this package looks promising,
and I might revisit this idea one day, when I learn more about SSA.

In any case, I'm also very inexperienced in Go -- and writing a compiler
for the language, instead of just generating code from higher-level nodes,
is proving to a useful way to learn every nook and cranny.

## What's the license? Is there any code of conduct?

Nothing new here:  I'm adopting the same license as Go itself (see the 
LICENSE file) and the same code of conduct that the community knows.
