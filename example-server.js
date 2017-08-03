var http = require('http');

http.createServer(function(req, res) {
  res.setHeader('x-example', 'abc123');
  res.end('hello world');
}).listen(1337);
