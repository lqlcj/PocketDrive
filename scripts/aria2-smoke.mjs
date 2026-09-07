// Requires Docker and Node 24. Uses disposable containers and volumes only.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { createServer } from 'node:http';
import { setTimeout as delay } from 'node:timers/promises';

const image = process.argv[2] || 'pocketdrive-aria2:test';
const name = `pd-aria2-test-${process.pid}`;
const secret = randomBytes(24).toString('hex');
const payload = randomBytes(1024 * 1024);
const docker = (...args) => execFileSync('docker', args, { encoding: 'utf8' }).trim();
const server = createServer((req, res) => {
    const start = Number(req.headers.range?.match(/^bytes=(\d+)-/)?.[1] || 0);
    res.writeHead(start ? 206 : 200, {
        'Content-Length': payload.length - start,
        'Accept-Ranges': 'bytes',
        ...(start ? { 'Content-Range': `bytes ${start}-${payload.length - 1}/${payload.length}` } : {}),
    });
    res.end(payload.subarray(start));
});
await new Promise(resolve => server.listen(0, '0.0.0.0', resolve));
let endpoint;
async function rpc(method, params = [], token = secret) {
    const response = await fetch(endpoint, {
        method: 'POST',
        body: JSON.stringify({ jsonrpc: '2.0', id: 'smoke', method: `aria2.${method}`, params: [`token:${token}`, ...params] }),
        signal: AbortSignal.timeout(5000),
    });
    const result = await response.json();
    if (result.error) throw new Error(result.error.message);
    return result.result;
}
async function until(check) {
    let lastError;
    for (let i = 0; i < 100; i++) {
        try { if (await check()) return; } catch (error) { lastError = error; }
        await delay(300);
    }
    throw new Error('Timed out waiting for aria2', { cause: lastError });
}

try {
    docker('run', '-d', '--name', name, '--add-host=host.docker.internal:host-gateway',
        '-p', '127.0.0.1::6800', '-e', `RPC_SECRET=${secret}`, image);
    const port = docker('port', name, '6800/tcp').split(':').at(-1);
    endpoint = `http://127.0.0.1:${port}/jsonrpc`;
    await until(async () => (await rpc('getVersion')).version);
    await assert.rejects(rpc('getVersion', [], 'incorrect-secret'));
    await rpc('changeGlobalOption', [{ 'max-concurrent-downloads': '2' }]);
    assert.equal((await rpc('getGlobalOption'))['max-concurrent-downloads'], '2');

    const gid = await rpc('addUri', [[`http://host.docker.internal:${server.address().port}/fixture.bin`],
        { dir: '/data/nested', 'max-download-limit': '64K' }]);
    await until(async () => Number((await rpc('tellStatus', [gid])).completedLength) > 0);
    await rpc('pause', [gid]);
    await until(async () => (await rpc('tellStatus', [gid])).status === 'paused');
    await rpc('saveSession');
    docker('restart', '--time', '60', name);
    await until(async () => (await rpc('tellStatus', [gid])).status === 'paused');
    await rpc('changeOption', [gid, { 'max-download-limit': '0' }]);
    await rpc('unpause', [gid]);
    await until(async () => (await rpc('tellStatus', [gid])).status === 'complete');
    const checksum = docker('exec', name, 'sha256sum', '/data/nested/fixture.bin').split(' ')[0];
    assert.equal(checksum, createHash('sha256').update(payload).digest('hex'));
    await rpc('removeDownloadResult', [gid]);

    // A valid one-piece torrent exercises paused metadata and file selection without peers.
    const torrent = Buffer.from('d4:infod6:lengthi4e4:name11:fixture.txt12:piece lengthi16384e6:pieces20:abcdefghijklmnopqrstee').toString('base64');
    const bt = await rpc('addTorrent', [torrent, [], { pause: 'true', dir: '/data' }]);
    const status = await rpc('tellStatus', [bt]);
    assert.equal(status.status, 'paused');
    assert.equal(status.files.length, 1);
    assert.equal(status.files[0].length, '4');
    await rpc('changeOption', [bt, { 'select-file': '1' }]);
    await rpc('remove', [bt]);
    console.log('aria2 smoke passed: RPC auth, settings, download, pause, restart, checksum, torrent selection, removal');
} catch (error) {
    try { console.error(docker('logs', '--tail', '80', name)); } catch { /* No container logs available. */ }
    throw error;
} finally {
    server.closeAllConnections();
    server.close();
    try { docker('rm', '-fv', name); } catch { /* Container may not have started. */ }
}
