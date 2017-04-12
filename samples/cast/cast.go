package main

type Barker interface {
	Bark()
}

type Dog int

func (Dog) Bark() {

}

func toDog(x Barker) Dog {
	return x.(Dog)
}
