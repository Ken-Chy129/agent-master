const REPOSITORY = 'Ken-Chy129/agent-master';

const platformNames = {
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows',
};

const architectureNames = {
  arm64: 'arm64',
  x64: 'amd64',
};

export function releasePlan({ version, platform, arch, baseUrl }) {
  const cleanVersion = String(version).replace(/^v/, '');
  if (!cleanVersion || cleanVersion.includes('development')) {
    throw new Error('agent-master must be installed from a published package version');
  }
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(cleanVersion)) {
    throw new Error(`agent-master requires a valid semantic version, got ${cleanVersion}`);
  }

  const releasePlatform = platformNames[platform];
  const releaseArch = architectureNames[arch];
  if (!releasePlatform || !releaseArch) {
    throw new Error(`Unsupported platform: ${platform}/${arch}`);
  }

  const extension = platform === 'win32' ? '.exe' : '';
  const asset = `agent-master-${releasePlatform}-${releaseArch}${extension}`;
  const releaseBase =
    baseUrl?.replace(/\/+$/, '') ??
    `https://github.com/${REPOSITORY}/releases/download/v${cleanVersion}`;
  if (new URL(releaseBase).protocol !== 'https:') {
    throw new Error('agent-master release downloads require HTTPS');
  }

  return {
    version: cleanVersion,
    asset,
    binaryName: `agent-master${extension}`,
    assetUrl: `${releaseBase}/${asset}`,
    checksumUrl: `${releaseBase}/${asset}.sha256`,
  };
}
