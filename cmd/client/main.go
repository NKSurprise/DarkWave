package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Println("could not connect to server:", err)
		os.Exit(1)
	}
	defer conn.Close()

	clearScreen()
	fmt.Println("=== connected to chat server ===")
	fmt.Println()

	nick, pass := promptLogin()
	fmt.Fprintf(conn, "/nick %s\n", nick)
	fmt.Fprintf(conn, "%s\n", pass)

	done := make(chan struct{})
	go readLoop(conn, done)
	go writeLoop(conn)

	<-done
	fmt.Println("goodbye!")
}
