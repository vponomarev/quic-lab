plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }
android {
    namespace = "ru.vpnc.quiclab"
    compileSdk = 35
    defaultConfig {
        applicationId = "ru.vpnc.quiclab"
        minSdk = 31
        targetSdk = 35
        versionCode = 3
        versionName = "0.3.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
}
dependencies {
    implementation(files("libs/quiclab.aar"))
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
