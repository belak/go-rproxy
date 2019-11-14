package main

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"net"
	"net/http"
	"sync"
)

func main() {
	log.Println("Starting rproxy")

	server := NewServer()
	err := server.Run(context.Background())
	if err != nil {
		log.Fatalln(err)
	}
}

func leftovers() {
	httpListener, err := net.Listen("tcp", ":80")
	if err != nil {
		log.Fatalln(err)
	}

	/*
		httpsListener, err := tls.Listen("tcp", ":443", cfg.TLSConfig())
		if err != nil {
			log.Fatalln(err)
		}
	*/

	fallbackChan := make(chan net.Conn)

	httpProxyListener := newProxyListener(httpListener.Addr(), fallbackChan)
	go http.Serve(
		httpProxyListener,
		http.FileServer(http.Dir(".")))

	for {
		c, err := httpListener.Accept()
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
				handleTCPProxy(wrapped, "localhost:2222")
			} else {
				fallbackChan <- wrapped
			}
		}()
	}
}

func handleTCPProxy(c net.Conn, remoteAddr string) {
	defer c.Close()

	serv, err := net.Dial("tcp", remoteAddr)
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
