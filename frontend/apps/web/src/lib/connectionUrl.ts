interface LocationLike {
  protocol: string;
  origin: string;
}

/**
 * The daemon-hosted Web client is same-origin, including LAN/Tailscale URLs.
 * The Electron shell uses app:// and should keep its existing localhost default.
 */
export function defaultDaemonUrl(location: LocationLike): string {
  if (location.protocol === 'http:' || location.protocol === 'https:') {
    return location.origin;
  }
  return 'http://localhost:8888';
}
