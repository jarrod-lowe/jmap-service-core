// Rendered version of jmap-path-rewrite.js.tftpl for testing (stage_name=v1)
// CloudFront function to rewrite /.well-known/jmap path for API Gateway
// Adds stage prefix so /.well-known/jmap becomes /v1/.well-known/jmap
// Supports X-JMAP-Stage header to route e2e test traffic to the e2e stage
function handler(event) {
  var request = event.request;

  if (request.uri === '/.well-known/jmap') {
    var stage = 'v1';
    var stageHeader = request.headers['x-jmap-stage'];
    if (stageHeader && stageHeader.value === 'e2e') {
      stage = 'e2e';
    }
    request.uri = '/' + stage + '/.well-known/jmap';
  }

  return request;
}
