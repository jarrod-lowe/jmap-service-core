function handler(event) {
  var request = event.request;
  var host = request.headers.host.value;

  if (request.uri === '/.well-known/jmap') {
    return {
      statusCode: 308,
      statusDescription: 'Permanent Redirect',
      headers: {
        'location': { value: 'https://' + host + '/v1/.well-known/jmap' }
      }
    };
  }
  return request;
}
