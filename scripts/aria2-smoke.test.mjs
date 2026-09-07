import assert from 'node:assert/strict';
import childProcess from 'node:child_process';
import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import { syncBuiltinESMExports } from 'node:module';
import { test } from 'node:test';

test('smoke reconnects to the newly published RPC port after restart', async (t) => {
    let secret;
    let paused = false;
    let restarted = false;
    let checksum;
    let portReads = 0;
    let callsAfterRestart = 0;
    const rpc = createServer(async (req, res) => {
        try {
            let body = '';
            for await (const chunk of req) body += chunk;
            const { method, params: [token, ...params] } = JSON.parse(body);
            if (restarted) callsAfterRestart++;
            res.setHeader('Content-Type', 'application/json');
            if (token !== `token:${secret}`) {
                res.end(JSON.stringify({ error: { message: 'Unauthorized' } }));
                return;
            }
            let result = 'OK';
            switch (method) {
                case 'aria2.getVersion': result = { version: 'test' }; break;
                case 'aria2.getGlobalOption': result = { 'max-concurrent-downloads': '2' }; break;
                case 'aria2.addUri': {
                    const url = new URL(params[0][0]);
                    url.hostname = '127.0.0.1';
                    const response = await fetch(url);
                    checksum = createHash('sha256').update(Buffer.from(await response.arrayBuffer())).digest('hex');
                    result = 'download';
                    break;
                }
                case 'aria2.pause': paused = true; break;
                case 'aria2.unpause': paused = false; break;
                case 'aria2.addTorrent': result = 'torrent'; break;
                case 'aria2.tellStatus':
                    result = params[0] === 'torrent'
                        ? { status: 'paused', files: [{ length: '4' }] }
                        : { status: paused ? 'paused' : restarted ? 'complete' : 'active', completedLength: '1' };
                    break;
            }
            res.end(JSON.stringify({ result }));
        } catch (error) {
            res.statusCode = 500;
            res.end(JSON.stringify({ error: { message: error.message } }));
        }
    });
    const before = createServer((req, res) => rpc.emit('request', req, res));
    await Promise.all([before, rpc].map((server) => new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))));
    t.after(async () => {
        for (const server of [before, rpc]) server.closeAllConnections();
        await Promise.all([before, rpc].map((server) => new Promise((resolve) => server.close(resolve))));
        t.mock.restoreAll();
        syncBuiltinESMExports();
    });
    assert.notEqual(before.address().port, rpc.address().port);
    t.mock.method(childProcess, 'execFileSync', (command, args) => {
        assert.equal(command, 'docker');
        switch (args[0]) {
            case 'run':
                secret = args.find((arg) => arg.startsWith('RPC_SECRET=')).slice('RPC_SECRET='.length);
                return 'test-container';
            case 'port':
                portReads++;
                return `127.0.0.1:${(restarted ? rpc : before).address().port}`;
            case 'restart':
                restarted = true;
                before.closeAllConnections();
                before.close();
                return 'test-container';
            case 'exec': return `${checksum}  /data/nested/fixture.bin`;
            case 'rm': return 'test-container';
            default: throw new Error(`Unexpected Docker command: ${args[0]}`);
        }
    });
    syncBuiltinESMExports();
    await import('./aria2-smoke.mjs');
    assert.equal(portReads, 2);
    assert.ok(callsAfterRestart > 0, 'RPC must reconnect after the old listener closes');
});
