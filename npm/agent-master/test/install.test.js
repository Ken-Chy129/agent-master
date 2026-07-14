import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { installBinary } from '../lib/install.js';
import { releasePlan } from '../lib/release.js';

test('npm package uses the owned scope while preserving the CLI command', async () => {
  const packageJSON = JSON.parse(
    await readFile(new URL('../package.json', import.meta.url), 'utf8'),
  );

  assert.equal(packageJSON.name, '@ken-chy129/agent-master');
  assert.equal(packageJSON.bin['agent-master'], 'bin/agent-master.js');
  assert.equal(packageJSON.publishConfig.access, 'public');
});

test('releasePlan maps Node platform names to release assets', () => {
  assert.deepEqual(releasePlan({ version: '0.2.2', platform: 'darwin', arch: 'arm64' }), {
    version: '0.2.2',
    asset: 'agent-master-darwin-arm64',
    binaryName: 'agent-master',
    assetUrl:
      'https://github.com/Ken-Chy129/agent-master/releases/download/v0.2.2/agent-master-darwin-arm64',
    checksumUrl:
      'https://github.com/Ken-Chy129/agent-master/releases/download/v0.2.2/agent-master-darwin-arm64.sha256',
  });

  assert.equal(
    releasePlan({ version: 'v0.2.2', platform: 'win32', arch: 'x64' }).asset,
    'agent-master-windows-amd64.exe',
  );
});

test('releasePlan rejects unsupported platforms and development versions', () => {
  assert.throws(
    () => releasePlan({ version: '0.2.2', platform: 'freebsd', arch: 'x64' }),
    /Unsupported platform/,
  );
  assert.throws(
    () => releasePlan({ version: '0.0.0-development', platform: 'darwin', arch: 'arm64' }),
    /published package version/,
  );
  assert.throws(
    () => releasePlan({ version: '../latest', platform: 'darwin', arch: 'arm64' }),
    /valid semantic version/,
  );
  assert.throws(
    () =>
      releasePlan({
        version: '0.2.2',
        platform: 'darwin',
        arch: 'arm64',
        baseUrl: 'http://downloads.example.com/v0.2.2',
      }),
    /HTTPS/,
  );
});

test('installBinary downloads and verifies the native executable', async () => {
  const binary = Buffer.from('native agent-master binary');
  const digest = createHash('sha256').update(binary).digest('hex');
  const installDir = await mkdtemp(path.join(tmpdir(), 'agent-master-npm-'));
  const requested = [];

  const binaryPath = await installBinary({
    version: '0.2.2',
    platform: 'darwin',
    arch: 'arm64',
    installDir,
    fetchImpl: async (url) => {
      requested.push(String(url));
      if (String(url).endsWith('.sha256')) {
        return new Response(`${digest}  agent-master-darwin-arm64\n`);
      }
      return new Response(binary);
    },
  });

  assert.deepEqual(requested, [
    'https://github.com/Ken-Chy129/agent-master/releases/download/v0.2.2/agent-master-darwin-arm64',
    'https://github.com/Ken-Chy129/agent-master/releases/download/v0.2.2/agent-master-darwin-arm64.sha256',
  ]);
  assert.equal(binaryPath, path.join(installDir, 'agent-master'));
  assert.deepEqual(await readFile(binaryPath), binary);
  assert.notEqual((await stat(binaryPath)).mode & 0o111, 0);
});

test('installBinary refuses a checksum mismatch', async () => {
  const installDir = await mkdtemp(path.join(tmpdir(), 'agent-master-npm-'));

  await assert.rejects(
    installBinary({
      version: '0.2.2',
      platform: 'linux',
      arch: 'x64',
      installDir,
      fetchImpl: async (url) =>
        String(url).endsWith('.sha256')
          ? new Response(`${'0'.repeat(64)}  agent-master-linux-amd64\n`)
          : new Response('unexpected payload'),
    }),
    /checksum mismatch/,
  );
});

test('installBinary rejects an oversized download', async () => {
  const installDir = await mkdtemp(path.join(tmpdir(), 'agent-master-npm-'));

  await assert.rejects(
    installBinary({
      version: '0.2.2',
      platform: 'linux',
      arch: 'x64',
      installDir,
      maxBinaryBytes: 4,
      fetchImpl: async (url) =>
        String(url).endsWith('.sha256')
          ? new Response(`${'0'.repeat(64)}  agent-master-linux-amd64\n`)
          : new Response('12345'),
    }),
    /exceeds 4 bytes/,
  );
});
