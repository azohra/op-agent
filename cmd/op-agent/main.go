package main

import (
	"os"

	"github.com/azohra/op-agent/internal/agent"
)

func main() {
	os.Exit(agent.Main(os.Args[1:]))
}
