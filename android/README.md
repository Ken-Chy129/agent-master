# agent-master — Android client

Native Android client for the agent-master daemon. It speaks the exact same
HTTP + SSE contract as the web and desktop clients (`docs/API.md`), so it is
"just another remote" in the multi-machine topology (`docs/DESIGN.md` §2): the
app holds a list of machine profiles `{name, url, token}` and connects directly
to whichever daemon is active. There is no central hub.

This module is a faithful Kotlin port of the verified TypeScript reference
client in `frontend/packages/core` and the web store in
`frontend/apps/web/src/store.ts`.

## Module layout

```
android/
  settings.gradle.kts        includes :core and :app; Aliyun mirrors FIRST
  build.gradle.kts           plugin versions pinned once (apply false)
  gradle.properties          AndroidX, parallel build, JVM args
  gradle/wrapper/…           Gradle 8.10.2 via Tencent CN mirror
  core/                      PURE Kotlin/JVM library — no Android imports
    Models.kt                wire types (Session, WireEvent, payloads, envelopes)
    ApiClient.kt             OkHttp REST client, Bearer auth, typed ApiError
    SseClient.kt             okhttp-sse stream, am_event parsing, resume+backoff
    Machines.kt              MachineProfile, parsePairLink, defaultMachineName
    SessionStore.kt          headless state store — the port of web store.ts
    src/test/…               JUnit: parse links, WireEvent round-trip, store logic
  app/                       Android Compose app
    MainActivity.kt          single activity, deep-link handling
    MainViewModel.kt         thin AndroidViewModel wrapping SessionStore
    EncryptedMachineStore.kt EncryptedSharedPreferences machine/token storage
    HomeScreen.kt            machine switcher + session list + create dialogs
    ConversationScreen.kt    dumb WireEvent renderer + composer
    Components.kt / Theme.kt shared UI
```

`:core` is the reusable, unit-testable boundary. All conversation and
machine-list logic lives there; `:app` only composes UI and wires platform
concerns (secure storage, deep links). This mirrors Garyx's "logic in Core, UI
only composes" split (`DESIGN.md` §11).

## What is verified vs. scaffold

**Verified in this environment (JDK 21, no Android SDK):**

- `:core` **compiles and all 28 unit tests pass** via `gradle :core:test`
  against the Aliyun Maven mirror. `:core` is pure Kotlin/JVM, so it builds
  without the Android SDK.
  - `MachinesTest` (9): `parsePairLink` valid/optional-name/missing-field/
    wrong-scheme/garbage, `defaultMachineName` host+port/no-port/fallback.
  - `WireEventTest` (11): per-type payload decode + accessors, tool
    call/result arbitrary JSON preservation, run finished states, unknown-type
    forward compatibility, envelope decoding, null-omitting request encode.
  - `SessionStoreTest` (8): `upsertEvent` append+sort+dedupe-by-seq,
    `computeRunActive` ledger reducer, `errText`.
- The Gradle wrapper (8.10.2) and CN mirror resolution are exercised and work.

**Scaffold — build on your Mac:**

- The `:app` Android module is **not compiled here** because there is no Android
  SDK in this environment. The Compose UI, ViewModel, EncryptedSharedPreferences
  store, manifest, and deep-link wiring are written to conform to the contract
  and to standard AndroidX/Compose APIs, but have not been compiled or run.
  Build them in Android Studio (steps below).

## Build on macOS (Android Studio)

1. Install **Android Studio** (Koala or newer) and, via its SDK Manager, the
   **Android SDK Platform 34** and **Build-Tools 34.x**.
2. Open `agent-master/android/` in Android Studio. It writes `local.properties`
   with your `sdk.dir` automatically. (If you build from the CLI, create
   `android/local.properties` containing
   `sdk.dir=/Users/<you>/Library/Android/sdk`.)
3. The Gradle wrapper points at the Tencent CN mirror and
   `settings.gradle.kts` lists the Aliyun Maven mirrors first, so dependency
   resolution works behind the GFW. If you are on an unrestricted network you
   can leave them; they still fall through to `google()` / `mavenCentral()`.
4. Run the `:core` tests:
   ```
   ./gradlew :core:test
   ```
5. Build / install the app on a device or emulator:
   ```
   ./gradlew :app:assembleDebug          # APK in app/build/outputs/apk/debug/
   ./gradlew :app:installDebug           # onto a connected device/emulator
   ```

## Pairing (deep link)

Opening an `agentmaster://pair?url=<daemon-url>&token=<token>&name=<label>` link
adds (or updates) that machine and selects it. The scheme is registered in
`AndroidManifest.xml`; `MainActivity` forwards the URI to
`MainViewModel.handleDeepLink`, which parses it with `parsePairLink` in `:core`
(same rules as the web/desktop clients).

## Pinned versions

AGP 8.5.2 · Kotlin 2.0.21 (+ compose compiler plugin) · Compose BOM 2024.09.03 ·
OkHttp 4.12.0 (+ okhttp-sse) · kotlinx-serialization-json 1.7.3 · coroutines
1.8.1 · androidx.security:security-crypto 1.1.0-alpha06.
