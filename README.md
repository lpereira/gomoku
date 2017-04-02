# Gomoku

Gomoku is a compiler for programs written in the Go Programming
Language, targeting modern C++.

This is an experiment to determine how well Go will perform on embedded
devices, where a port of existing compilers would prove to be actually
more complicated due to the different expectations from platforms
supported by gc: devices with severe memory restrictions and without
virtual memory.

The bulk of the compiler has been written in a day (April 1st, 2017),
The lexer, parser, and type system are part of the standard library
already, so it was just the matter of tying everything together.

The generated code still doesn't compile: only the input program itself
is generated.  All the type information and constants from imported
packages is also generated, but no functions or methods from those are
actually generated.

No automated testing is performed.  Any help to move this forward is
greatly appreciated.

# Sample code

## surface (from chapter 3 of "The Go Programming Language" by Kernighan and Donovan)

[Full code here](https://gist.github.com/lpereira/8bc64bf9796984b7868b8255d1692d59), excerpt below:

### Go

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


### C++

    std::pair<double, double> corner(int i, int j) {
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
      return std::pair<double, double>(sx, sy);
    }

## samples/interfaces/interfaces.go

### Go

    type Interfacer interface {
            Interface() int
    }

    type Foo struct {
            someInt int
    }

    func (f Foo) Interface() int {
            return f.someInt
    }

### C++

    struct Foo : public Interfacer {
      int someInt{0};
      virtual int Interface() override;
    };
    struct Interfacer { // NB: this is declared in the wrong order
      virtual int Interface() = 0;
    };
    int Foo::Interface() {
      // NB: the selector is wrong below; it should've been this->someInt
      return main::someInt;
    }

# Usage

The program is parsed from the standard input and written to the standard
output:

    gomoku < samples/interfaces/interfaces.go

To generate type system debugging information instead of C++ code, pass
the -debugtype command line flag.

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
- [ ] Closures / anonymous functions
- [ ] Deferred statements
- [ ] Range-based loops
- [ ] Switch statement
- [ ] Write a basic standard library for embedded devices
- [ ] Memory management with garbage collection
- [ ] Perform escape analysis to determine where to allocate things
- [ ] Channels (including select statement)
- [ ] Goroutines