package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

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

func promptLogin() (nick, pass string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("nick: ")
	nick, _ = reader.ReadString('\n')
	nick = strings.TrimSpace(nick)

	fmt.Print("password: ")
	pass, _ = reader.ReadString('\n')
	pass = strings.TrimSpace(pass)

	return nick, pass
}
