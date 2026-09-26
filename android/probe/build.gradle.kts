plugins { id("com.android.application") }
android {
 namespace="ru.vpnc.quicprobe"
 compileSdk=35
 defaultConfig { applicationId="ru.vpnc.quicprobe";minSdk=30;targetSdk=35;versionCode=1;versionName="test" }
 flavorDimensions += "route"
 productFlavors { create("selected") {dimension="route"}; create("direct") {dimension="route";applicationIdSuffix=".direct"} }
 compileOptions {sourceCompatibility=JavaVersion.VERSION_17;targetCompatibility=JavaVersion.VERSION_17}
}
