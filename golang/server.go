package main

import (
	"io"
	"log"
	"net"
	"crypto/tls"
	"fmt"
	"time"
	"context"
	"io/ioutil"
	"net/http"
	"golang.org/x/net/http2"
)

func main() {
	go startCloudServer()

	startHubServer()
}

func startHubServer() {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 60 * time.Second,
		DualStack: true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "Hello, world from hub!\n")
	})
	h2Server := http2.Server{}
	
	h1server := &http.Server{Addr: "127.0.0.1:3001", Handler: mux}
	go h1server.ListenAndServe()

	var clientConnection *net.Conn 

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				log.Printf("Custom Dial for Client %s %s", network, address)
				conn, err := dialer.DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				clientConnection = &conn
				return conn, err
			},
		},
	}

	resp, err := client.Get("http://localhost:3000/hijack")
	if err != nil {
		log.Fatalf("Failed get: %s", err)
	}

	if (resp.StatusCode != 101) {
		log.Fatalf("Failed get not 101 was %d: %s", resp.StatusCode, err)
	}
	h2Server.ServeConn(*clientConnection, &http2.ServeConnOpts{
		Handler:    mux,
		BaseConfig: h1server,
	})

}

func sendPeerRequest(client *http.Client) {
	resp, err := client.Get("https://server.go/test")
	if err != nil {
		log.Fatalf("Failed get: %s", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed reading response body: %s", err)
	}
	fmt.Printf(
		"Got response %d: %s %s\n",
		resp.StatusCode, resp.Proto, string(body))
}

func startCloudServer() {
	http.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "Hello, world!\n")
	})

	http.HandleFunc("/hijack", func(w http.ResponseWriter, req *http.Request) {
		log.Printf("Received Hijack Request")

		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := http.Response{
			StatusCode: 101,
			ProtoMajor: 1,
			ProtoMinor: 1,
			ContentLength: 0,
			Header: http.Header{
				"Connection": {"Upgrade"},
				"Upgrade": {"websocket"},
				"Sec-Websocket-Accept": {"xl25ZO2sm/ejbX8qKZlpX7PCXr8="},
				"Content-Length":   {"0"},
			},
			
		}
		resp.Write(bufrw.Writer)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		bufrw.Flush()

		client := &http.Client{
			Transport: &http2.Transport{
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					return conn, nil
				},
			},
		}

		sendPeerRequest(client)
	})

	log.Fatal(http.ListenAndServe("127.0.0.1:3000", nil))
}