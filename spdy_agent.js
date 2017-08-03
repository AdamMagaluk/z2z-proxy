var Agent = require('http').Agent;
var util = require('util');

var SpdyAgent = module.exports = function(options) {
  this.socket = options.socket;
  this.host = options.host;
  this.port = options.port;
  Agent.call(this, options);
};
util.inherits(SpdyAgent, Agent);

SpdyAgent.prototype.createConnection = function(options) {
  console.log('SpdyAgent.createConnection');
  setTimeout(function() {
    console.log('connect...')
    options.socket.emit('connect');
  }, 0)
  return options.socket;
};

SpdyAgent.prototype.createSocket = function(options) {
  console.log('SpdyAgent.createSocket');
  return options.socket;
};
