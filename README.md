# Gomoku

Gomoku is a compiler for programs written in the Go Programming
Language, targeting modern C++.

This is an experiment to determine how well Go will perform on embedded
devices, where a port of existing compilers would prove to be actually
more complicated due to the different expectations from platforms
supported by gc and devices with very few kilobytes of RAM.

The bulk of the compiler has been written in a day (April 1st, 2017),
but in reality, it's the author's second attempt at doing so.  The
lexer, parser, and type system are part of the standard library
already, so it was just the matter of tying everything together.

The generated code still doesn't compile: only the input program itself
is generated.  All the type information and constants from imported
packages is also generated, but no functions or methods from those are
actually generated.

Also, the code has been hacked together in a little bit less than a day
worth of work, so there are many mistakes and opportunities to
refactor.  Of note, no testing is performed.  Any help to move this
forward is greatly appreciated.

