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

  return {
    version: cleanVersion,
    asset,
    binaryName: `agent-master${extension}`,
    assetUrl: `${releaseBase}/${asset}`,
    checksumUrl: `${releaseBase}/${asset}.sha256`,
  };
}
