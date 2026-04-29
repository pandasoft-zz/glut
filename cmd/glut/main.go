package main

import (
	"os"
	"path/filepath"

	"github.com/pandasoft-zz/glut/internal/mockwrapper"
)

func main() {
	if filepath.Base(os.Args[0]) != "glut" && filepath.Base(os.Args[0]) != "glut.exe" {
		mockwrapper.Run()
		return
	}
	Execute()
}
