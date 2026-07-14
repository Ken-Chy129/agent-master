import { createHash } from 'node:crypto';
import { chmod, mkdir, rename, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';

import { releasePlan } from './release.js';

export async function installBinary({
  version,
  platform = process.platform,
  arch = process.arch,
  installDir,
  baseUrl,
  fetchImpl = globalThis.fetch,
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

  const binary = Buffer.from(await binaryResponse.arrayBuffer());
  const checksumText = await checksumResponse.text();
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

function assertDownload(response, url) {
  if (!response?.ok) {
    throw new Error(`download failed (${response?.status ?? 'unknown'}): ${url}`);
  }
}
