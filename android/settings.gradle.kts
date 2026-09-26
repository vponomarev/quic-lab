pluginManagement { repositories { google(); mavenCentral(); gradlePluginPortal() } }
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories { google(); mavenCentral() }
}
rootProject.name = "QuicLab"
include(":app")

// Optional on-device traffic generator; never included in the production app.
if (providers.gradleProperty("withProbe").isPresent) include(":probe")
