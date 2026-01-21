// CloudFront function to rewrite /blobs/* paths for S3 origin
// Strips the /blobs prefix so /blobs/accountId/blobId becomes /accountId/blobId
function handler(event) {
    var request = event.request;

    // Strip /blobs prefix from URI
    if (request.uri.startsWith('/blobs/')) {
        request.uri = request.uri.substring(6); // Remove '/blobs' (6 chars)
    }

    return request;
}
