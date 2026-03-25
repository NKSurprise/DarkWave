package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

func readLoop(conn net.Conn, done chan struct{}) {
	defer close(done)
	s := bufio.NewScanner(conn)
	for s.Scan() {
		line := s.Text()
		fmt.Print("\r\033[K")
		printMessage(line) // from ui.go
		fmt.Print("> ")
	}
	fmt.Println("\r\n** disconnected from server")
}

func writeLoop(conn net.Conn) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}
		fmt.Fprintf(conn, "%s\n", line)
		if line == "/quit" {
			return
		}
		fmt.Print("> ")
	}
}
