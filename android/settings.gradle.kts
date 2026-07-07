// agent-master Android client — multi-module Gradle build.
//
// CN mirrors are listed FIRST so the build resolves plugins/dependencies from
// them before falling back to the canonical Google / Maven Central repos. This
// keeps the project buildable behind Great-Firewall-affected networks.

pluginManagement {
    repositories {
        maven("https://maven.aliyun.com/repository/public")
        maven("https://maven.aliyun.com/repository/google")
        maven("https://maven.aliyun.com/repository/gradle-plugin")
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    // Modules may declare their own repos (none do), but prefer the settings
    // block so mirror ordering is enforced project-wide.
    repositoriesMode.set(RepositoriesMode.PREFER_SETTINGS)
    repositories {
        maven("https://maven.aliyun.com/repository/public")
        maven("https://maven.aliyun.com/repository/google")
        google()
        mavenCentral()
    }
}

rootProject.name = "agent-master-android"

include(":core")
include(":app")
