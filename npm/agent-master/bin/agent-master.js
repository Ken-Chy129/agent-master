#!/usr/bin/env node

import { readFile } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

import { releasePlan } from '../lib/release.js';

const packageJSON = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
const plan = releasePlan({
  version: process.env.AGENT_MASTER_VERSION || packageJSON.version,
  platform: process.platform,
  arch: process.arch,
});
const binaryPath = fileURLToPath(new URL(`../vendor/${plan.binaryName}`, import.meta.url));

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: 'inherit',
  windowsHide: false,
});

child.on('error', (error) => {
  console.error(`Unable to run agent-master: ${error.message}`);
  console.error('Try reinstalling it with: npm install -g @ken-chy129/agent-master');
  process.exitCode = 1;
});

child.on('exit', (code, signal) => {
  if (signal && process.platform !== 'win32') {
    process.kill(process.pid, signal);
    return;
  }
  process.exitCode = code ?? 1;
});
