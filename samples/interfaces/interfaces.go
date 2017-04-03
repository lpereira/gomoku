package main

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

func main() {
	var f Foo
	println(f.Interface())
	println(f.ConcreteMethod())
}
