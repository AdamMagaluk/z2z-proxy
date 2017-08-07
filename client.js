var spdy = require('spdy');
var argo = require('argo');
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
    .use(function(handle) {
      handle('request', function(env, next) {
        if (env.request.url === '/push') {
          var data = new Buffer(JSON.stringify({ timestamp: new Date().getTime() }));
          var opts = {
            request: { 'Host': 'fog.argo.cx',
                       'Content-Length': data.length
                     },
          };
          var stream = env.response.push('/event', opts);
          stream.end(data);
        }
        next(env);
      })
    })
    .target("http://localhost:1337");

cloud = cloud.build();
server.on('request', cloud.run);

console.log('Connecting to ', process.argv[2])
var ws = new WebSocket(process.argv[2], {});
ws.on('open', function onOpen(socket) {
  server.emit('connection', socket);
});

ws.start();










