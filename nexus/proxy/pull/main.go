package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: ptmove-pull hub|agent")
	}
	var err error
	switch os.Args[1] {
	case "hub":
		err = runHub()
	case "agent":
		err = runAgent()
	default:
		err = fmt.Errorf("unknown PTMove pull mode")
	}
	if err != nil {
		log.Fatal(err)
	}
}
