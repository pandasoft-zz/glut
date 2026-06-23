package main

import (
	"os"

	"github.com/pandasoft-zz/glut/internal/mockwrapper"
)

func main() {
	if mockwrapper.ShouldRunAsMock(os.Args, os.Environ()) {
		mockwrapper.Run()
		return
	}
	Execute()
}
