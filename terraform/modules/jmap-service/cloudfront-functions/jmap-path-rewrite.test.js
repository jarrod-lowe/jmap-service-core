const fs = require('fs');
const vm = require('vm');
const path = require('path');

// Load and evaluate the CloudFront function in a sandbox
const functionCode = fs.readFileSync(
    path.join(__dirname, 'jmap-path-rewrite.js'),
    'utf8'
);

// Create a context and run the function code to get the handler
const context = {};
vm.runInContext(functionCode, vm.createContext(context));

// The handler function is now available as context.handler
const handler = context.handler;

// Helper to create a mock CloudFront event
function createEvent(uri, headers = {}) {
    return {
        request: {
            uri: uri,
            headers: headers
        }
    };
}

describe('jmap-path-rewrite CloudFront function', () => {
    describe('default stage routing', () => {
        test('rewrites /.well-known/jmap to /v1/.well-known/jmap by default', () => {
            const event = createEvent('/.well-known/jmap');
            const result = handler(event);

            expect(result.uri).toBe('/v1/.well-known/jmap');
        });

        test('passes through non-matching URIs unchanged', () => {
            const event = createEvent('/v1/jmap');
            const result = handler(event);

            expect(result.uri).toBe('/v1/jmap');
        });

        test('passes through other paths unchanged', () => {
            const event = createEvent('/some/other/path');
            const result = handler(event);

            expect(result.uri).toBe('/some/other/path');
        });
    });

    describe('e2e stage routing via X-JMAP-Stage header', () => {
        test('rewrites to /e2e/.well-known/jmap when X-JMAP-Stage is e2e', () => {
            const event = createEvent('/.well-known/jmap', {
                'x-jmap-stage': { value: 'e2e' }
            });
            const result = handler(event);

            expect(result.uri).toBe('/e2e/.well-known/jmap');
        });

        test('ignores X-JMAP-Stage header for non-matching URIs', () => {
            const event = createEvent('/v1/jmap', {
                'x-jmap-stage': { value: 'e2e' }
            });
            const result = handler(event);

            expect(result.uri).toBe('/v1/jmap');
        });

        test('uses default stage when X-JMAP-Stage has unexpected value', () => {
            const event = createEvent('/.well-known/jmap', {
                'x-jmap-stage': { value: 'staging' }
            });
            const result = handler(event);

            expect(result.uri).toBe('/v1/.well-known/jmap');
        });

        test('uses default stage when X-JMAP-Stage header is empty', () => {
            const event = createEvent('/.well-known/jmap', {
                'x-jmap-stage': { value: '' }
            });
            const result = handler(event);

            expect(result.uri).toBe('/v1/.well-known/jmap');
        });
    });
});
