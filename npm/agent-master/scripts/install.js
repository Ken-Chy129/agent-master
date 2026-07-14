import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

import { installBinary } from '../lib/install.js';

const packageJSON = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));

try {
  const binaryPath = await installBinary({
    version: process.env.AGENT_MASTER_VERSION || packageJSON.version,
    installDir: fileURLToPath(new URL('../vendor/', import.meta.url)),
    baseUrl: process.env.AGENT_MASTER_RELEASE_BASE_URL,
  });
  console.log(`agent-master native binary installed at ${binaryPath}`);
} catch (error) {
  console.error(`agent-master installation failed: ${error instanceof Error ? error.message : error}`);
  process.exitCode = 1;
}
