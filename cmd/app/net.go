package main

import (
	"bufio"
	"fmt"
	"net"
)

type Connection struct {
	conn     net.Conn
	incoming chan string
	outgoing chan string
}

func connect(addr, nick, pass string) (*Connection, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	c := &Connection{
		conn:     conn,
		incoming: make(chan string, 100),
		outgoing: make(chan string, 100),
	}

	// send nick and password
	fmt.Fprintf(conn, "/nick %s\n", nick)
	fmt.Fprintf(conn, "%s\n", pass)

	// start read loop
	go func() {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			c.incoming <- scanner.Text()
		}
		close(c.incoming)
	}()

	// start write loop
	go func() {
		for msg := range c.outgoing {
			fmt.Fprintf(conn, "%s\n", msg)
		}
	}()

	return c, nil
}

func (c *Connection) send(msg string) {
	c.outgoing <- msg
}

func (c *Connection) close() {
	c.conn.Close()
}
