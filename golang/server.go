package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

// A peer client from the z2z protocol.
type peerClient struct {
	// Peer id.
	id string

	// Holds the estabilished h2 client to the peer.
	h2Client *http2.ClientConn
}

// Caches peerClients based on a peer id. Each peer id may contain
// multiple clients.
type clientCache struct {
	mu      sync.Mutex
	clients map[string][]*peerClient
}

// Adds a peerClient to cache.
func (cc *clientCache) tryAddClient(peer *peerClient) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	clients, ok := cc.clients[peer.id]
	if !ok {
		clients = []*peerClient{}
		cc.clients[peer.id] = clients
	}
	cc.clients[peer.id] = append(cc.clients[peer.id], peer)
	return nil
}

// Lookup connections for the peer id.
func (cc *clientCache) lookupClient(id string) ([]*peerClient, bool) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	clients, ok := cc.clients[id]
	return clients, ok
}

func main() {
	http2.VerboseLogs = os.Getenv("DEBUG") != ""

	// Create a "cloud" instance of the server and listne
	cloud := createServer("localhost:3000")
	go cloud.ListenAndServe()

	// Create a "hub" instance of the server and listne
	hub := createServer("localhost:30001")
	go hub.ListenAndServe()

	// Peer to the cloud server. If disconnected reconnect.
	for {
		// Peer to a given address and proxy h2 requests to the hub server.
		peerToServer("http://localhost:3000/hijack", hub)
		time.Sleep(time.Second * 1)
	}
}

// Tries to peer to a remote server wit the z2z methods. Proxies all received
// requests on the h2 connetion to the given server
func peerToServer(url string, server *http.Server) {
	log.Printf("Peering to %s", url)
	// Create a blank http2 server. This is used to server the h2 connection.
	// NOTE: Could be replaced with a single h2 server created in the main.
	h2Server := http2.Server{}

	// Create a custom net.Dialer to override the default used by http.Transport.
	// We need to override the DialContext function to get ahold of the TCP connection
	// used by request/response. Once the HTTP response is received we pass the connection
	// to the HTTP/2 server.
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 60 * time.Second,
	}

	// When dialer.DialContext returns store the TCP connection here.
	var clientConnection net.Conn

	// Create a HTTP/1 Client. Override the Dial func to record the TCP connection.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				// Call original Dial()
				conn, err := dialer.DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				// save the TCP connection.
				clientConnection = conn
				return conn, err
			},
		},
	}

	// Peer to the server below. Start with a
	resp, err := client.Post(url, "application/vnd.zetta.h2z2z", nil)
	if err != nil {
		log.Printf("Failed to peer: %s %s", err, url)
		return
	}

	// Check to ensure the status code is 101.
	if resp.StatusCode != 101 {
		log.Printf("Failed to peer %s, status code not 101 was %d: %s", url, resp.StatusCode, err)
		return
	}

	// Example of a HUB side shutdown. Note this just closes the TCP connection.
	// The Golang http2 interface does not provide a way to get ahold of the
	// server connection to send a graceful GOAWAY.
	// go func() {
	// 	time.Sleep(time.Second * 5)
	// 	log.Printf("Closing Client Connection")
	// 	clientConnection.Close()
	// }()

	// Serve the TCP connection. Provide the base HTTP server and HTTP handler.
	// This will block until the connection is closed.
	h2Server.ServeConn(clientConnection, &http2.ServeConnOpts{
		Handler:    server.Handler,
		BaseConfig: server,
	})

	log.Printf("Peer connection %s Closed....", url)
}

func createServer(address string) *http.Server {
	// Init a peer cache
	clientCache := clientCache{
		clients: make(map[string][]*peerClient),
	}

	// Create a new ServerMux to seperate from the default from the cloud server in the same binary.
	mux := http.NewServeMux()

	// Proxy usrl
	mux.HandleFunc("/proxy", func(rw http.ResponseWriter, req *http.Request) {
		// Peer ID from HEADER
		peerID := req.Header.Get("Peer")

		log.Printf("Received Proxy Request for peer %s", peerID)

		// Lookup in the peered clients for the peer "peer"
		connections, ok := clientCache.lookupClient(peerID)
		if !ok {
			http.Error(rw, "Peer Not found", http.StatusNotFound)
			return
		}

		// Take the first connection there may be multiple.
		// TODO: This should be a LIFO to ensure the most recent
		// peered connection is used. Should be pluggable.
		h2Client := connections[0].h2Client

		// Use the CloseNotifier to cancel the request if the initiating
		// request is closed.
		// Taken from https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go
		ctx := req.Context()
		if cn, ok := rw.(http.CloseNotifier); ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			defer cancel()
			notifyChan := cn.CloseNotify()
			go func() {
				select {
				case <-notifyChan:
					cancel()
				case <-ctx.Done():
				}
			}()
		}

		// Create a shallow copy of the request with the context of the originating request.
		outreq := req.WithContext(ctx)
		if req.ContentLength == 0 {
			outreq.Body = nil // Issue 16036: nil Body for http.Transport retries
		}
		// Clone request headers.
		outreq.Header = cloneHeader(req.Header)
		outreq.Close = false
		// IMPORTANT: This must be set to HOst of the first request over the h2
		// connection or it will try to use the DialTLS call again and start a new h2
		// connection over the same TCP socket and fail.
		outreq.URL.Host = peerID

		// TODO: Hardcode for now. Should strip base path out.
		outreq.URL.Path = "/hi"

		// Needs to be set for h2 client.
		outreq.URL.Scheme = "https"

		// Proxy the request.
		resp, err := h2Client.RoundTrip(outreq)
		if err != nil {
			http.Error(rw, "Connection failed", http.StatusBadGateway)
			return
		}
		// Copy the response headers.
		copyHeader(rw.Header(), resp.Header)
		// Write the headers and status.
		rw.WriteHeader(resp.StatusCode)

		// Copy the response body between requests.
		err = copyResponse(rw, resp.Body)
		if err != nil {
			http.Error(rw, "Connection failed", http.StatusInternalServerError)
			return
		}
		resp.Body.Close() // close now, instead of defer, to populate res.Trailer
	})

	mux.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "Hello, world!\n")
	})

	mux.HandleFunc("/hi", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Other Header", "OK")
		w.WriteHeader(200)
		io.WriteString(w, "Hub Received proxy request!\n")
	})

	mux.HandleFunc("/hijack", func(w http.ResponseWriter, req *http.Request) {
		log.Printf("Received Hijack Request")

		// Hijack the TCP connection from the HTTP ResponseWriter.
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
			return
		}
		// Return the TCP connection object and a r/w buffer.
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Write the 101 Upgrade response back to the client. The headers are hard
		// coded for now.
		resp := http.Response{
			StatusCode:    101,
			ProtoMajor:    1,
			ProtoMinor:    1,
			ContentLength: 0,
			Header: http.Header{
				"Connection":           {"Upgrade"},
				"Upgrade":              {"websocket"},
				"Sec-Websocket-Accept": {"xl25ZO2sm/ejbX8qKZlpX7PCXr8="},
				"Content-Length":       {"0"},
			},
		}
		// Write the response.
		resp.Write(bufrw.Writer)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Flush all writes. After this call all io is assumed to be HTTP/2 binary protocol.
		bufrw.Flush()

		ts := &http2.Transport{}
		cc, err := ts.NewClientConn(conn)
		if err != nil {
			log.Printf("failed to create h2 client %s", err)
			return
		}

		// Create a dialer that when dialTLS is called will return the hijacked connection.
		client := &peerClient{
			id:       "peer",
			h2Client: cc,
		}

		// Add client to client cache the /proxy request handler uses this to
		// send http requests to a peered conncetion.
		clientCache.tryAddClient(client)

		// Example of CLOUD side shutdown.
		// go func() {
		// 	time.Sleep(time.Second * 5)

		// 	log.Printf("Closing peer connection.")
		// 	client.h2Client.Shutdown(context.Background())
		// }()
	})

	return &http.Server{
		Addr:    address,
		Handler: mux,
	}
}

// Taken from https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go
func cloneHeader(h http.Header) http.Header {
	h2 := make(http.Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}

// Taken from https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go
func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// Taken from https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go
func copyResponse(dst io.Writer, src io.Reader) error {
	var buf []byte
	_, err := copyBuffer(dst, src, buf)
	return err
}

// Taken from https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go
// copyBuffer returns any write errors or non-EOF read errors, and the amount
// of bytes written.
func copyBuffer(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	if len(buf) == 0 {
		buf = make([]byte, 32*1024)
	}
	var written int64
	for {
		nr, rerr := src.Read(buf)
		if rerr != nil && rerr != io.EOF && rerr != context.Canceled {
			log.Printf("httputil: ReverseProxy read error during body copy: %v", rerr)
		}
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if werr != nil {
				return written, werr
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				rerr = nil
			}
			return written, rerr
		}
	}
}
