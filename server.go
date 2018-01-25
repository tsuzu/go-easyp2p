package easyp2p

import (
	"log"
	"strings"
	"time"

	"github.com/anacrolix/utp"
)

func RunServer(addr string) {
	log.Println("Server started")
	listener, err := utp.Listen(addr)

	if err != nil {
		panic(err)
	}

	defer listener.Close()

	for {
		conn, err := listener.Accept()

		if err != nil {
			log.Println("Accept Error:", err.Error())

			continue
		}

		go func() {
			defer conn.Close()

			addr := conn.RemoteAddr().String()

			conn.SetDeadline(time.Now().Add(10 * time.Second))

			b := make([]byte, 1024)
			n, err := conn.Read(b)

			if err != nil {
				return
			}

			b = b[:n]

			arr := strings.Split(string(b), " ")

			if len(arr) != 2 || arr[0] != IPDiscoveryRequestHeader {
				conn.Write([]byte(IPDiscoveryResponseHeaderProcotolError + " 0"))

				return
			}

			switch arr[1] {
			case IPDiscoveryVersion:
				conn.Write([]byte(IPDiscoveryResponseHeaderOK + " " + addr))
			default:
				conn.Write([]byte(IPDiscoveryResponseHeaderProcotolError + " 0"))
			}
		}()
	}
}
