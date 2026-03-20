package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const serverAddr = "localhost:3000"

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func moveCursorToBottom(rows int) {
	fmt.Printf("\033[%d;0H", rows)
}

func printMessage(msg string) {
	ts := time.Now().Format("15:04")
	fmt.Printf("\r\033[K[%s] %s\n", ts, msg)
}

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

	// login
	fmt.Print("nick: ")
	reader := bufio.NewReader(os.Stdin)
	nick, _ := reader.ReadString('\n')
	nick = strings.TrimSpace(nick)

	fmt.Print("password: ")
	pass, _ := reader.ReadString('\n')
	pass = strings.TrimSpace(pass)

	// send login command to server
	fmt.Fprintf(conn, "/nick %s %s\n", nick, pass)

	done := make(chan struct{})

	// goroutine: read from server and print
	go func() {
		defer close(done)
		s := bufio.NewScanner(conn)
		for s.Scan() {
			line := s.Text()
			// clear current input line, print message, reprint prompt
			fmt.Print("\r\033[K") // clear line
			printMessage(line)
			fmt.Print("> ") // reprint prompt
		}
		fmt.Println("\r\n** disconnected from server")
	}()

	// read from stdin and send to server
	go func() {
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
	}()

	<-done
	fmt.Println("goodbye!")
}
