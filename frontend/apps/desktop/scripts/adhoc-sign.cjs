'use strict';

const { execFileSync } = require('node:child_process');
const path = require('node:path');

/**
 * electron-builder afterPack hook: give the macOS bundle a real ad-hoc signature.
 *
 * There is no Apple Developer certificate in CI, and the release workflow sets
 * CSC_IDENTITY_AUTO_DISCOVERY=false, so electron-builder skips signing entirely.
 * What ships then is only the signature Apple's linker puts on each arm64 binary,
 * which does not cover the bundle:
 *
 *     Identifier=Electron
 *     flags=0x20002(adhoc,linker-signed)
 *     Info.plist=not bound
 *     Sealed Resources=none
 *     $ codesign --verify --deep --strict "Agent Master.app"
 *     code has no resources but signature indicates they must be present
 *
 * A quarantined download with a signature that fails validation like that is not
 * merely "from an unidentified developer" — on macOS 15+ Gatekeeper reports it as
 * malware and moves the app to the Trash, which no amount of clearing the
 * quarantine attribute can undo. Signing here produces a bundle that validates
 * (correct identifier, bound Info.plist, sealed resources), so the worst a user
 * sees is the ordinary unsigned-app prompt.
 *
 * This is still not notarized: `spctl -a` rejects it by design, and users clear
 * the quarantine flag once. See the README.
 */
exports.default = async function adhocSign(context) {
  if (context.electronPlatformName !== 'darwin') return;

  // Skip when a real identity is in play — re-signing ad-hoc would throw it away.
  if (process.env.CSC_IDENTITY_AUTO_DISCOVERY !== 'false' && (process.env.CSC_LINK || process.env.CSC_NAME)) {
    console.log('[adhoc-sign] a signing identity is configured; leaving the signature alone');
    return;
  }

  const appPath = path.join(
    context.appOutDir,
    `${context.packager.appInfo.productFilename}.app`,
  );

  // --deep is deprecated by Apple for distribution signing, but it is the
  // supported way to ad-hoc sign an already-assembled bundle's nested helpers
  // and frameworks in one step, which is exactly what is needed here.
  execFileSync('codesign', ['--force', '--deep', '--sign', '-', appPath], {
    stdio: 'inherit',
  });

  // Fail the build rather than ship another bundle that Gatekeeper will reject:
  // the whole point of this hook is that the signature validates.
  execFileSync('codesign', ['--verify', '--deep', '--strict', appPath], {
    stdio: 'inherit',
  });

  console.log(`[adhoc-sign] ad-hoc signed and verified ${appPath}`);
};
