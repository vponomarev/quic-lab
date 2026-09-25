plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }
android {
    namespace = "ru.vpnc.quiclab"
    compileSdk = 35
    defaultConfig {
        applicationId = "ru.vpnc.quiclab"
        minSdk = 30
        targetSdk = 35
        versionCode = 8
        versionName = "0.6.0-dev"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
}
dependencies {
    // CaptureManager uses ContextCompat and ActivityCompat at runtime.
    implementation("androidx.core:core:1.16.0")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.google.zxing:core:3.5.3")
    implementation(files("libs/quiclab.aar"))
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
