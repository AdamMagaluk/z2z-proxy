# Golang Z2Z Proxy

Example of [Zetta.js](http://zettajs.com) [Z2Z protocol](https://github.com/zettajs/zetta/wiki/Zetta-Server-to-Server-Protocol-(Z2Z)-%5BDRAFT%5D).

The Zetta server-to-server protocol allows instances of Zetta to peer with one another. An initiating peer begins a connection request. Once the receiving peer has accepted, the receiving peer can see the devices connected to the initiating peer. This allows Zetta apps and API clients to access these devices across network boundaries.

Note this example only addresses the Peering conect of the connection and not the zetta specific tings like subscriptions and state requests.

## Usage

Start the server. Contains a cloud server listening on `http://localhost:3000` and a hub server listening on `http://localhost:3001`. Their sever implementation is identical it's just a matter of which port they listen on.

Once the servers on listening the `hub` will try to peer to the `http://localhot:3000/hijack`.


**Start A Cloud Instance**
```
go run server.go --address ":3000"
```

**Start a Hub**
```
go run server.go --address ":3001" --peer "http://localhost:3000" --id peer1
```


Make a request wich travels over the z2z connection. Note: The Peer header and the `id` flag must match.

```
curl -v -H "Peer:peer1" localhost:3000/proxy/hi?test=abc
```

This makes a request to cloud `/proxy/hi` which forwards the request to the hub with the id `peer1` to it's `/hi` path.


## Server Pushes

This Golang does not support http2 server push.