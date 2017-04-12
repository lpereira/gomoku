# Gomoku

Gomoku is a compiler for programs written in the Go Programming
Language, targeting modern C++.

This is an experiment to determine how well Go will perform on embedded
devices.  Please see the FAQ for more details on the reasoning behind
this project.

Help to move this forward is greatly appreciated.

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
  _defer_.Push([=]() mutable { fmt::Printf("defer %d\n", x); });
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
  while (y != 0) {
    std::tie(x, y) = std::tuple<int, int>(y, x % y);
  }
  return x;
}
```

## Switch statement (tagged)

### Go
```Go
switch i {
default:
        println(4)
        fallthrough
case 1:
        println(1)
case 2:
        println(2)
case 3:
        println(3)
}
```

### C++
```C++
if ((i == 1)) {
_ident_1_:
  println(1);
} else if ((i == 2)) {
_ident_2_:
  println(2);
} else if ((i == 3)) {
_ident_3_:
  println(3);
} else {
_ident_0_:
  println(4);
  goto _ident_1_;
}
```

## Switch statement (non-tagged)

### Go
```Go
switch {
default:
        println(0)
case i > 10 && i != 50:
        println(1)
case i < 20, i > 150:
        println(2)
}
```

### C++
```C++
if ((i > 10 && i != 50)) {
_ident_1_:
  println(1);
} else if ((i < 20) || (i > 150)) {
_ident_2_:
  println(2);
} else {
_ident_0_:
  println(0);
}
```

## Button and LED sample from Gobot's Getting Started

### Go
```Go
func main() {
        firmataAdaptor := firmata.NewAdaptor("/dev/ttyACM0")

        button := gpio.NewButtonDriver(firmataAdaptor, "5")
        led := gpio.NewLedDriver(firmataAdaptor, "13")

        work := func() {
                button.On(gpio.ButtonPush, func(data interface{}) {
                        led.On()
                })
                button.On(gpio.ButtonRelease, func(data interface{}) {
                        led.Off()
                })
        }

        robot := gobot.NewRobot("buttonBot",
                []gobot.Connection{firmataAdaptor},
                []gobot.Device{button, led},
                work,
        )

        robot.Start()
}
```

### C++
```C++
void _main() {
  ButtonDriver *button{std::nullptr};
  Adaptor *firmataAdaptor{std::nullptr};
  LedDriver *led{std::nullptr};
  Robot *robot{std::nullptr};
  std::function<void()> work{std::nullptr};

  firmataAdaptor = firmata::NewAdaptor("/dev/ttyACM0");
  button = gpio::NewButtonDriver(firmataAdaptor, "5");
  led = gpio::NewLedDriver(firmataAdaptor, "13");
  work = [=]() mutable -> void {
    button->On(gpio::ButtonPush,
               [=](moku::empty_interface data) mutable -> void { led->On(); });
    button->On(gpio::ButtonRelease,
               [=](moku::empty_interface data) mutable -> void { led->Off(); });
  };
  robot = gobot::NewRobot("buttonBot",
                          moku::slice<gobot::Connection>{firmataAdaptor},
                          moku::slice<gobot::Device>{button, led}, work);
  robot->Start();
}
```

# Usage

Pass the path to the program being built.  Paths relative to `$GOHOME`,
relative, and absolute paths are accepted.  Output will be generated in
`outdir/` under the current directory (this directory will be cleared
every run):

    gomoku ./samples/interfaces

This will generate a pair of `.cpp` and `.h` files for each imported
package (and their dependencies).  Generated files are not indented
(yet), so piping them through `clang-format` (or other indenting tool)
is recommended.

It is posible that the compiler will abort midway through code
generation since not all data types, statements, and expressions are
implemented yet.

# FAQ

## What's with the name?

Go is one of the oldest board games that are still played today.  Gomoku
is a newer game, but is played with the same board and pieces.  The rules
are different, but for the untrained eye, they look exactly the same.

The parallels with the Go programming language and this compiler were too
good to not make the pun.

## Why not just modify the Go compiler?

There are a few reasons:

* There are many architectures out there that I'd like to support, and
writing a backend for every single one of them would be impractical. 
Mainly, We'd like to support x86, ARC, ARM Thumb, Xtensa, and RISC-V. 
x86 is already (well) supported by the reference Go compiler, but
microcontroller class x86 will often sport an older ISA (usually i586
class CPU or older).
* Go was never designed for embedded applications, specially when
executing on environments with severe memory restrictions such as many
microcontrollers.
* While C++ is a language that people love to hate, the modern variants are
powerful, reasonably expressive, and compilers such as GCC and Clang
leverage years of engineering time in order to produce correct, efficient
code.  All the while supporting all the architectures that we'd want to
support.
* The Go compiler was recently changed to use an SSA backend; this made
it easier to write backends, and it's possible to write one that
generates C code.  This would mostly take care of the instruction set,
though, still leaving on the table many of the assumptions for the kind
of operating systems the reference compiler has been designed to
generate code for.
* This is a good challenge and a great way to learn all the nooks and
crannies of a language.

## What's the license?

It's a 2-clause BSD.

## I'd like to contribute.  Is there a code of conduct?

Yes.  [We're using the same code that the Go commmunity
uses](https://golang.org/conduct).

## When it will actually be able to generate compilable code?

It's hard to tell; there are still a lot of things to do.  Some of them
are easier than others, but there are a lot of subtle details that are
hard to get right.

But we're very open to contributions, so if you'd like to see this
happen faster, you know what to do.

## Will it be self-hosting?

The original Go compiler was written in C, mechanically translated to
Go (with manual work to fix up the translation).  If this compiler
could compile itself, it would be almost a full circle.  While awesome,
we're quite far away from this possibility.

## What platforms will this support?

At first, Linux with STL is going to be the only supported platform. 
The reason is that tools such as AddressSanitizer and Valgrind are
invaluable when debugging.

Afterwards, it's very possible that an RTOS such as Zephyr will be
supported.  There's a good possibility that STL will be ditched at this
point, favoring either something existing but lightweight, or something
custom made.

In any case, compilation for different operating systems and/or
architectures should be as simple as setting the GOOS and GOARCH
environment variables, or different but related variables that will be
specific to Gomoku.

## Are there unit tests?

No, not yet.  This is a must and will happen soon.

## What about memory management, what's going to be the strategy?

At first, a stop-the-world-mark-and-sweep garbage collector, with
collection happening at allocation phase, when there's no more heap
space available.

This will most likely be custom made, but experimnents with existing
collectors might be performed -- if they're adequate, there will be no need
to write (or modify) one.

## Will it use the Go standard library?

At first, yes, as that's what we already have.  However, it's very
likely that a lightweight library will be written for embedded devices.
Not everything should be written from scratch, though; some parts from the
standard library might be cherry-picked.

## What's necessary to have this actually work?

There are many things that can (and need) to be performed before calling
this even remotely usable.  Here's a short list of tasks and things that
need to be implemented; it's by no means complete, and some of these tasks
are way more challenging than others:

- [ ] Generating code for imports, not only type information
- [ ] Ensuring that all types implementing interfaces are assignable to the interface type
- [ ] Fix the order of generated types
- [ ] Reorganize the package in smaller files
- [ ] Implement unit tests
- [ ] Get pointer vs. value semantics as correct as possible
- [ ] Implement type conversion
- [ ] Implement type assertion (incl. type switches)
- [ ] Implement basic Go data types (arrays, slices, and maps)
- [x] Nil values
- [ ] Comparison with nil values
- [ ] Built-in functions (e.g. make(), new(), append(), cap(), etc.)
- [x] Closures / anonymous functions
- [x] Deferred statements
- [x] Range-based loops
- [x] Switch statement
- [x] If statement
- [ ] Write a basic standard library for embedded devices
- [ ] Memory management with garbage collection
- [ ] Perform escape analysis to determine where to allocate things
- [ ] Channels (including select statement)
- [ ] Goroutines
