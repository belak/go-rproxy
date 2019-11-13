package main

import (
	"bufio"
	"bytes"
	"log"
	"net"
	"net/http"
	"sync"
)

func main() {
	log.Println("Starting rproxy")

	fallbackChan := make(chan net.Conn)

	l, err := net.Listen("tcp", ":8000")
	if err != nil {
		log.Fatalln(err)
	}

	httpListener := newProxyListener(l.Addr(), fallbackChan)
	go http.Serve(httpListener, http.FileServer(http.Dir(".")))

	for {
		c, err := l.Accept()
		if err != nil {
			log.Fatalln(err)
		}

		go func() {
			// TODO: ensure we don't leak net conns because we haven't closed them
			br := bufio.NewReader(c)

			data, err := br.Peek(10)
			if err != nil {
				log.Println(err)
				return
			}

			// Create a wrapper conn so we can store the peeked data somewhere.
			peeked, _ := br.Peek(br.Buffered())
			wrapped := &Conn{
				Peeked: peeked,
				Conn:   c,
			}

			if bytes.HasPrefix(data, []byte("SSH-2.0")) {
				handleTCPProxy(wrapped)
			} else {
				fallbackChan <- wrapped
			}
		}()
	}
}

func handleTCPProxy(c net.Conn) {
	defer c.Close()

	addr, _ := net.ResolveTCPAddr("tcp", "localhost:2222")
	serv, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		log.Println(err)
		return
	}

	defer serv.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go proxyCopy(&wg, serv, c)
	go proxyCopy(&wg, c, serv)

	wg.Wait()
}
