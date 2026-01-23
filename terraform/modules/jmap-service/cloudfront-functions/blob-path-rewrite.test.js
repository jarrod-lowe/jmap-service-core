const fs = require('fs');
const vm = require('vm');
const path = require('path');

// Load and evaluate the CloudFront function in a sandbox
const functionCode = fs.readFileSync(
    path.join(__dirname, 'blob-path-rewrite.js'),
    'utf8'
);

// Create a context and run the function code to get the handler
const context = {};
vm.runInContext(functionCode, vm.createContext(context));

// The handler function is now available as context.handler
const handler = context.handler;

// Helper to create a mock CloudFront event
function createEvent(uri) {
    return {
        request: {
            uri: uri,
            headers: {}
        }
    };
}

describe('blob-path-rewrite CloudFront function', () => {
    describe('basic path rewriting', () => {
        test('strips /blobs prefix from path', () => {
            const event = createEvent('/blobs/acct123/blob456');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456');
            expect(result.headers['range']).toBeUndefined();
        });

        test('passes through paths without /blobs prefix', () => {
            const event = createEvent('/other/path');
            const result = handler(event);

            expect(result.uri).toBe('/other/path');
        });
    });

    describe('range extraction from composite blobId', () => {
        test('extracts range from composite blobId and adds Range header', () => {
            const event = createEvent('/blobs/acct123/blob456,0,999');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456');
            expect(result.headers['range']).toEqual({ value: 'bytes=0-999' });
        });

        test('extracts range with larger byte values', () => {
            const event = createEvent('/blobs/acct123/blob456,1024,5120');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456');
            expect(result.headers['range']).toEqual({ value: 'bytes=1024-5120' });
        });

        test('handles zero start byte', () => {
            const event = createEvent('/blobs/acct123/blob456,0,100');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456');
            expect(result.headers['range']).toEqual({ value: 'bytes=0-100' });
        });
    });

    describe('invalid range handling (passes through unchanged)', () => {
        test('passes through non-numeric start value unchanged', () => {
            const event = createEvent('/blobs/acct123/blob456,foo,bar');
            const result = handler(event);

            // Should pass through unchanged - S3 will 404
            expect(result.uri).toBe('/acct123/blob456,foo,bar');
            expect(result.headers['range']).toBeUndefined();
        });

        test('passes through negative start value unchanged', () => {
            const event = createEvent('/blobs/acct123/blob456,-1,100');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456,-1,100');
            expect(result.headers['range']).toBeUndefined();
        });

        test('passes through when start >= end unchanged', () => {
            const event = createEvent('/blobs/acct123/blob456,100,50');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456,100,50');
            expect(result.headers['range']).toBeUndefined();
        });

        test('passes through when start == end unchanged', () => {
            const event = createEvent('/blobs/acct123/blob456,100,100');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456,100,100');
            expect(result.headers['range']).toBeUndefined();
        });
    });

    describe('non-composite blobId handling', () => {
        test('handles simple blobId without commas', () => {
            const event = createEvent('/blobs/acct123/blob456');
            const result = handler(event);

            expect(result.uri).toBe('/acct123/blob456');
            expect(result.headers['range']).toBeUndefined();
        });

        test('handles blobId with one comma (not composite)', () => {
            const event = createEvent('/blobs/acct123/blob,456');
            const result = handler(event);

            // One comma means not a valid composite format - pass through
            expect(result.uri).toBe('/acct123/blob,456');
            expect(result.headers['range']).toBeUndefined();
        });

        test('handles blobId with more than two commas (not composite)', () => {
            const event = createEvent('/blobs/acct123/a,b,c,d');
            const result = handler(event);

            // Too many commas - pass through unchanged
            expect(result.uri).toBe('/acct123/a,b,c,d');
            expect(result.headers['range']).toBeUndefined();
        });
    });
});
