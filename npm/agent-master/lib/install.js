import { createHash } from 'node:crypto';
import { chmod, mkdir, rename, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';

import { releasePlan } from './release.js';

const DEFAULT_MAX_BINARY_BYTES = 128 * 1024 * 1024;
const MAX_CHECKSUM_BYTES = 8 * 1024;

export async function installBinary({
  version,
  platform = process.platform,
  arch = process.arch,
  installDir,
  baseUrl,
  fetchImpl = globalThis.fetch,
  maxBinaryBytes = DEFAULT_MAX_BINARY_BYTES,
}) {
  if (!installDir) throw new Error('installDir is required');
  if (typeof fetchImpl !== 'function') throw new Error('Node.js 20 or newer is required');

  const plan = releasePlan({ version, platform, arch, baseUrl });
  const [binaryResponse, checksumResponse] = await Promise.all([
    fetchImpl(plan.assetUrl),
    fetchImpl(plan.checksumUrl),
  ]);
  assertDownload(binaryResponse, plan.assetUrl);
  assertDownload(checksumResponse, plan.checksumUrl);

  const [binary, checksum] = await Promise.all([
    readLimited(binaryResponse, maxBinaryBytes, plan.asset),
    readLimited(checksumResponse, MAX_CHECKSUM_BYTES, `${plan.asset}.sha256`),
  ]);
  const checksumText = checksum.toString('utf8');
  const expected = checksumText.trim().split(/\s+/)[0]?.toLowerCase();
  if (!expected || !/^[a-f0-9]{64}$/.test(expected)) {
    throw new Error(`invalid checksum file for ${plan.asset}`);
  }

  const actual = createHash('sha256').update(binary).digest('hex');
  if (actual !== expected) {
    throw new Error(`checksum mismatch for ${plan.asset}: expected ${expected}, got ${actual}`);
  }

  await mkdir(installDir, { recursive: true });
  const binaryPath = path.join(installDir, plan.binaryName);
  const temporaryPath = `${binaryPath}.download`;
  await writeFile(temporaryPath, binary, { mode: 0o755 });
  await chmod(temporaryPath, 0o755);
  await rm(binaryPath, { force: true });
  await rename(temporaryPath, binaryPath);
  return binaryPath;
}

async function readLimited(response, maxBytes, label) {
  const declaredLength = Number(response.headers.get('content-length'));
  if (Number.isFinite(declaredLength) && declaredLength > maxBytes) {
    throw new Error(`${label} exceeds ${maxBytes} bytes`);
  }

  if (!response.body) {
    const data = Buffer.from(await response.arrayBuffer());
    if (data.length > maxBytes) throw new Error(`${label} exceeds ${maxBytes} bytes`);
    return data;
  }

  const reader = response.body.getReader();
  const chunks = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maxBytes) {
      await reader.cancel();
      throw new Error(`${label} exceeds ${maxBytes} bytes`);
    }
    chunks.push(Buffer.from(value));
  }
  return Buffer.concat(chunks, total);
}

function assertDownload(response, url) {
  if (!response?.ok) {
    throw new Error(`download failed (${response?.status ?? 'unknown'}): ${url}`);
  }
}
