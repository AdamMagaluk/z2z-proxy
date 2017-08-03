var http = require('http');
var spdy = require('spdy');
var heapdump = require('heapdump');
var argo = require('argo');
var titan = require('titan');
var WebSocketServer = require('ws').Server;
var SpdyAgent = require('./spdy_agent');
var WebSocket = require('./web_socket');

var server = spdy.createServer({
  connection: {
    windowSize: 1024 * 1024, // Server's window size 
    autoSpdy31: false
  },
  spdy: {
    plain: true,
    ssl: false
  }
});


var cloud = argo()
    .target("http://localhost:1337");

cloud = cloud.build();
//server.on('request', cloud.run);
server.on('request', cloud.run);

console.log('Connecting to ', process.argv[2])
var ws = new WebSocket(process.argv[2], {});
ws.on('open', function onOpen(socket) {
  server.emit('connection', socket);
});

ws.start();










