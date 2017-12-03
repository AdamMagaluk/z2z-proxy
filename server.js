var http = require('http');
var spdy = require('spdy');
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

var agent = null;
var pingInterval = null;

var topics = 0;
var cloud = argo()
  .use(titan)
  .allow({
    methods: ['DELETE', 'PUT', 'PATCH', 'POST'],
    origins: ['*'],
    headers: ['accept', 'content-type'],
    maxAge: '432000'
  })
  .use(function(handle) {
    handle('request', function(env, next) {
      if (!agent) {
        console.log('agent missing')
        env.response.statusCode = 502;
        return next(env);
      }

      var req = env.request;
      var res = env.response;

      var opts = {
        method: req.method,
        headers: req.headers,
        path: req.url,
        agent: agent,
        pipe: true
      };

      console.log('req', req.url)
      var request = http.request(opts, function(response) {
        Object.keys(response.headers).forEach(function(header) {
          res.setHeader(header, response.headers[header]);
        });
        
        res.statusCode = response.statusCode;
        env.response.body = response;
        next(env);
      }).on('error', function(err) {
        env.response.statusCode = 502;
        return next(env);
      });

      request.on('push', function(stream) {
        var encoding = stream.headers['x-event-encoding'] || 'json';
        var length = Number(stream.headers['content-length']);
        console.log(stream.headers);
        var data = Buffer.alloc(length);
        var idx = 0;
        var d = null;
        stream.on('readable', function() {
          while (d = stream.read()) {
            for (var i=0; i<d.length;i++) {
              data[idx++] = d[i];
            }
          };
        });

        stream.on('error', function(err) {
          console.log('stream error:', err)
        })

        stream.on('end', function() {
          var json = JSON.parse(data.toString());
          console.log(json);
          stream.destroy();
        });

      })
      
      if (req.body) {
        request.end(req.body);
      } else {
        req.pipe(request);
      }
    });
  });

cloud = cloud.build();
server.on('request', cloud.run);
//spdyServer.on('request', cloud.run);

var wss = new WebSocketServer({ server: server });
wss.on('connection', function(ws) {
  ws._socket.removeAllListeners('data'); // Remove WebSocket data handler.

  ws.on('error', function(err) {
    console.log(err)
  })

  agent = spdy.createAgent(SpdyAgent, {
    port: 80,
    socket: ws._socket,
    spdy: {
      plain: true,
      ssl: false,
      protocol: 'h2',
    }
  });
  
  clearInterval(pingInterval);
  
  agent.once('_connect', function() {
    pingInterval = setInterval(function() {
      agent._spdyState.connection.ping();
    }, 500);
  });

  agent.maxSockets = 150;
  agent.on('error', function(err) {
    console.error('agent error:', err);
    agent.close();
  });
});

server.listen(process.argv[2]);
