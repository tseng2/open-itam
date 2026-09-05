// cmd/agent-watchdog/main.go - 双向守护者
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: agent-watchdog <core-exe-path>")
		os.Exit(1)
	}
	target := os.Args[1]
	for {
		if !isRunning(target) {
			log("core-agent exited, restarting...")
			_ = exec.Command(target).Start()
		}
		time.Sleep(5 * time.Second)
	}
}

func isRunning(path string) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", filepath.Base(path))).Output()
	if err != nil {
		return false
	}
	return len(out) > 100
}

func log(msg string) {
	f, _ := os.OpenFile("watchdog.log", os.O_CREATE|os.O_APPEND, 0644)
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}
