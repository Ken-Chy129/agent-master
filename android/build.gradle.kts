// Root build script. Plugins are declared with `apply false` so their versions
// are pinned once here and applied by the subprojects that need them.
plugins {
    id("com.android.application") version "8.5.2" apply false
    id("org.jetbrains.kotlin.android") version "2.0.21" apply false
    id("org.jetbrains.kotlin.jvm") version "2.0.21" apply false
    id("org.jetbrains.kotlin.plugin.serialization") version "2.0.21" apply false
    // Compose compiler is a standalone Kotlin plugin from Kotlin 2.0 onward.
    id("org.jetbrains.kotlin.plugin.compose") version "2.0.21" apply false
}
