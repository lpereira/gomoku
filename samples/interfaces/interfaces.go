package main

type Interfacer interface {
	Interface() int
}

type Foo struct {
	someInt int
}

func (f Foo) Interface() int {
	return f.someInt
}

func (f Foo) ConcreteMethod() int { return 42 }

func main() {
	var f Foo
	println(f.Interface())
	println(f.ConcreteMethod())
}
