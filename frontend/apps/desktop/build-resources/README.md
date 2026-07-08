# Build resources

electron-builder picks up the app icon from this directory automatically:

- `icon.png` — 1024x1024 (or at least 512x512) source icon; electron-builder
  converts it to `.icns` (macOS) / `.ico` (Windows) at package time.

Drop the icon file here and rebuild (`npm run dist -w @agent-master/desktop`).
