// CloudFront function to rewrite /blobs/* paths for S3 origin
// Strips the /blobs prefix so /blobs/accountId/blobId becomes /accountId/blobId
// Also handles composite blobIds with byte ranges: /blobs/accountId/blobId,start,end
// Extracts range and adds Range header for partial content requests
function handler(event) {
    var request = event.request;

    // Strip /blobs prefix from URI
    if (request.uri.startsWith('/blobs/')) {
        request.uri = request.uri.substring(6); // Remove '/blobs' (6 chars)
    }

    // Check for range suffix in blobId: /accountId/blobId,start,end
    var lastSlash = request.uri.lastIndexOf('/');
    if (lastSlash >= 0) {
        var blobIdPart = request.uri.substring(lastSlash + 1);
        var parts = blobIdPart.split(',');

        if (parts.length === 3) {
            var baseBlobId = parts[0];
            var start = parseInt(parts[1], 10);
            var end = parseInt(parts[2], 10);

            // Validate: non-negative, start < end, and both are valid numbers
            if (!isNaN(start) && !isNaN(end) && start >= 0 && end > start) {
                // Rewrite path to remove range suffix
                request.uri = request.uri.substring(0, lastSlash + 1) + baseBlobId;
                // Add Range header for S3 partial content
                request.headers['range'] = { value: 'bytes=' + start + '-' + end };
            }
            // If validation fails, pass through unchanged (S3 will 404 on invalid path)
        }
    }

    return request;
}
