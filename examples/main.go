package main

import (
	"context"
	"log"
	"sync"

	"github.com/cs3238-tsuzu/go-easyp2p"
)

func main() {
	a, b := make(chan string, 1), make(chan string, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Wait()
	go func(send, recv chan string) {
		defer wg.Done()

		p2p := easyp2p.NewP2PConn([]string{"stun.l.google.com:19302"})
		defer p2p.Close()

		if least, err := p2p.DiscoverIP(); err != nil {
			log.Println(err)

			if !least {
				return
			}
		}

		if desc, err := p2p.LocalDescription(); err != nil {
			panic(err)
		} else {
			a <- desc
			log.Println(desc)
		}

		log.Println("connecting")
		if err := p2p.Connect(context.Background(), <-b); err != nil {
			log.Println(err)

			return
		}

		log.Println("connected")

		b := make([]byte, 1024)
		n, err := p2p.Read(b)

		log.Println(string(b[:n]))

		if err != nil {
			log.Println(err)

			return
		}
	}(a, b)

	func(send, recv chan string) {
		p2p := easyp2p.NewP2PConn([]string{"stun.l.google.com:19302"})
		defer p2p.Close()

		if least, err := p2p.DiscoverIP(); err != nil {
			log.Println(err)

			if !least {
				return
			}
		}

		if desc, err := p2p.LocalDescription(); err != nil {
			panic(err)
		} else {
			b <- desc
			log.Println(desc)
		}

		log.Println("connecting")
		if err := p2p.Connect(context.Background(), <-a); err != nil {
			log.Println("connect error", err)
		}
		log.Println("successfully connected")

		if _, err := p2p.Write([]byte(`Hello, world!`)); err != nil {
			log.Println(err)

			return
		}
	}(b, a)
}
